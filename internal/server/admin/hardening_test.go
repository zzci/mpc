package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zzci/mpc/internal/server/coorddb"
)

// doFrom is do() with a controllable source address (netGate is keyed on the
// direct peer IP).
func doFrom(t *testing.T, h http.Handler, method, path, remoteAddr, token, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var br io.Reader
	if body != "" {
		br = strings.NewReader(body)
	}
	r := httptest.NewRequestWithContext(context.Background(), method, path, br)
	r.RemoteAddr = remoteAddr
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var m map[string]any
	if b, _ := io.ReadAll(w.Body); len(b) > 0 {
		_ = json.Unmarshal(b, &m)
	}
	return w, m
}

// --- read/control separation hardening (P6 §4) ---------------------------

func TestConfig_RejectsWeakAndBadCIDR(t *testing.T) {
	store := coorddb.NewStore(filepath.Join(t.TempDir(), "d.db"))
	// Short tokens defeat the privilege boundary.
	if _, err := New(Config{Listen: ":0", ReadToken: "short", ControlToken: "alsoshort"}, store); err == nil {
		t.Fatal("expected weak-token rejection")
	}
	// Malformed CIDR must fail closed, never silently widen the boundary.
	if _, err := New(Config{
		Listen: ":0", ReadToken: readTok, ControlToken: controlTok,
		AllowedCIDRs: []string{"not-a-cidr"},
	}, store); err == nil {
		t.Fatal("expected invalid-CIDR rejection")
	}
}

// --- non-public IP allowlist enforcement (admin.md §5 / §7bis) -----------

func TestNetGate_AllowlistEnforced(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "coord.db")
	store := coorddb.NewStore(dbPath)
	t.Cleanup(func() { _ = store.Close() })
	cfg := Config{
		Listen: "127.0.0.1:0", ReadToken: readTok, ControlToken: controlTok,
		AllowedCIDRs: []string{"127.0.0.0/8"},
	}
	s, err := New(cfg, store)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	h := s.router()

	// In-allowlist source with a valid read token → reaches the handler.
	if w, _ := doFrom(t, h, "GET", "/admin/lock-status", "127.0.0.1:5555", readTok, ""); w.Code != http.StatusOK {
		t.Fatalf("allowed source: got %d want 200", w.Code)
	}
	// Out-of-allowlist source → 403 even with a valid token (netGate is
	// outermost, fail-closed before auth).
	if w, _ := doFrom(t, h, "GET", "/admin/lock-status", "192.0.2.7:5555", controlTok, ""); w.Code != http.StatusForbidden {
		t.Fatalf("blocked source w/ token: got %d want 403", w.Code)
	}
	// Out-of-allowlist source with no token → still 403 (boundary precedes auth).
	if w, _ := doFrom(t, h, "GET", "/healthz", "203.0.113.9:1", "", ""); w.Code != http.StatusForbidden {
		t.Fatalf("blocked source no token: got %d want 403", w.Code)
	}
	// Malformed RemoteAddr → not parseable → denied.
	if w, _ := doFrom(t, h, "GET", "/admin/lock-status", "garbage", readTok, ""); w.Code != http.StatusForbidden {
		t.Fatalf("unparseable source: got %d want 403", w.Code)
	}
}

// --- strong-auth seam: enforced + audit attribution (P6 §4) --------------

type stubStrongAuth struct{ principal string }

func (s stubStrongAuth) Authenticate(r *http.Request) (string, error) {
	if r.Header.Get("X-Fail") != "" {
		return "", fmt.Errorf("strong-auth: rejected")
	}
	return s.principal, nil
}

func TestStrongAuth_EnforcedAndAttributed(t *testing.T) {
	s, store, _ := newServer(t, WithStrongAuth(stubStrongAuth{principal: "alice@oidc"}))
	unlock(t, store)
	h := s.router()

	// Strong-auth failure → 401 before the scope check, no data.
	r := httptest.NewRequestWithContext(context.Background(), "GET", "/admin/lock-status", nil)
	r.Header.Set("Authorization", "Bearer "+readTok)
	r.Header.Set("X-Fail", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("strong-auth reject: got %d want 401", w.Code)
	}

	// Strong-auth pass: a control op is attributed to the verified principal
	// in admin_audit, not the token-scope label.
	if w, _ := do(t, h, "POST", "/admin/controls/ban-peer", controlTok, `{"peerId":"p"}`); w.Code != http.StatusAccepted {
		t.Fatalf("ban: got %d want 202", w.Code)
	}
	_, m := do(t, h, "GET", "/admin/audit", readTok, "")
	items := m["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("want 1 audit row, got %d", len(items))
	}
	if got := items[0].(map[string]any)["adminId"]; got != "alice@oidc" {
		t.Fatalf("audit principal: got %v want alice@oidc", got)
	}
}

// --- unlock concurrency hardening (admin.md §8 防爆破) --------------------

func TestUnlock_InflightRejected(t *testing.T) {
	s, store, _ := newServer(t)
	unlock(t, store)
	h := s.router()
	// Simulate an unlock already in progress by holding the inflight lock.
	s.unlock.inflight.Lock()
	defer s.unlock.inflight.Unlock()
	w, _ := do(t, h, "POST", "/admin/unlock", controlTok, `{"passphrase":"`+testPass+`"}`)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("concurrent unlock: got %d want 429", w.Code)
	}
}

// --- sustained brute-force alarm (security.md §5 解锁尝试限速) -----------

func TestUnlock_BruteForceEscalationAlarm(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	var buf bytes.Buffer
	var mu sync.Mutex
	lg := slog.New(slog.NewTextHandler(&lockedWriter{w: &buf, mu: &mu}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s, store, _ := newServer(t, WithClock(clk.now), WithLogger(lg))
	h := s.router()
	unlock(t, store)
	if err := store.Relock(); err != nil {
		t.Fatalf("relock: %v", err)
	}

	for i := 0; i < maxUnlockFailures; i++ {
		if w, _ := do(t, h, "POST", "/admin/unlock", controlTok, `{"passphrase":"WRONG"}`); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: got %d want 401", i, w.Code)
		}
		clk.advance(unlockBackoffCap) // clear backoff so the next attempt runs
	}
	mu.Lock()
	out := buf.String()
	mu.Unlock()
	if !strings.Contains(out, "SUSTAINED BRUTE-FORCE") {
		t.Fatalf("expected sustained-brute-force alarm after %d failures; log:\n%s", maxUnlockFailures, out)
	}
}

// lockedWriter serializes concurrent slog writes (handler may be called from
// the request goroutine; keeps the race detector quiet).
type lockedWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
