package mobileapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/bnb-chain/tss-lib/v3/tss"

	"github.com/zzci/mpc/internal/contract"
	"github.com/zzci/mpc/internal/coordclient"
	"github.com/zzci/mpc/internal/mpc"
	"github.com/zzci/mpc/internal/txdecode"
)

// SignCallback is the Go→host channel of a signing session. Per DREV-001 D4-1
// it carries ONLY notifications: the host's Approve/Reject decision is a
// host→Go call on the returned *SignSession, not a callback method, so the two
// directions are never conflated (docs/design/mcp/sdk.md §2, fixes the sketch where
// Approve/Reject were drawn inside the callback).
//
// Ordering contract: OnDecoded fires at most once, only after every security
// check passed; OnResult/OnError fire exactly once and always last. On any
// security-class failure OnError fires and OnDecoded never does — the flow
// hard-rejects without entering MPC (docs/design/mcp/sdk.md §3/§5).
type SignCallback interface {
	// OnDecoded delivers the digest-bound A-zone facts, the optional B-zone
	// businessInfo, and the A/B declarative mismatches, each as JSON, for the
	// human WYSIWYS review.
	OnDecoded(aFactsJSON string, bInfoJSON string, mismatchJSON string)
	// OnResult delivers the final 65-byte [V+27‖R‖S] signature.
	OnResult(rsv []byte)
	// OnError reports a terminal failure as a stable {code,msg} pair.
	OnError(code string, msg string)
}

// SignSession is the opaque handle returned by Sign. Its Approve/Reject
// methods are the host→Go reverse entry points (DREV-001 D4-1): the host
// calls one exactly once after OnDecoded to drive the human decision back
// into the Go-side flow. Returned by pointer; never crosses the bridge by
// value.
type SignSession struct {
	mu       sync.Mutex
	decided  bool
	decision chan bool // buffered(1): true=approve, false=reject
}

func newSignSession() *SignSession {
	return &SignSession{decision: make(chan bool, 1)}
}

// Approve records the human's approval (host→Go). Calling it before OnDecoded,
// or more than once, or after the session already concluded, is a safe no-op.
func (ss *SignSession) Approve() { ss.decide(true) }

// Reject records the human's rejection (host→Go). Same idempotency as Approve.
func (ss *SignSession) Reject() { ss.decide(false) }

func (ss *SignSession) decide(approve bool) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.decided {
		return
	}
	ss.decided = true
	ss.decision <- approve
}

// signConfig is the configJSON schema for Sign under DM-3 (hard-cut). The
// distributed-mpc-impl.md §B DM-3 envelope wraps the coord-delivered
// StartSigning so partyIndex / n / t / memberSet / relay / role accompany
// the signed envelope on the same call site. SessionID MUST equal
// Start.RequestID so the R5 gate (sessionId isolation) is consistent with
// the WYSIWYS invariant.
type signConfig struct {
	GroupID    *string                `json:"groupId"`
	SessionID  *string                `json:"sessionID"`
	PartyIndex *int                   `json:"partyIndex"`
	N          *int                   `json:"n"`
	T          *int                   `json:"t"`
	MemberSet  []string               `json:"memberSet"`
	Relay      *relayConfig           `json:"relay"`
	Role       *string                `json:"role"`
	Start      *contract.StartSigning `json:"start"`
}

// validate enforces the DM-3 hard-cut for sign. Same posture as keygen /
// reshare: any missing required field is CodeBadConfig.
func (cfg signConfig) validate() error {
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
	case cfg.Start == nil:
		return fmt.Errorf("missing start")
	case *cfg.N < 2:
		return fmt.Errorf("n must be >= 2, got %d", *cfg.N)
	case *cfg.T < 1 || *cfg.T >= *cfg.N:
		return fmt.Errorf("need 1 <= t < n, got t=%d n=%d", *cfg.T, *cfg.N)
	case *cfg.PartyIndex < 0 || *cfg.PartyIndex >= *cfg.N:
		return fmt.Errorf("partyIndex %d out of range [0,%d)", *cfg.PartyIndex, *cfg.N)
	case len(cfg.MemberSet) != *cfg.N:
		return fmt.Errorf("memberSet length %d != n=%d", len(cfg.MemberSet), *cfg.N)
	case *cfg.SessionID != cfg.Start.RequestID:
		return fmt.Errorf("sessionID %q must equal start.requestId %q", *cfg.SessionID, cfg.Start.RequestID)
	}
	return nil
}

// participantsFor maps the start.Signers memberId set onto 0-based party
// indices via the configured memberSet. Every signer must be present in the
// memberSet and the local device's partyIndex must itself be a signer (else
// the device should not have received this Start).
func (cfg signConfig) participantsFor() ([]int, error) {
	idx := make(map[string]int, len(cfg.MemberSet))
	for i, m := range cfg.MemberSet {
		idx[m] = i
	}
	out := make([]int, 0, len(cfg.Start.Signers))
	selfIn := false
	for _, mid := range cfg.Start.Signers {
		i, ok := idx[mid]
		if !ok {
			return nil, fmt.Errorf("signer %q not in memberSet", mid)
		}
		out = append(out, i)
		if i == *cfg.PartyIndex {
			selfIn = true
		}
	}
	if !selfIn {
		return nil, fmt.Errorf("this device's partyIndex %d is not in start.Signers", *cfg.PartyIndex)
	}
	if len(out) < *cfg.T+1 {
		return nil, fmt.Errorf("need at least t+1=%d signers, got %d", *cfg.T+1, len(out))
	}
	return out, nil
}

// Sign runs the device-side WYSIWYS signing flow on a background goroutine
// against the host-supplied wire transport and returns a session handle
// immediately. configJSON is the DM-3 envelope wrapping the coord-delivered
// StartSigning + this device's session metadata; the flow
// (docs/design/mcp/sdk.md §3) is, in order:
//
//	hard-cut configJSON → verify envelope (version, metaHash, proposerSig,
//	requestId consistency) → check not expired → tx-decode + recompute digest
//	== digest32 → OnDecoded (human review) → host Approve/Reject → re-check
//	not expired → single-party MPC sign (drive mpc.SignParty over the host
//	wire) → re-check not expired → OnResult.
//
// Any security-class failure hard-rejects via OnError and never enters MPC.
func (s *SDK) Sign(configJSON string, wire WireCallbacks, cb SignCallback) *SignSession {
	ss := newSignSession()
	go s.runSign(configJSON, wire, cb, ss)
	return ss
}

func (s *SDK) runSign(configJSON string, wire WireCallbacks, cb SignCallback, ss *SignSession) {
	var cfg signConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		cb.OnError(CodeBadConfig, fmt.Sprintf("invalid configJSON: %v", err))
		return
	}
	if err := cfg.validate(); err != nil {
		cb.OnError(CodeBadConfig, err.Error())
		return
	}
	if wire == nil {
		cb.OnError(CodeBadConfig, "WireCallbacks is required for sign")
		return
	}
	st := cfg.Start

	// 1. Envelope verification: version + metaHash + proposerSig + requestId
	// consistency, with the coord self-describing proposer-key convention.
	if err := coordclient.VerifyStartSelfDescribing(st); err != nil {
		cb.OnError(codeFor(err), fmt.Sprintf("envelope verification failed: %v", err))
		return
	}

	// 2. Expiry. WYSIWYS mandates an enforceable expiry re-checked before and
	// after MPC (docs/design/mcp/sdk.md §3/§5); coordclient.NotExpired treats a
	// non-positive expiry as "never expires", so an envelope without a
	// positive expiry would defeat that guard *and* leave the decision wait
	// (and its session-table entry) unbounded on an attacker-controlled
	// START. Require a positive expiry, then check it.
	if st.Envelope.Expiry <= 0 {
		cb.OnError(CodeInvalidEnvelope, "envelope has no positive expiry")
		return
	}
	if !coordclient.NotExpired(st, time.Now().UnixMilli()) {
		cb.OnError(CodeExpired, "request expired before review")
		return
	}

	// 3. tx-decode security gate: recompute the chain digest and assert it
	// equals digest32. Any error is a hard rejection with no facts surfaced
	// (docs/design/mcp/sdk.md §3/§5, docs/design/contract/protocol.md:25).
	res, err := txdecode.New().Decode(&st.Envelope)
	if err != nil {
		cb.OnError(codeFor(err), fmt.Sprintf("tx-decode rejected: %v", err))
		return
	}

	// 4. Human review: surface the digest-bound A-zone, the B-zone, and the
	// A/B mismatches as JSON.
	aFacts, err := json.Marshal(res.Facts)
	if err != nil {
		cb.OnError(CodeInternal, fmt.Sprintf("marshal facts: %v", err))
		return
	}
	bInfo, err := json.Marshal(st.Envelope.BusinessInfo)
	if err != nil {
		cb.OnError(CodeInternal, fmt.Sprintf("marshal businessInfo: %v", err))
		return
	}
	mism, err := json.Marshal(res.Mismatches)
	if err != nil {
		cb.OnError(CodeInternal, fmt.Sprintf("marshal mismatches: %v", err))
		return
	}

	s.registerSession(st.RequestID, ss)
	defer s.unregisterSession(st.RequestID)

	cb.OnDecoded(string(aFacts), string(bInfo), string(mism))

	// 5. Await the host→Go decision, bounded by the request lifetime so a
	// silent host cannot pin a session past expiry.
	approved, ok := waitDecision(ss, decisionDeadline(st))
	if !ok {
		cb.OnError(CodeExpired, "no decision before deadline")
		return
	}
	if !approved {
		cb.OnError(CodeRejected, "rejected by reviewer")
		return
	}

	// 6. Re-check expiry before entering MPC (docs/design/mcp/sdk.md §3).
	if !coordclient.NotExpired(st, time.Now().UnixMilli()) {
		cb.OnError(CodeExpired, "request expired after approval")
		return
	}

	// 7. Single-party MPC sign: drive this device's mpc.SignParty over the
	// host-owned wire (Go→host outbound; OnWireMessage host→Go inbound).
	// participants are the start.Signers mapped through memberSet to 0-based
	// indices; this device contributes only its own share_i (DM-3).
	share, threshold, _, _, hasShare := s.snapshotShareForGroup(*cfg.GroupID)
	if !hasShare {
		cb.OnError(CodeNoShares, "no key share held for this group")
		return
	}
	if threshold != *cfg.T {
		cb.OnError(CodeBadConfig, fmt.Sprintf("config threshold %d disagrees with held share threshold %d", *cfg.T, threshold))
		return
	}
	participants, err := cfg.participantsFor()
	if err != nil {
		cb.OnError(CodeBadConfig, err.Error())
		return
	}

	sig, err := s.runSignParty(*cfg.SessionID, *cfg.PartyIndex, threshold, participants, share, st.Envelope.Digest32, wire)
	if err != nil {
		cb.OnError(CodeInternal, fmt.Sprintf("mpc signing failed: %v", err))
		return
	}

	// 8. Final expiry re-check before releasing the signature
	// (docs/design/mcp/sdk.md §3: re-check not-expired before producing the result).
	if !coordclient.NotExpired(st, time.Now().UnixMilli()) {
		cb.OnError(CodeExpired, "request expired before result release")
		return
	}
	cb.OnResult(sig.Compact())
}

// runSignParty drives this device's mpc.SignParty against wire and returns
// the joint {R,S,V}. The single-party engine sees only this device's share;
// the wire pump fans outbound messages out via wire and routes the
// OnWireMessage feed back into the active session.
func (s *SDK) runSignParty(
	sessionID string,
	partyIndex, threshold int,
	participants []int,
	share mpc.Share,
	digest []byte,
	wire WireCallbacks,
) (mpc.Signature, error) {
	ctx := context.Background()
	party, err := mpc.NewSignParty(ctx, mpc.SinglePartySignConfig{
		SessionID:    sessionID,
		Threshold:    threshold,
		PartyIndex:   partyIndex,
		Participants: participants,
		Share:        share,
		Digest:       digest,
	})
	if err != nil {
		return mpc.Signature{}, err
	}

	self := party.PartyID().Id
	pids := party.Peers()
	apply := func(parsed tss.ParsedMessage, _ *contract.MpcMessage) {
		_ = party.Update(parsed)
	}

	ws := &wireSession{
		sessionID: sessionID,
		self:      self,
		resolveFn: pidsResolver(pids),
		applyFn:   apply,
	}
	if !s.installSession(ws) {
		return mpc.Signature{}, fmt.Errorf("another MPC session is already active on this SDK")
	}
	defer s.removeSession(sessionID)

	stop := make(chan struct{})
	rerr := make(chan error, 1)
	pumpCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	pump := &wirePump{
		sdk:       s,
		sessionID: sessionID,
		self:      self,
		peerTags:  tagsOf(pids),
		wc:        wire,
		outCh:     party.Out(),
	}
	pump.start(pumpCtx, stop, rerr)

	if err := party.Start(); err != nil {
		close(stop)
		return mpc.Signature{}, fmt.Errorf("start sign: %w", err)
	}

	sig, doneErr := party.Done(ctx)
	close(stop)
	if doneErr != nil {
		return mpc.Signature{}, doneErr
	}
	select {
	case err := <-rerr:
		return mpc.Signature{}, fmt.Errorf("wire pump: %w", err)
	default:
	}
	return sig, nil
}

// decisionDeadline is the earliest of the envelope expiry and the dispatch
// deadline (unix ms); zero means unbounded (no deadline declared).
func decisionDeadline(st *contract.StartSigning) int64 {
	d := st.Envelope.Expiry
	if st.Deadline > 0 && (d == 0 || st.Deadline < d) {
		d = st.Deadline
	}
	return d
}

// waitDecision blocks until the host calls Approve/Reject or deadlineMs (unix
// ms) passes. ok is false on timeout.
func waitDecision(ss *SignSession, deadlineMs int64) (approved, ok bool) {
	if deadlineMs <= 0 {
		return <-ss.decision, true
	}
	d := time.Until(time.UnixMilli(deadlineMs))
	if d <= 0 {
		return false, false
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case a := <-ss.decision:
		return a, true
	case <-t.C:
		return false, false
	}
}
