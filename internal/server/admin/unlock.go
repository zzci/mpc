package admin

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/zzci/mpc/internal/server/coorddb"
)

// unlockGuard rate-limits unlock attempts with exponential backoff so a leaked
// .db file cannot be brute-forced through the API (admin.md §8, database.md §7
// "失败限速 + 退避防爆破"). It tracks consecutive failures and the earliest
// time the next attempt is allowed; a success resets it. Backoff constants
// mirror the coord unlock loop (500ms base, 30s cap).
type unlockGuard struct {
	// inflight serializes the whole reserve→Unlock→settle sequence so
	// concurrent attempts cannot slip past the backoff window between the
	// reserve check and settle (TOCTOU); unlock is admin-only and rare, and
	// the Argon2id derivation already dominates latency, so serializing is
	// free in practice and tightens the anti-brute-force guarantee
	// (admin.md §8 "防爆破").
	inflight sync.Mutex

	mu          sync.Mutex
	failures    int
	nextAllowed time.Time
}

const (
	unlockBackoffBase = 500 * time.Millisecond
	unlockBackoffCap  = 30 * time.Second
	// maxUnlockFailures is the consecutive-failure count past which a
	// sustained brute-force is assumed: the backoff is already pinned at the
	// cap by then, but crossing it raises a distinct high-severity alarm so
	// the attempt pattern is operator-visible (admin.md §8 "防爆破",
	// security.md §5 "解锁尝试限速").
	maxUnlockFailures = 5
)

// reserve reports whether an attempt is permitted now; if not it returns the
// remaining wait. It does not record the outcome — call settle afterwards.
func (g *unlockGuard) reserve(now time.Time) (time.Duration, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if now.Before(g.nextAllowed) {
		return g.nextAllowed.Sub(now), false
	}
	return 0, true
}

// settle records the attempt outcome, arms the next backoff window, and
// returns the consecutive-failure count after this attempt (0 on success) so
// the caller can raise the sustained-brute-force alarm.
func (g *unlockGuard) settle(now time.Time, ok bool) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ok {
		g.failures = 0
		g.nextAllowed = time.Time{}
		return 0
	}
	g.failures++
	backoff := unlockBackoffBase << min(g.failures-1, 16)
	if backoff > unlockBackoffCap || backoff <= 0 {
		backoff = unlockBackoffCap
	}
	g.nextAllowed = now.Add(backoff)
	return g.failures
}

type unlockBody struct {
	Passphrase string `json:"passphrase"`
}

// hUnlock derives the whole-DB key from the operator passphrase and mounts the
// store (admin.md §8: strong-auth + passphrase → Argon2id → mount). The
// passphrase arrives only here (never config/env/KMS), is copied to a byte
// buffer that is zeroized after the attempt regardless of outcome, and is
// never logged or audited. Residual: the transient JSON string lives until GC
// (same documented by-design class as the coorddb DSN-key copy — defends
// offline file theft, not in-process forensics).
func (s *Server) hUnlock(w http.ResponseWriter, r *http.Request) {
	var b unlockBody
	if !s.readJSON(w, r, &b) {
		return
	}
	if b.Passphrase == "" {
		s.writeErr(w, errBadRequest("missing passphrase"))
		return
	}
	// Non-blocking: a second unlock arriving while one is in flight is
	// rejected outright instead of queueing. Blocking would let an attacker
	// pile up goroutines that each force an Argon2id derivation, amplifying
	// brute-force and memory pressure past the backoff window (admin.md §8
	// "防爆破" concurrency hardening).
	if !s.unlock.inflight.TryLock() {
		s.log.Warn("admin unlock rejected: another unlock in progress", "src", clientIP(r))
		s.writeErr(w, errRateLimited("an unlock attempt is already in progress"))
		return
	}
	defer s.unlock.inflight.Unlock()

	now := s.now()
	if wait, ok := s.unlock.reserve(now); !ok {
		s.log.Warn("admin unlock rate-limited", "src", clientIP(r), "retry_after_ms", wait.Milliseconds())
		s.writeErr(w, errRateLimited("too many unlock attempts; backing off"))
		return
	}

	pass := []byte(b.Passphrase)
	err := s.store.Unlock(r.Context(), pass)
	zeroize(pass)

	if err != nil && errors.Is(err, coorddb.ErrUnlocked) {
		// Already mounted: treat as success, no state change, no backoff.
		s.unlock.settle(now, true)
		s.writeJSON(w, http.StatusOK, map[string]any{"locked": false, "changed": false})
		return
	}
	if err != nil {
		fails := s.unlock.settle(now, false)
		// LOCKED → cannot write admin_audit; the attempt is recorded to the
		// process log/metrics only (admin.md §8, database.md §7).
		s.log.Warn("admin unlock attempt failed", "src", clientIP(r), "consecutive_failures", fails)
		if fails >= maxUnlockFailures {
			s.log.Error("admin unlock SUSTAINED BRUTE-FORCE: backoff pinned at cap",
				"src", clientIP(r), "consecutive_failures", fails,
				"backoff_cap_ms", unlockBackoffCap.Milliseconds())
		}
		s.writeErr(w, errUnauthorized("unlock failed (bad passphrase or corrupt db)"))
		return
	}
	s.unlock.settle(now, true)
	// Now UNLOCKED: back-fill the success into admin_audit (admin.md §8
	// "解锁成功后补记 admin_audit"). A non-fatal audit error is logged.
	if aerr := s.audit(r.Context(), r, scopeControl, "db.unlock", nil); aerr != nil {
		s.log.Error("admin unlock audit append failed", "err", aerr.Error())
	}
	s.log.Info("admin unlocked coord store", "src", clientIP(r))
	s.writeJSON(w, http.StatusOK, map[string]any{"locked": false, "changed": true})
}

// hRelock zeroizes the in-memory key and unmounts the store (admin.md §8
// relock). The audit row is written while still UNLOCKED (it cannot be written
// after relock); an already-LOCKED store is an idempotent no-op.
func (s *Server) hRelock(w http.ResponseWriter, r *http.Request) {
	if !s.store.IsUnlocked() {
		s.writeJSON(w, http.StatusOK, map[string]any{"locked": true, "changed": false})
		return
	}
	if aerr := s.audit(r.Context(), r, scopeControl, "db.relock", nil); aerr != nil {
		// Best-effort: still relock (fail-closed bias) but report the audit
		// gap rather than silently dropping it.
		s.log.Error("admin relock audit append failed", "err", aerr.Error())
	}
	if err := s.store.Relock(); err != nil {
		s.writeErr(w, asAPIError(err))
		return
	}
	s.log.Info("admin relocked coord store", "src", clientIP(r))
	s.writeJSON(w, http.StatusOK, map[string]any{"locked": true, "changed": true})
}

// hLockStatus reports only the lock boolean (admin.md §8 "查看锁定状态"). It is
// reachable under LOCKED and leaks no data.
func (s *Server) hLockStatus(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"locked": !s.store.IsUnlocked()})
}

// zeroize wipes a secret byte buffer (defense in depth; coorddb also does not
// retain the passphrase).
func zeroize(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
