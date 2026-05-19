package coord

import (
	"net/http"
)

// P6 anti-abuse hardening (docs/design/security.md §5 "denial of service" row, server/server.md
// C7/C8). X-001 only rate-limited the self-attesting /v1/groups* endpoints; P6
// extends defence-in-depth to the external (A) and member (B) surfaces and
// strengthens external-service auth. Nothing here changes the C1-C10 semantics
// or the cryptographic invariants — it only sheds abusive load earlier and
// fails closed on a malformed external-auth context.
//
// All limiters are the same fixed-window rateLimiter X-001 introduced, driven
// by the injected coord clock so tests are deterministic. Defaults are
// generous (normal members/services never trip them); the limits are tunable
// via Options for tests and tighter deployments.

const (
	// extDefaultMax/extWindow bound external-service requests per client IP.
	// The window also throttles API-key brute force: the limiter counts every
	// attempt before auth runs, so a guessing client is rate-capped.
	extDefaultMax = 600
	// memberDefaultMax bounds member (B) requests per client IP within the same
	// window. It is keyed by IP, not by the claimed memberId: the memberId
	// header is unauthenticated at the gate, so keying by it would let an
	// attacker spoof a victim's id and exhaust that member's budget
	// (amplification). Per-IP shedding bounds an abusive origin while the
	// identity-sig gate still rejects forged content.
	memberDefaultMax = 600
)

// extGate rate-limits the external (A) surface per client IP and is wrapped
// outside extAuth so an unauthenticated flood is shed before the constant-time
// key compare runs (DoS + brute-force throttle, docs/design/security.md §5).
func (c *Coord) extGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !c.externalRL.allow(clientIP(r), c.clock.Now()) {
			c.writeErr(w, errRateLimited("too many external requests"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// memberAllowed reports whether a member (B) request from ip is within the
// per-IP budget. memberGate calls it before the EC signature verify, so an
// abusive origin is shed cheaply and a spoofed memberId cannot target a
// victim's budget (see memberDefaultMax).
func (c *Coord) memberAllowed(ip string) bool {
	return c.memberRL.allow(ip, c.clock.Now())
}

// checkExternalAuth is the hardened external-service auth gate (api.md A1,
// docs/design/server/server.md C7). It is a pure decision so it is unit-testable
// without a live TLS listener.
//
//   - api_key: the presented X-API-Key must constant-time match the configured
//     key. An empty/absent header is an explicit reject (fail-closed).
//   - mtls: TLS termination/verification is the listener's job (cmd/node owns
//     the listener; out of this package). coord adds a defence-in-depth check:
//     when the request actually carries TLS state, it MUST present a verified
//     client certificate chain — a TLS connection with no/again-unverified
//     client cert is rejected rather than silently trusted. When r.TLS is nil
//     (a reverse proxy already terminated mTLS and forwards plaintext) coord
//     cannot re-derive the peer and defers to that deployment boundary, as
//     documented in server/server.md (mTLS is a transport concern).
func (c *Coord) checkExternalAuth(r *http.Request) error {
	switch c.cfg.ExternalAuth {
	case authAPIKey:
		k := r.Header.Get("X-API-Key")
		if k == "" {
			return errUnauthenticated("missing api key")
		}
		return c.checkAPIKey(k)
	case authMTLS:
		if r.TLS != nil && len(r.TLS.VerifiedChains) == 0 {
			return errUnauthenticated("mtls: no verified client certificate")
		}
		return nil
	default:
		return nil
	}
}
