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

// KeyGenCallback receives keygen progress and the terminal outcome. Every
// method is a Go→host call carrying only flat types (gomobile constraint,
// docs/design/mcp/sdk.md §2). Exactly one of OnResult / OnError fires, always after
// any OnProgress calls (callback ordering contract).
type KeyGenCallback interface {
	// OnProgress reports a coarse stage label ("preparams", "computing",
	// "sealing"); PreParams/MPC run off the UI thread (docs/design/mcp/sdk.md §5).
	OnProgress(stage string)
	// OnResult delivers a small JSON summary
	// {"threshold","parties","partyIndex","moniker","groupPubKey":"<hex>"}; the
	// device's share_i stays Go-side, sealed in the keystore (distributed-mpc-
	// impl.md §B DM-3: only this device's share_i is produced).
	OnResult(summaryJSON string)
	// OnError reports a terminal failure as a stable {code,msg} pair.
	OnError(code string, msg string)
}

// relayConfig carries the host's libp2p relay coordinates for one MPC
// session. The fields are pass-through to the host's transport — the SDK
// itself never dials, but treats them as part of the mandatory configJSON
// envelope so the schema is the same whether the device runs against the
// PC-CLI libp2p host (DM-5) or the mobile native bridge.
type relayConfig struct {
	PeerID string   `json:"peerID"`
	Addrs  []string `json:"addrs"`
}

// keygenConfig is the configJSON schema for KeyGen under DM-3 (hard-cut):
// every new field is mandatory and the SDK rejects an old configJSON missing
// any of them with CodeBadConfig. Pointer fields are used wherever the zero
// value is a valid datum (PartyIndex=0 is the first party; T=0 must fail-
// loud), so field absence is unambiguously detectable.
type keygenConfig struct {
	GroupID    *string      `json:"groupId"`
	SessionID  *string      `json:"sessionID"`
	PartyIndex *int         `json:"partyIndex"`
	N          *int         `json:"n"`
	T          *int         `json:"t"`
	MemberSet  []string     `json:"memberSet"`
	Relay      *relayConfig `json:"relay"`
	Role       *string      `json:"role"`
	Passphrase string       `json:"passphrase"`
}

// keygenSummary is the OnResult payload. Single-party (DM-3): one moniker.
type keygenSummary struct {
	Threshold   int    `json:"threshold"`
	Parties     int    `json:"parties"`
	PartyIndex  int    `json:"partyIndex"`
	Moniker     string `json:"moniker"`
	GroupPubKey string `json:"groupPubKey"`
}

// validate enforces the DM-3 hard-cut: every required field must be present
// and structurally valid. The returned message names the missing field so
// the host can pinpoint a regression without diffing schemas.
func (cfg keygenConfig) validate() error {
	switch {
	case cfg.GroupID == nil || *cfg.GroupID == "":
		return fmt.Errorf("missing groupId")
	case cfg.SessionID == nil || *cfg.SessionID == "":
		return fmt.Errorf("missing sessionID")
	case cfg.PartyIndex == nil:
		return fmt.Errorf("missing partyIndex")
	case cfg.N == nil:
		return fmt.Errorf("missing n")
	case cfg.T == nil:
		return fmt.Errorf("missing t")
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
	case *cfg.T < 1 || *cfg.T >= *cfg.N:
		return fmt.Errorf("need 1 <= t < n, got t=%d n=%d", *cfg.T, *cfg.N)
	case *cfg.PartyIndex < 0 || *cfg.PartyIndex >= *cfg.N:
		return fmt.Errorf("partyIndex %d out of range [0,%d)", *cfg.PartyIndex, *cfg.N)
	case len(cfg.MemberSet) != *cfg.N:
		return fmt.Errorf("memberSet length %d != n=%d", len(cfg.MemberSet), *cfg.N)
	case cfg.Passphrase == "":
		return fmt.Errorf("passphrase must not be empty")
	}
	return nil
}

// KeyGen runs THIS device's share of a distributed t-of-n ECDSA threshold
// keygen on a background goroutine: it drives one mpc.KeygenParty (DM-1)
// against the host-supplied wire callback (Go→host outbound) and the
// receive-side OnWireMessage feed (host→Go inbound, R5-gated, sdk.md §3).
// On success the device's share_i is sealed to the keystore and the OnResult
// summary names only the local moniker (distributed-mpc-impl.md §B DM-3:
// only this device's share_i is produced).
//
// configJSON is mandatory and hard-cut: an old configJSON lacking any of
// {groupId, sessionID, partyIndex, n, t, memberSet, relay{peerID,addrs[]},
// role} fails with CodeBadConfig before any work begins. In production each
// device generates its own PreParams locally (RED LINE: a backend MUST NOT
// pre-generate or push them, mpc.KeygenConfig.PreParams).
func (s *SDK) KeyGen(configJSON string, wire WireCallbacks, cb KeyGenCallback) {
	var cfg keygenConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		cb.OnError(CodeBadConfig, fmt.Sprintf("invalid configJSON: %v", err))
		return
	}
	if err := cfg.validate(); err != nil {
		cb.OnError(CodeBadConfig, err.Error())
		return
	}
	if wire == nil {
		cb.OnError(CodeBadConfig, "WireCallbacks is required for keygen")
		return
	}
	go s.runKeyGen(cfg, wire, cb)
}

func (s *SDK) runKeyGen(cfg keygenConfig, wire WireCallbacks, cb KeyGenCallback) {
	ctx := context.Background()
	cb.OnProgress("preparams")

	var pre *tsskeygen.LocalPreParams
	if len(s.preParams) > 0 {
		// Test seam (DM-3 single-party): each SDK consumes its own first
		// fixture entry; production keeps PreParams nil so the device
		// generates locally.
		p := s.preParams[0]
		pre = &p
	}

	party, err := mpc.NewKeygenParty(ctx, mpc.SinglePartyKeygenConfig{
		Threshold:  *cfg.T,
		Parties:    *cfg.N,
		PartyIndex: *cfg.PartyIndex,
		PreParams:  pre,
	})
	if err != nil {
		cb.OnError(CodeInternal, fmt.Sprintf("new keygen party: %v", err))
		return
	}

	self := party.PartyID().Id
	pids := party.Peers()
	apply := func(parsed tss.ParsedMessage, _ *contract.MpcMessage) {
		// Errors are dropped: tss-lib already wraps protocol violations and
		// the transport gate has already rejected malicious senders. Mirrors
		// internal/cli/mpcnet.go's applyInbound posture.
		_ = party.Update(parsed)
	}

	ws := &wireSession{
		sessionID: *cfg.SessionID,
		self:      self,
		resolveFn: pidsResolver(pids),
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
	pump := &wirePump{
		sdk:       s,
		sessionID: ws.sessionID,
		self:      self,
		peerTags:  tagsOf(pids),
		wc:        wire,
		outCh:     party.Out(),
	}
	pump.start(pumpCtx, stop, rerr)

	cb.OnProgress("computing")
	if err := party.Start(); err != nil {
		close(stop)
		cb.OnError(CodeInternal, fmt.Sprintf("start keygen: %v", err))
		return
	}

	share, doneErr := party.Done(ctx)
	close(stop)
	if doneErr != nil {
		cb.OnError(CodeInternal, fmt.Sprintf("keygen failed: %v", doneErr))
		return
	}
	select {
	case err := <-rerr:
		cb.OnError(CodeInternal, fmt.Sprintf("keygen wire pump: %v", err))
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
		cb.OnError(CodeInternal, fmt.Sprintf("seal share %q: %v", share.Moniker, err))
		return
	}

	s.setOwnShare(*cfg.GroupID, share, *cfg.T, *cfg.N, *cfg.PartyIndex, pubHex)

	out, err := json.Marshal(keygenSummary{
		Threshold:   *cfg.T,
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

// pidsResolver builds a fromTag→PartyID lookup over a deterministic peer set;
// the wireSession uses it on the receive path so OnWireMessage can parse
// inbound messages without each protocol re-deriving the committee.
func pidsResolver(pids tss.SortedPartyIDs) func(string) *tss.PartyID {
	return func(from string) *tss.PartyID {
		for _, p := range pids {
			if p.Id == from {
				return p
			}
		}
		return nil
	}
}
