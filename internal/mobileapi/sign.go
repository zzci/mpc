package mobileapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

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

// Sign runs the device-side WYSIWYS signing flow for a coord START message on
// a background goroutine and returns a session handle immediately. startJSON
// is the contract.StartSigning the coord delivered. The flow
// (docs/design/mcp/sdk.md §3) is, in order:
//
//	verify envelope (version, metaHash, proposerSig) and requestId consistency
//	→ check not expired → tx-decode + recompute digest == digest32
//	→ OnDecoded (human review) → host Approve/Reject
//	→ re-check not expired → MPC sign → re-check not expired → OnResult
//
// Any security-class failure hard-rejects via OnError and never enters MPC.
func (s *SDK) Sign(startJSON string, cb SignCallback) *SignSession {
	ss := newSignSession()
	go s.runSign(startJSON, cb, ss)
	return ss
}

func (s *SDK) runSign(startJSON string, cb SignCallback, ss *SignSession) {
	var st contract.StartSigning
	if err := json.Unmarshal([]byte(startJSON), &st); err != nil {
		cb.OnError(CodeBadConfig, fmt.Sprintf("invalid startJSON: %v", err))
		return
	}

	// 1. Envelope verification: version + metaHash + proposerSig + requestId
	// consistency, with the coord self-describing proposer-key convention.
	if err := coordclient.VerifyStartSelfDescribing(&st); err != nil {
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
	if !coordclient.NotExpired(&st, time.Now().UnixMilli()) {
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
	approved, ok := waitDecision(ss, decisionDeadline(&st))
	if !ok {
		cb.OnError(CodeExpired, "no decision before deadline")
		return
	}
	if !approved {
		cb.OnError(CodeRejected, "rejected by reviewer")
		return
	}

	// 6. Re-check expiry before entering MPC (docs/design/mcp/sdk.md §3).
	if !coordclient.NotExpired(&st, time.Now().UnixMilli()) {
		cb.OnError(CodeExpired, "request expired after approval")
		return
	}

	// 7. MPC signing over the in-process committee. Networked multi-device
	// signing via transport is wired by a separate task; here the digest-
	// bound signing itself is exercised end-to-end in-process.
	shares, threshold, hasShares := s.snapshotShares()
	if !hasShares {
		cb.OnError(CodeNoShares, "no key share held for this group")
		return
	}
	sig, err := mpc.Sign(context.Background(), mpc.SignConfig{
		SessionID: st.RequestID,
		Threshold: threshold,
		Shares:    shares,
		Digest:    st.Envelope.Digest32,
	})
	if err != nil {
		cb.OnError(CodeInternal, fmt.Sprintf("mpc signing failed: %v", err))
		return
	}

	// 8. Final expiry re-check before releasing the signature
	// (docs/design/mcp/sdk.md §3: re-check not-expired before producing the result).
	if !coordclient.NotExpired(&st, time.Now().UnixMilli()) {
		cb.OnError(CodeExpired, "request expired before result release")
		return
	}
	cb.OnResult(sig.Compact())
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
