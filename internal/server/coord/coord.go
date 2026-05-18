package coord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/zzci/mpc/internal/contract"
	"github.com/zzci/mpc/internal/server/coorddb"
)

// Coord is the coord-role orchestrator. It owns the D-001 store + presence set
// and the in-process coordination state (START dispatch hub, result waiters,
// quorum engine, nonce cache). It never holds a share and never runs MPC
// (docs/design/server/server.md C1/C8).
type Coord struct {
	cfg      Config
	store    *coorddb.Store
	presence *coorddb.Presence
	db       *db
	clock    Clock
	log      *slog.Logger

	resolveProposer proposerKeyResolver
	notifier        Notifier
	callback        callbackSink
	nonces          *nonceCache

	hub         *dispatchHub
	results     *resultHub
	engine      *engine
	provisionRL *rateLimiter
	externalRL  *rateLimiter // P6: per-IP external (A) surface
	memberRL    *rateLimiter // P6: per-IP member (B) surface

	httpSrv *http.Server
}

// Notifier wakes a member device out-of-band when a START is dispatched (B6
// FCM/APNs). Real push needs external credentials/services unavailable in the
// in-process E2E environment; the long-poll dispatch channel (B6 fallback) is
// the testable path, so the default Notifier is a no-op and a production
// Notifier is injected via Options. coord never blocks on it.
type Notifier interface {
	NotifyDispatch(ctx context.Context, groupID string, signers []string)
}

type noopNotifier struct{}

func (noopNotifier) NotifyDispatch(context.Context, string, []string) {}

// Option configures a Coord at construction (functional options).
type Option func(*Coord)

// WithClock injects a deterministic clock (tests).
func WithClock(c Clock) Option { return func(co *Coord) { co.clock = c } }

// WithLogger sets the structured logger.
func WithLogger(l *slog.Logger) Option { return func(co *Coord) { co.log = l } }

// WithNotifier injects a real push Notifier.
func WithNotifier(n Notifier) Option { return func(co *Coord) { co.notifier = n } }

// WithProposerKeyResolver overrides proposer-key resolution (default: proposer
// identifier is its hex secp256k1 public key, see auth.go).
func WithProposerKeyResolver(r func(string) ([]byte, error)) Option {
	return func(co *Coord) { co.resolveProposer = r }
}

// WithHTTPClient injects the HTTP client used for webhook result callbacks
// (tests point it at an httptest server).
func WithHTTPClient(h *http.Client) Option {
	return func(co *Coord) { co.callback.client = h }
}

// WithExternalRateLimit tunes the P6 per-IP external (A) surface limiter
// (max requests per minute). max<=0 leaves the generous default.
func WithExternalRateLimit(max int) Option {
	return func(co *Coord) {
		if max > 0 {
			co.externalRL = newRateLimiter(max, time.Minute)
		}
	}
}

// WithMemberRateLimit tunes the P6 per-IP member (B) surface limiter
// (max requests per minute). max<=0 leaves the generous default.
func WithMemberRateLimit(max int) Option {
	return func(co *Coord) {
		if max > 0 {
			co.memberRL = newRateLimiter(max, time.Minute)
		}
	}
}

// New builds a Coord. store and presence are the D-001 dependencies (the
// caller owns their lifecycle: Unlock/Relock/Close). The HTTP server is not
// started until Start.
func New(cfg Config, store *coorddb.Store, presence *coorddb.Presence, opts ...Option) (*Coord, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if store == nil || presence == nil {
		return nil, fmt.Errorf("coord: store and presence are required")
	}
	c := &Coord{
		cfg:             cfg,
		store:           store,
		presence:        presence,
		db:              newDB(store),
		clock:           systemClock{},
		log:             slog.Default(),
		resolveProposer: defaultProposerKey,
		notifier:        noopNotifier{},
		nonces:          newNonceCache(),
		hub:             newDispatchHub(),
		results:         newResultHub(),
		provisionRL:     newRateLimiter(30, time.Minute),
		externalRL:      newRateLimiter(extDefaultMax, time.Minute),
		memberRL:        newRateLimiter(memberDefaultMax, time.Minute),
	}
	c.callback = callbackSink{
		mode:   cfg.ResultCallback,
		url:    cfg.CallbackURL,
		client: &http.Client{Timeout: 10 * time.Second},
		log:    c.log,
	}
	for _, o := range opts {
		o(c)
	}
	c.callback.log = c.log
	c.engine = newEngine(c)
	return c, nil
}

// Start binds the HTTP listener and starts the quorum engine's background
// sweep (expiry timers + signer-offline rollback). It returns once the
// listener is up; Serve runs until the context is cancelled or Shutdown.
func (c *Coord) Start(ctx context.Context) error {
	c.engine.start(ctx)
	c.httpSrv = &http.Server{
		Addr:              c.cfg.Listen,
		Handler:           c.router(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = c.httpSrv.Shutdown(sctx)
	}()
	if err := c.httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("coord: http serve: %w", err)
	}
	c.engine.stop()
	return nil
}

// Stop stops the engine and the HTTP server (idempotent; used by tests and the
// node shutdown path). It does not Relock the store — store lifecycle is the
// caller's (cmd/node) responsibility.
func (c *Coord) Stop() {
	c.engine.stop()
	if c.httpSrv != nil {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = c.httpSrv.Shutdown(sctx)
	}
}

// UnlockProvider supplies the in-memory whole-DB-encryption passphrase
// (docs/design/server/server.md C9b: never in config/env/KMS). The implementation
// is injected by the admin-api (A-001) at interactive unlock time; coord only
// drives the unlock loop. It is structurally identical to node.UnlockProvider
// so cmd/node can pass that implementation without coord importing internal/node.
type UnlockProvider interface {
	Passphrase() ([]byte, error)
}

// errUnlockUnavailable means no passphrase is available yet (provider not
// wired, e.g. before the admin-api exists). The store stays LOCKED and the API
// fail-closes 503 — exactly the designed startup behavior.
var errUnlockUnavailable = errors.New("coord: unlock passphrase unavailable")

// Unlock drives one rate-limit-backed unlock attempt loop: it pulls the
// passphrase from the provider, calls Store.Unlock, and on a bad-passphrase
// failure backs off with exponential delay (capped) before the next attempt,
// up to maxAttempts. The passphrase buffer is zeroized after every attempt
// regardless of outcome (docs/design/server/server.md C9b; D-001 also does not
// retain it). A nil provider yields errUnlockUnavailable immediately.
func (c *Coord) Unlock(ctx context.Context, p UnlockProvider, maxAttempts int) error {
	if p == nil {
		return errUnlockUnavailable
	}
	backoff := 500 * time.Millisecond
	const maxBackoff = 30 * time.Second
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		pass, err := p.Passphrase()
		if err != nil {
			return fmt.Errorf("coord: obtain passphrase: %w", err)
		}
		err = c.store.Unlock(ctx, pass)
		zeroize(pass)
		if err == nil {
			c.log.Info("coord store unlocked")
			return nil
		}
		if errors.Is(err, coorddb.ErrUnlocked) {
			return nil
		}
		c.log.Warn("coord unlock attempt failed", "attempt", attempt)
		if attempt == maxAttempts {
			return fmt.Errorf("coord: unlock failed after %d attempts: %w", maxAttempts, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
	return fmt.Errorf("coord: unlock exhausted attempts")
}

func zeroize(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// rebuildEnvelope reconstructs the exact SigningRequest the proposer signed
// from a stored row, so START carries the full envelope and proposerSig
// re-verifies bit-identically after a restart (S-001 §8). Only EnvelopeVersionV1
// is accepted at ingestion, so Version is fixed here.
func rebuildEnvelope(r *storedRequest) (*contract.SigningRequest, error) {
	env := &contract.SigningRequest{
		Version:     contract.EnvelopeVersionV1,
		RequestID:   r.RequestID,
		GroupID:     r.GroupID,
		Chain:       r.Chain,
		UnsignedTx:  r.UnsignedTx,
		Digest32:    r.Digest32,
		Proposer:    r.Proposer,
		CreatedAt:   r.CreatedAtMs,
		Expiry:      r.ExpiryMs,
		MetaHash:    r.MetaHash,
		ProposerSig: r.ProposerSig,
	}
	if len(r.BusinessRaw) > 0 {
		bi := &contract.BusinessInfo{}
		if err := json.Unmarshal(r.BusinessRaw, bi); err != nil {
			return nil, fmt.Errorf("%w: stored businessInfo: %w", contract.ErrInvalidEnvelope, err)
		}
		env.BusinessInfo = bi
	}
	return env, nil
}
