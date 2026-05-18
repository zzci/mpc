package coord

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// rateLimiter is a fixed-window per-key limiter. S-002 §6.3/§8 require
// rate-limiting the self-attesting /v1/groups* endpoints against registration
// storms (transport auth is intentionally absent there); this is the minimal
// 429 RATE_LIMITED guard, keyed by client IP. Heavier abuse controls are H-002.
type rateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	max    int
	window time.Duration
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{hits: map[string][]time.Time{}, max: max, window: window}
}

// allow records a hit for key and reports whether it is within the window
// budget. Expired timestamps are pruned on access.
func (rl *rateLimiter) allow(key string, now time.Time) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cut := now.Add(-rl.window)
	kept := rl.hits[key][:0]
	for _, t := range rl.hits[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= rl.max {
		rl.hits[key] = kept
		return false
	}
	rl.hits[key] = append(kept, now)
	return true
}

// rateGate wraps a handler with the provisioning rate limiter.
func (c *Coord) rateGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !c.provisionRL.allow(ip, c.clock.Now()) {
			c.writeErr(w, errRateLimited("too many provisioning requests"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
