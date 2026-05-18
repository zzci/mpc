package admin

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// scope is the privilege a request must hold. admin.md §4 mandates separate
// read and control credentials even for a single administrator (least
// privilege; the control surface is a new attack surface).
type scope int

const (
	scopeRead    scope = iota // read-only queries
	scopeControl              // abuse controls + unlock/relock
)

// adminID extracts a stable, non-secret operator label for the admin_audit
// `admin_id` column (database.md §6). Priority: the StrongAuth-verified
// principal (cryptographically attributed, P6 §4) > the optional X-Admin-Id
// human label > the privilege-scope name. The bearer token itself is a secret
// and MUST NOT be logged or audited.
func adminID(r *http.Request, s scope) string {
	if p := principalFrom(r.Context()); p != "" {
		return p
	}
	if v := strings.TrimSpace(r.Header.Get("X-Admin-Id")); v != "" {
		return v
	}
	if s == scopeControl {
		return "admin:control"
	}
	return "admin:read"
}

// bearer pulls the token from the Authorization: Bearer <token> header.
func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(h) <= len(p) || !strings.EqualFold(h[:len(p)], p) {
		return ""
	}
	return h[len(p):]
}

// authorize enforces the scope. A control token also satisfies a read scope
// (control implies read); a read token never satisfies control. Comparison is
// constant-time (mirrors coord.checkAPIKey) so a token is not discoverable by
// timing. On failure it writes the error envelope and returns false.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request, want scope) bool {
	tok := bearer(r)
	if tok == "" {
		s.writeErr(w, errUnauthorized("missing bearer token"))
		return false
	}
	ctrl := subtle.ConstantTimeCompare([]byte(tok), []byte(s.cfg.ControlToken)) == 1
	read := subtle.ConstantTimeCompare([]byte(tok), []byte(s.cfg.ReadToken)) == 1
	switch want {
	case scopeControl:
		if !ctrl {
			s.writeErr(w, errForbidden("control privilege required"))
			return false
		}
	case scopeRead:
		if !ctrl && !read {
			s.writeErr(w, errUnauthorized("invalid token"))
			return false
		}
	}
	return true
}
