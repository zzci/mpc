package admin

import (
	"context"
	"crypto/tls"
	"net/http"
)

// StrongAuth is the deployment seam for strong administrator authentication
// (admin.md §4 "强鉴权(mTLS 或 OIDC + 2FA)", security.md §5 恶意/被控管理员
// 行). The admin identity is independent of members/external services; this
// contract lets a deployment terminate mTLS or OIDC+2FA in front of (or
// inside) admin-api and bind a verified principal to the audited operation.
//
// It is intentionally a small, injected interface (golang/patterns "define
// interfaces where used"): the package ships no OIDC client — environments
// without a real IdP wire a mTLS-backed implementation, and the bearer-token
// scope check (auth.go) remains the always-on baseline either way.
//
// Authenticate runs BEFORE the scope/token check. Returning a non-nil error
// fails the request closed (401) and nothing downstream executes. The
// returned principal is a non-secret operator label recorded in admin_audit
// (database.md §6 "谁"); it MUST NOT be a token or any secret.
type StrongAuth interface {
	Authenticate(r *http.Request) (principal string, err error)
}

// WithStrongAuth wires a strong-auth verifier (OIDC+2FA / mTLS principal
// extraction). Without it the admin-api runs bearer-only "soft validation"
// and Start emits a prominent hardening warning: the deployment is then
// responsible for terminating mTLS/OIDC in front (admin.md §5, the documented
// soft-validation closure for environments without a real IdP).
func WithStrongAuth(sa StrongAuth) Option { return func(s *Server) { s.strongAuth = sa } }

// WithTLS makes Start serve over TLS. For mTLS strong auth the deployment
// supplies a *tls.Config with ClientAuth=RequireAndVerifyClientCert and a
// client-cert pool; the package performs no certificate file IO so it stays
// unit-testable (cmd/node builds the config after secret resolution).
func WithTLS(cfg *tls.Config) Option { return func(s *Server) { s.tlsCfg = cfg } }

// principalKey carries the StrongAuth principal from the guard to the audit
// layer so admin_audit attributes the verified operator, not just the token
// scope label.
type principalKey struct{}

func withPrincipal(ctx context.Context, p string) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func principalFrom(ctx context.Context) string {
	p, _ := ctx.Value(principalKey{}).(string)
	return p
}
