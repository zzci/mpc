package admin

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/zzci/mpc/internal/server/coorddb"
)

// maxBodyBytes caps an admin request body (DoS guard; mirrors coord).
const maxBodyBytes = 1 << 20

// Server is the coord-side admin-api. It owns no persistence: it reads and
// drives the D-001 store (shared with coord) through its public API only.
type Server struct {
	cfg   Config
	store *coorddb.Store
	log   *slog.Logger
	now   func() time.Time

	unlock unlockGuard

	relayController RelayController // nil → directives audited, enforcement delegated
	relayMetrics    RelayMetrics    // nil → metrics scraped from relay's own surface

	strongAuth StrongAuth   // nil → bearer-only soft validation (Start warns)
	tlsCfg     *tls.Config  // nil → plaintext (deployment terminates TLS/mTLS)
	allowNets  []*net.IPNet // empty → no in-process IP allowlist (Start warns)

	httpSrv *http.Server
}

// Option configures a Server (functional options).
type Option func(*Server)

// WithLogger sets the structured logger.
func WithLogger(l *slog.Logger) Option { return func(s *Server) { s.log = l } }

// WithClock injects a deterministic time source (tests; drives unlock backoff).
func WithClock(now func() time.Time) Option { return func(s *Server) { s.now = now } }

// WithRelayController wires relay-side enforcement of the abuse kill-switches.
func WithRelayController(rc RelayController) Option {
	return func(s *Server) { s.relayController = rc }
}

// WithRelayMetrics wires a read-only relay metrics source (server.md R6).
func WithRelayMetrics(rm RelayMetrics) Option {
	return func(s *Server) { s.relayMetrics = rm }
}

// New builds the admin-api over the shared coord store (D-001). The store
// lifecycle (Unlock/Relock/Close) is co-owned: cmd/server creates it, coord
// serves from it, and admin-api drives unlock/relock — all via the store's own
// concurrency-safe public API.
func New(cfg Config, store *coorddb.Store, opts ...Option) (*Server, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, fmt.Errorf("admin: store is required")
	}
	s := &Server{
		cfg:   cfg,
		store: store,
		log:   slog.Default(),
		now:   func() time.Time { return time.Now().UTC() },
	}
	for _, o := range opts {
		o(s)
	}
	nets, err := cfg.allowedNets() // already validated; re-parse to store
	if err != nil {
		return nil, err
	}
	s.allowNets = nets
	return s, nil
}

// router builds the admin HTTP surface. Wrapper order is outermost→innermost:
// netGate (IP allowlist) → auth (strong-auth + scope) → lockGate
// (data/control only) → handler.
//
// Layout: every JSON endpoint lives under /api/*; the bare root and every
// non-api path is the htmx panel (see ui.go). /healthz, /api/unlock and
// /api/lock-status are reachable under LOCKED (admin.md §8: only unlock +
// minimal status before unlock); every other endpoint fail-closes 503
// LOCKED while the store is locked.
func (s *Server) router() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Reachable under LOCKED (no encrypted-store data).
	mux.Handle("POST /api/unlock", s.guard(scopeControl, http.HandlerFunc(s.hUnlock)))
	mux.Handle("POST /api/relock", s.guard(scopeControl, http.HandlerFunc(s.hRelock)))
	mux.Handle("GET /api/lock-status", s.guard(scopeRead, http.HandlerFunc(s.hLockStatus)))

	// Read-only queries (scopeRead) — fail-closed 503 under LOCKED.
	mux.Handle("GET /api/transactions", s.guard(scopeRead, s.lockGate(http.HandlerFunc(s.hTransactions))))
	mux.Handle("GET /api/transactions/{requestId}", s.guard(scopeRead, s.lockGate(http.HandlerFunc(s.hTransactionDetail))))
	mux.Handle("GET /api/audit", s.guard(scopeRead, s.lockGate(http.HandlerFunc(s.hAudit))))
	mux.Handle("GET /api/relay/metrics", s.guard(scopeRead, s.lockGate(http.HandlerFunc(s.hRelayMetrics))))

	// Abuse controls (scopeControl) — fail-closed 503 under LOCKED (the
	// directive must land in the encrypted admin_audit).
	mux.Handle("POST /api/controls/ban-peer", s.guard(scopeControl, s.lockGate(http.HandlerFunc(s.hBanPeer))))
	mux.Handle("POST /api/controls/revoke-reservation", s.guard(scopeControl, s.lockGate(http.HandlerFunc(s.hRevokeReservation))))
	mux.Handle("POST /api/controls/rotate-psk", s.guard(scopeControl, s.lockGate(http.HandlerFunc(s.hRotatePSK))))
	mux.Handle("POST /api/controls/quota", s.guard(scopeControl, s.lockGate(http.HandlerFunc(s.hSetQuota))))

	// admin-ui (UI-001): read-only htmx+tailwind SSR panel served in-process
	// (admin.md §3). It registers under the same mux so s.netGate (§5) wraps
	// it too; it uses its own StrongAuth+session auth (ui.go) instead of
	// s.guard, and reuses s.lockGate for data pages.
	s.newUI().register(mux)

	return s.netGate(mux)
}

// guard runs strong auth (when wired), then enforces the privilege scope and
// caps the body, then delegates. Strong auth runs first so an unverified
// caller never reaches the token comparison; its principal is threaded into
// the request context for the audit layer (admin.md §4).
func (s *Server) guard(want scope, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.strongAuth != nil {
			principal, err := s.strongAuth.Authenticate(r)
			if err != nil {
				s.log.Warn("admin strong-auth rejected", "src", clientIP(r))
				s.writeErr(w, errUnauthorized("strong authentication failed"))
				return
			}
			if principal != "" {
				r = r.WithContext(withPrincipal(r.Context(), principal))
			}
		}
		if !s.authorize(w, r, want) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// lockGate fail-closes when the coord store is LOCKED (admin.md §8,
// server.md C9b): nothing past it runs and no data leaks.
func (s *Server) lockGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.store.IsUnlocked() {
			s.writeErr(w, errLocked())
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Start binds the listener and serves until ctx is cancelled. The admin-api
// must be up before coord can be unlocked (it owns the interactive unlock),
// so cmd/server starts it ahead of the blocking coord serve.
func (s *Server) Start(ctx context.Context) error {
	s.logHardeningPosture()
	s.httpSrv = &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           s.router(),
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig:         s.tlsCfg,
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(sctx)
	}()
	serve := s.httpSrv.ListenAndServe
	if s.tlsCfg != nil {
		// Certs/keys live in tlsCfg.Certificates (deployment-supplied); for
		// mTLS the deployment also sets ClientAuth=RequireAndVerifyClientCert.
		serve = func() error { return s.httpSrv.ListenAndServeTLS("", "") }
	}
	s.log.Info("admin-api serving", "listen", s.cfg.Listen, "tls", s.tlsCfg != nil)
	if err := serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("admin: http serve: %w", err)
	}
	return nil
}

// logHardeningPosture emits prominent warnings for every soft-validation
// closure left to the deployment (admin.md §4/§5: strong auth + non-public are
// otherwise deployment concerns). It makes the residual responsibility
// auditable in the process log instead of silently assumed.
func (s *Server) logHardeningPosture() {
	if s.strongAuth == nil && s.tlsCfg == nil {
		s.log.Warn("admin-api HARDENING: no strong auth (mTLS/OIDC+2FA) wired — " +
			"bearer-only soft validation; deployment MUST terminate mTLS/OIDC in front (admin.md §4)")
	}
	if len(s.allowNets) == 0 {
		s.log.Warn("admin-api HARDENING: no IP allowlist (AllowedCIDRs empty) — " +
			"non-public boundary delegated to deployment network (admin.md §5)")
	}
	if !isLoopbackListen(s.cfg.Listen) && len(s.allowNets) == 0 && s.tlsCfg == nil {
		s.log.Warn("admin-api HARDENING: listening on a non-loopback address with neither "+
			"IP allowlist nor TLS — public-exposure risk (admin.md §5/§7bis)", "listen", s.cfg.Listen)
	}
}

// Stop shuts the HTTP server down (idempotent). It does not Relock the store —
// store lifecycle is the caller's (cmd/server) responsibility.
func (s *Server) Stop() {
	if s.httpSrv != nil {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(sctx)
	}
}

// --- shared I/O helpers --------------------------------------------------

func (s *Server) readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeErr(w, errBadRequest("unreadable body"))
		return false
	}
	if err := json.Unmarshal(raw, v); err != nil {
		s.writeErr(w, errBadRequest("malformed JSON body"))
		return false
	}
	return true
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr emits the {error:{code,message}} envelope; the message never
// carries internals or secrets (api.md:79 discipline).
func (s *Server) writeErr(w http.ResponseWriter, e *apiError) {
	s.writeJSON(w, e.status, map[string]any{
		"error": map[string]any{"code": e.code, "message": e.message},
	})
}

// rawJSON emits a stored JSON-TEXT column verbatim as a JSON value rather than
// a re-encoded string. Invalid JSON degrades to a string so a corrupt cell
// never breaks the response.
type rawJSON string

func (j rawJSON) MarshalJSON() ([]byte, error) {
	if json.Valid([]byte(j)) {
		return []byte(j), nil
	}
	return json.Marshal(string(j))
}
