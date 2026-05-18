package coord

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/zzci/mpc/internal/contract"
	"github.com/zzci/mpc/internal/server/coorddb"
)

// engine is the C5 quorum-initiation algorithm (docs/design/server/server.md C5),
// driven by events (ingest / approval / heartbeat) and a background sweep
// (expiry timers, dispatch timeout, signer-offline rollback). coord only emits
// START; it never enters the tss protocol.
type engine struct {
	c *Coord

	keys   sync.Mutex
	locked map[string]*sync.Mutex // per-request serialization

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// sweepInterval bounds how quickly expiry/timeout/rollback is detected when no
// event arrives. TTL/expiry is first-class (C6); this is the timer behind
// C6(a) and the dispatch-timeout / signer-offline guards.
const sweepInterval = time.Second

func newEngine(c *Coord) *engine {
	return &engine{c: c, locked: map[string]*sync.Mutex{}}
}

func (e *engine) start(ctx context.Context) {
	sctx, cancel := context.WithCancel(ctx)
	e.cancel = cancel
	e.wg.Add(1)
	go e.sweepLoop(sctx)
}

func (e *engine) stop() {
	if e.cancel != nil {
		e.cancel()
	}
	e.wg.Wait()
}

// lockFor returns the per-request mutex so concurrent events for the same
// request serialize (the DB status guard is the ultimate correctness
// guarantee; this just avoids redundant dispatch work).
func (e *engine) lockFor(requestID string) *sync.Mutex {
	e.keys.Lock()
	defer e.keys.Unlock()
	m, ok := e.locked[requestID]
	if !ok {
		m = &sync.Mutex{}
		e.locked[requestID] = m
	}
	return m
}

// evaluate re-assesses one request against the C5 algorithm. It is safe to
// call from any event path; benign races resolve via the transition guard.
func (e *engine) evaluate(ctx context.Context, requestID string) {
	m := e.lockFor(requestID)
	m.Lock()
	defer m.Unlock()

	r, err := e.c.db.loadRequest(ctx, requestID)
	if err != nil {
		if !errors.Is(err, coorddb.ErrLocked) {
			e.c.log.Error("engine load request", "requestId", requestID, "err", err.Error())
		}
		return
	}
	if isTerminal(r.Status) {
		return
	}

	// C6(a): expiry is checked on every state before anything else.
	if e.c.isExpired(r.ExpiryMs) {
		e.toTerminal(ctx, r.RequestID, r.Status, stExpired, nil, "expired")
		return
	}
	if r.Status != stPending {
		return
	}

	g, err := e.c.db.group(ctx, r.GroupID)
	if err != nil {
		e.c.log.Error("engine load group", "groupId", r.GroupID, "err", err.Error())
		return
	}
	members, err := e.c.db.members(ctx, r.GroupID)
	if err != nil {
		e.c.log.Error("engine load members", "groupId", r.GroupID, "err", err.Error())
		return
	}
	active := map[string]bool{}
	for _, mr := range members {
		if mr.Status == "active" {
			active[mr.MemberID] = true
		}
	}
	decisions, err := e.c.db.decisions(ctx, requestID)
	if err != nil {
		e.c.log.Error("engine load decisions", "requestId", requestID, "err", err.Error())
		return
	}

	// REJECTED when rejections make the threshold unreachable (api.md:50).
	rejected := 0
	for mid, d := range decisions {
		if d == "rejected" && active[mid] {
			rejected++
		}
	}
	if len(active)-rejected < g.ThresholdT {
		e.toTerminal(ctx, requestID, stPending, stRejected, nil, "threshold unreachable")
		return
	}

	online, err := e.c.presence.Online(ctx, r.GroupID)
	if err != nil {
		e.c.log.Error("engine presence", "groupId", r.GroupID, "err", err.Error())
		return
	}
	onlineSet := map[string]int64{}
	for _, o := range online {
		onlineSet[o.MemberID] = o.TS
	}

	// approvedOnline = online ∩ approved ∩ active group members (C5).
	type cand struct {
		id string
		ts int64
	}
	var cands []cand
	for mid := range active {
		if decisions[mid] != "approved" {
			continue
		}
		ts, ok := onlineSet[mid]
		if !ok {
			continue
		}
		cands = append(cands, cand{mid, ts})
	}
	if len(cands) < g.ThresholdT {
		return // not yet a quorum; a later event re-evaluates
	}

	// Signer selection: stable = sorted memberId; liveness = freshest
	// heartbeat first (docs/design/server/server.md C5, config quorum.signer_select).
	if e.c.cfg.SignerSelect == signerLiveness {
		sort.Slice(cands, func(i, j int) bool {
			if cands[i].ts != cands[j].ts {
				return cands[i].ts > cands[j].ts
			}
			return cands[i].id < cands[j].id
		})
	} else {
		sort.Slice(cands, func(i, j int) bool { return cands[i].id < cands[j].id })
	}
	signers := make([]string, g.ThresholdT)
	for i := 0; i < g.ThresholdT; i++ {
		signers[i] = cands[i].id
	}

	nowMs := unixMillis(e.c.clock.Now())
	if err := e.c.dispatchTx(ctx, requestID, signers, nowMs, nowISO(e.c)); err != nil {
		if !errors.Is(err, errConflict) {
			e.c.log.Error("engine dispatch", "requestId", requestID, "err", err.Error())
		}
		return
	}

	env, err := rebuildEnvelope(r)
	if err != nil {
		e.c.log.Error("engine rebuild envelope", "requestId", requestID, "err", err.Error())
		return
	}
	deadline, _ := e.c.dispatchDeadline(r.ExpiryMs)
	signerSet := map[string]bool{}
	for _, s := range signers {
		signerSet[s] = true
	}
	for _, mid := range signers {
		e.c.hub.publish(r.GroupID, contract.StartSigning{
			RequestID: requestID,
			Envelope:  *env,
			Signers:   signers,
			SelfRole:  signerSet[mid],
			Deadline:  unixMillis(deadline),
		})
	}
	e.c.notifier.NotifyDispatch(ctx, r.GroupID, signers)
	e.c.log.Info("dispatched", "requestId", requestID, "signers", len(signers))
}

// onGroupEvent re-evaluates every PENDING request of a group (heartbeat /
// approval fan-out).
func (e *engine) onGroupEvent(ctx context.Context, groupID string) {
	ids, err := e.c.db.pendingRequestIDs(ctx, groupID)
	if err != nil {
		return
	}
	for _, id := range ids {
		e.evaluate(ctx, id)
	}
}

// sweepLoop is the timer behind C6(a) expiry, the dispatch timeout, and the
// signer-offline rollback (docs/design/server/server.md C5).
func (e *engine) sweepLoop(ctx context.Context) {
	defer e.wg.Done()
	t := time.NewTicker(sweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.sweep(ctx)
		}
	}
}

func (e *engine) sweep(ctx context.Context) {
	if !e.c.store.IsUnlocked() {
		return // LOCKED: nothing readable; sweep resumes after unlock
	}
	active, err := e.c.db.activeRequestIDs(ctx)
	if err != nil {
		return
	}
	for _, id := range active {
		e.sweepActive(ctx, id)
	}
	// PENDING expiry / late-quorum is handled by re-evaluating each pending
	// request (C6(a) timer + event-miss safety net).
	pend, err := e.c.db.allPendingRequestIDs(ctx)
	if err != nil {
		return
	}
	for _, id := range pend {
		e.evaluate(ctx, id)
	}
}

// sweepActive handles a DISPATCHED/SIGNING request: expiry -> EXPIRED, dispatch
// timeout -> FAILED, any signer offline -> rollback to PENDING and re-evaluate.
func (e *engine) sweepActive(ctx context.Context, requestID string) {
	m := e.lockFor(requestID)
	m.Lock()
	defer m.Unlock()
	r, err := e.c.db.loadRequest(ctx, requestID)
	if err != nil || isTerminal(r.Status) {
		return
	}
	if r.Status != stDispatched && r.Status != stSigning {
		return
	}
	if e.c.isExpired(r.ExpiryMs) {
		e.toTerminal(ctx, requestID, r.Status, stExpired, nil, "expired")
		return
	}
	_, d := e.c.dispatchDeadline(r.ExpiryMs)
	if r.DispatchedAt > 0 {
		elapsed := time.Duration(unixMillis(e.c.clock.Now())-r.DispatchedAt) * time.Millisecond
		if elapsed >= d {
			e.toTerminal(ctx, requestID, r.Status, stFailed, nil, "dispatch timeout")
			return
		}
	}
	online, err := e.c.presence.Online(ctx, r.GroupID)
	if err != nil {
		return
	}
	onlineSet := map[string]bool{}
	for _, o := range online {
		onlineSet[o.MemberID] = true
	}
	for _, s := range decodeSigners(r.SignersJSON) {
		if !onlineSet[s] {
			// Signer dropped while < terminal and not expired -> re-schedule.
			if err := e.c.transition(ctx, requestID, r.Status, stPending,
				"coord", "signer offline, rescheduling"); err != nil && !errors.Is(err, errConflict) {
				e.c.log.Error("engine rollback", "requestId", requestID, "err", err.Error())
				return
			}
			e.evaluate(ctx, requestID)
			return
		}
	}
}

// toTerminal performs the guarded terminal transition and fires the mandatory
// external callback (docs/design/server/server.md C3:211, api.md A4). RETURNED is
// driven separately from the result path (it carries RSV).
func (e *engine) toTerminal(ctx context.Context, requestID, from, to string, rsv []byte, reason string) {
	if err := e.c.resultTx(ctx, requestID, from, to, rsv, reason, nowISO(e.c)); err != nil {
		if !errors.Is(err, errConflict) {
			e.c.log.Error("engine terminal", "requestId", requestID, "to", to, "err", err.Error())
		}
		return
	}
	e.c.reportTerminal(ctx, requestID, to, rsv, reason)
}

func nowISO(c *Coord) string { return c.clock.Now().UTC().Format(time.RFC3339Nano) }
