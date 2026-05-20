package mobileapi

import (
	"context"
	"encoding/json"
	"fmt"

	tsskeygen "github.com/bnb-chain/tss-lib/v3/ecdsa/keygen"
	"github.com/bnb-chain/tss-lib/v3/tss"

	"github.com/zzci/mpc/internal/contract"
	"github.com/zzci/mpc/internal/keystore"
	"github.com/zzci/mpc/internal/mpc"
)

// ReshareCallback mirrors KeyGenCallback for a resharing run. The master
// public key — and therefore every derived chain address — is invariant
// across resharing (docs/design/mcp/sdk.md §7); the summary's groupPubKey MUST
// equal the pre-reshare value.
type ReshareCallback interface {
	// OnProgress reports a coarse stage label.
	OnProgress(stage string)
	// OnResult delivers the new-committee summary JSON (same shape as keygen,
	// single-party: one moniker for THIS device).
	OnResult(summaryJSON string)
	// OnError reports a terminal failure as a stable {code,msg} pair.
	OnError(code string, msg string)
}

// reshareConfig is the configJSON schema for Reshare under DM-3 (hard-cut).
// The DM-3 mandatory wire envelope is shared with keygen / sign; OldT and
// NewT carry the threshold transition that distinguishes a reshare config
// from a fresh keygen config (the doc's bare "t" maps to NewT; OldT is
// required because rotate mode uses a (t→t') transition even when t==t').
type reshareConfig struct {
	GroupID    *string      `json:"groupId"`
	SessionID  *string      `json:"sessionID"`
	PartyIndex *int         `json:"partyIndex"`
	N          *int         `json:"n"`
	OldT       *int         `json:"oldT"`
	NewT       *int         `json:"newT"`
	MemberSet  []string     `json:"memberSet"`
	Relay      *relayConfig `json:"relay"`
	Role       *string      `json:"role"`
	Passphrase string       `json:"passphrase"`
}

// validate enforces the DM-3 hard-cut for reshare. Same posture as the
// keygen schema: pointer fields detect absence, structural ranges fail loud.
func (cfg reshareConfig) validate() error {
	switch {
	case cfg.GroupID == nil || *cfg.GroupID == "":
		return fmt.Errorf("missing groupId")
	case cfg.SessionID == nil || *cfg.SessionID == "":
		return fmt.Errorf("missing sessionID")
	case cfg.PartyIndex == nil:
		return fmt.Errorf("missing partyIndex")
	case cfg.N == nil:
		return fmt.Errorf("missing n")
	case cfg.OldT == nil:
		return fmt.Errorf("missing oldT")
	case cfg.NewT == nil:
		return fmt.Errorf("missing newT")
	case len(cfg.MemberSet) == 0:
		return fmt.Errorf("missing memberSet")
	case cfg.Relay == nil:
		return fmt.Errorf("missing relay")
	case cfg.Relay.PeerID == "":
		return fmt.Errorf("missing relay.peerID")
	case len(cfg.Relay.Addrs) == 0:
		return fmt.Errorf("missing relay.addrs")
	case cfg.Role == nil || *cfg.Role == "":
		return fmt.Errorf("missing role")
	case *cfg.N < 2:
		return fmt.Errorf("n must be >= 2, got %d", *cfg.N)
	case *cfg.OldT < 1 || *cfg.OldT >= *cfg.N:
		return fmt.Errorf("need 1 <= oldT < n, got oldT=%d n=%d", *cfg.OldT, *cfg.N)
	case *cfg.NewT < 1 || *cfg.NewT >= *cfg.N:
		return fmt.Errorf("need 1 <= newT < n, got newT=%d n=%d", *cfg.NewT, *cfg.N)
	case *cfg.PartyIndex < 0 || *cfg.PartyIndex >= *cfg.N:
		return fmt.Errorf("partyIndex %d out of range [0,%d)", *cfg.PartyIndex, *cfg.N)
	case len(cfg.MemberSet) != *cfg.N:
		return fmt.Errorf("memberSet length %d != n=%d", len(cfg.MemberSet), *cfg.N)
	case cfg.Passphrase == "":
		return fmt.Errorf("passphrase must not be empty")
	}
	return nil
}

// Reshare redistributes THIS device's view of the existing committee onto a
// new (t→t') committee on a background goroutine: it drives one
// mpc.ReshareParty (DM-1 rotate mode, n=n') against the host's wire callback
// and OnWireMessage feed, reseals the refreshed share_i to the keystore, and
// keeps the master public key fixed (docs/design/mcp/sdk.md §7).
//
// configJSON is mandatory and hard-cut (distributed-mpc-impl.md §B DM-3): an
// old configJSON lacking any of the new envelope fields fails with
// CodeBadConfig before MPC begins.
func (s *SDK) Reshare(configJSON string, wire WireCallbacks, cb ReshareCallback) {
	var cfg reshareConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		cb.OnError(CodeBadConfig, fmt.Sprintf("invalid configJSON: %v", err))
		return
	}
	if err := cfg.validate(); err != nil {
		cb.OnError(CodeBadConfig, err.Error())
		return
	}
	if wire == nil {
		cb.OnError(CodeBadConfig, "WireCallbacks is required for reshare")
		return
	}
	go s.runReshare(cfg, wire, cb)
}

func (s *SDK) runReshare(cfg reshareConfig, wire WireCallbacks, cb ReshareCallback) {
	ctx := context.Background()
	old, _, _, _, ok := s.snapshotOwnShare()
	if !ok {
		cb.OnError(CodeNoShares, "no key share held to reshare from")
		return
	}

	var pre *tsskeygen.LocalPreParams
	if len(s.preParams) > 0 {
		p := s.preParams[0]
		pre = &p
	}

	party, err := mpc.NewReshareParty(ctx, mpc.SinglePartyReshareConfig{
		OldThreshold: *cfg.OldT,
		NewThreshold: *cfg.NewT,
		Parties:      *cfg.N,
		PartyIndex:   *cfg.PartyIndex,
		OldShare:     old,
		PreParams:    pre,
	})
	if err != nil {
		cb.OnError(CodeInternal, fmt.Sprintf("new reshare party: %v", err))
		return
	}

	// Resharing parses each inbound by the SENDING committee's PartyID space
	// (committee marker is the leading byte of MpcMessage.From / To); the
	// resolver hands back the right PartyID for either committee.
	oldPIDs := party.OldPeers()
	newPIDs := party.NewPeers()
	resolve := func(from string) *tss.PartyID {
		if len(from) < 2 {
			return nil
		}
		switch from[0] {
		case 'O':
			for _, p := range oldPIDs {
				if p.Id == from[1:] {
					return p
				}
			}
		case 'N':
			for _, p := range newPIDs {
				if p.Id == from[1:] {
					return p
				}
			}
		}
		return nil
	}
	apply := func(parsed tss.ParsedMessage, mm *contract.MpcMessage) {
		// Committee target lives in the leading byte of each To entry
		// (cli/mpcnet.go / mpcnet.committeeTargets convention). The same
		// inbound parsed message may need feeding into both committees
		// when the marker is 'B'; the underlying mpc.ReshareParty is safe
		// under concurrent UpdateOld/UpdateNew (mpcnet.go pumpReshare).
		toOld, toNew := false, false
		for _, t := range mm.To {
			if len(t) == 0 {
				continue
			}
			switch t[0] {
			case 'O':
				toOld = true
			case 'N':
				toNew = true
			case 'B':
				toOld, toNew = true, true
			}
		}
		if mm.IsBroadcast {
			toOld, toNew = true, true
		}
		if toOld {
			go func() { _ = party.UpdateOld(parsed) }()
		}
		if toNew {
			go func() { _ = party.UpdateNew(parsed) }()
		}
	}

	// The wire-from / wire-self tags follow the reshare committee marker
	// convention: this device is "O<idx>" when its old committee sends,
	// "N<idx>" when its new committee sends. The wire pump's emitOutbound
	// path picks the correct prefix per message via reshareEmitOutbound.
	ws := &wireSession{
		sessionID: *cfg.SessionID,
		// The "self" here is purely descriptive for the receive-side gate;
		// we set it to a marker that no inbound MpcMessage.From can match
		// (since wire-from is committee-prefixed) so the SDK never short-
		// circuits a same-device inbound on identity. The actual outbound
		// from-tag is composed per message in reshareEmitOutbound.
		self:      "",
		resolveFn: resolve,
		applyFn:   apply,
	}
	if !s.installSession(ws) {
		cb.OnError(CodeInternal, "another MPC session is already active on this SDK")
		return
	}
	defer s.removeSession(ws.sessionID)

	stop := make(chan struct{})
	rerr := make(chan error, 1)
	pumpCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go reshareOutbound(pumpCtx, party, *cfg.PartyIndex, *cfg.N, *cfg.SessionID, wire, stop, rerr, apply)

	cb.OnProgress("computing")
	if err := party.Start(); err != nil {
		close(stop)
		cb.OnError(CodeInternal, fmt.Sprintf("start reshare: %v", err))
		return
	}

	share, doneErr := party.Done(ctx)
	close(stop)
	if doneErr != nil {
		cb.OnError(CodeInternal, fmt.Sprintf("reshare failed: %v", doneErr))
		return
	}
	select {
	case err := <-rerr:
		cb.OnError(CodeInternal, fmt.Sprintf("reshare wire pump: %v", err))
		return
	default:
	}

	cb.OnProgress("sealing")
	pubHex, err := groupPubHex(share)
	if err != nil {
		cb.OnError(CodeInternal, err.Error())
		return
	}
	if err := s.store.Save(ctx, keystore.Share{
		Moniker:  share.Moniker,
		SaveData: share.SaveData,
	}, cfg.Passphrase); err != nil {
		cb.OnError(CodeInternal, fmt.Sprintf("seal new share %q: %v", share.Moniker, err))
		return
	}

	s.setOwnShare(share, *cfg.NewT, *cfg.N, *cfg.PartyIndex, pubHex)

	out, err := json.Marshal(keygenSummary{
		Threshold:   *cfg.NewT,
		Parties:     *cfg.N,
		PartyIndex:  *cfg.PartyIndex,
		Moniker:     share.Moniker,
		GroupPubKey: pubHex,
	})
	if err != nil {
		cb.OnError(CodeInternal, fmt.Sprintf("marshal summary: %v", err))
		return
	}
	cb.OnResult(string(out))
}

// reshareOutbound is the wire pump for one reshare session: it tags each
// outbound message with the sender's committee ('O' = old, 'N' = new) and
// the destination committee marker on every To entry, mirroring the wire
// layout in internal/cli/mpcnet.go (and mpcnet.routeReshareOut). Same-device
// cross-committee deliveries are applied in-process via apply — libp2p (and
// any host transport) cannot dial self.
func reshareOutbound(
	ctx context.Context,
	party *mpc.ReshareParty,
	selfIdx, n int,
	sessionID string,
	wc WireCallbacks,
	stop <-chan struct{},
	rerr chan<- error,
	apply func(parsed tss.ParsedMessage, mm *contract.MpcMessage),
) {
	oTag := "O" + reshareTagFor(selfIdx)
	nTag := "N" + reshareTagFor(selfIdx)
	oldSelf := party.OldPartyID()
	newSelf := party.NewPartyID()

	emit := func(m tss.Message) {
		if err := reshareEmitOutbound(m, selfIdx, n, oTag, nTag, oldSelf, newSelf, sessionID, wc, apply); err != nil {
			select {
			case rerr <- err:
			default:
			}
		}
	}

	out := party.Out()
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-out:
			if !ok {
				return
			}
			emit(m)
		case <-stop:
			drainOutbound(out, emit)
			return
		}
	}
}

func reshareTagFor(idx int) string { return fmt.Sprintf("%d", idx+1) }

// reshareEmitOutbound serializes one outbound reshare message with the
// committee-prefixed From / To tags and either ships it over the wire or
// applies it locally (same-device cross-committee). The wire layout is
// identical to internal/cli/mpcnet.go / mpcnet.routeReshareOut, so DM-5 PC
// CLI and DM-3 mobile bridge round-trip the same bytes: a broadcast emits
// one per-destination envelope (committee-marker preserved on every To tag)
// rather than a single bare-broadcast envelope, because the marker is what
// the receiver routes on.
func reshareEmitOutbound(
	m tss.Message,
	selfIdx, n int,
	oTag, nTag string,
	oldSelf, newSelf *tss.PartyID,
	sessionID string,
	wc WireCallbacks,
	apply func(parsed tss.ParsedMessage, mm *contract.MpcMessage),
) error {
	bz, _, err := m.WireBytes()
	if err != nil {
		return fmt.Errorf("wire bytes: %w", err)
	}
	from := m.GetFrom()
	wireFrom := nTag
	selfFrom := newSelf
	if from != nil && oldSelf != nil && from.KeyInt().Cmp(oldSelf.KeyInt()) == 0 {
		wireFrom = oTag
		selfFrom = oldSelf
	}
	isB := m.IsBroadcast() || m.GetTo() == nil
	toOld := m.IsToOldCommittee() || m.IsToOldAndNewCommittees()
	toNew := !m.IsToOldCommittee() || m.IsToOldAndNewCommittees()
	marker := "N"
	switch {
	case toOld && toNew:
		marker = "B"
	case toOld:
		marker = "O"
	}

	dests := map[int]bool{}
	if isB {
		for i := 0; i < n; i++ {
			dests[i] = true
		}
	} else {
		for _, d := range m.GetTo() {
			if d == nil {
				continue
			}
			// reshare PartyIDs carry the bare 1-based index in their Id
			// (no committee prefix in tss.PartyID.Id), so parseTagIndex
			// returns the right 0-based slot.
			if idx := parseTagIndex(d.Id); idx >= 0 {
				dests[idx] = true
			}
		}
	}

	// Per-destination directed envelopes. Each To entry is committee-marker
	// prefixed so the receiver routes to the correct committee party.
	// Same-device cross-committee deliveries are applied in-process (libp2p
	// cannot dial self; mpcnet.routeReshareOut uses the same trick).
	for d := range dests {
		if d == selfIdx {
			parsed, perr := tss.ParseWireMessage(bz, selfFrom, isB)
			if perr != nil {
				return fmt.Errorf("reshare self-parse: %w", perr)
			}
			selfMM := contract.MpcMessage{
				Version:     contract.MpcVersionV1,
				SessionID:   sessionID,
				From:        wireFrom,
				To:          []string{marker + reshareTagFor(selfIdx)},
				IsBroadcast: isB,
				Payload:     bz,
			}
			apply(parsed, &selfMM)
			continue
		}
		mm := contract.MpcMessage{
			Version:     contract.MpcVersionV1,
			SessionID:   sessionID,
			From:        wireFrom,
			To:          []string{marker + reshareTagFor(d)},
			IsBroadcast: isB,
			Payload:     bz,
		}
		if err := emitJSON(mm, wc); err != nil {
			return err
		}
	}
	return nil
}

// emitJSON marshals one MpcMessage and forwards it to the host wire callback.
func emitJSON(mm contract.MpcMessage, wc WireCallbacks) error {
	out, err := json.Marshal(mm)
	if err != nil {
		return fmt.Errorf("marshal MpcMessage: %w", err)
	}
	wc.OnWireMessage(out)
	return nil
}

// parseTagIndex returns the 0-based index encoded in a 1-based decimal party
// tag ("1".."n"). Returns -1 on a malformed tag. Mirrors mpcnet.tagIndex.
func parseTagIndex(tag string) int {
	n := 0
	for _, c := range tag {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return -1
	}
	return n - 1
}
