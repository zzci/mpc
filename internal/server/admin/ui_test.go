package admin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/royqta/mcp-wallet/internal/server/coorddb"
)

// uiReq issues a UI request with optional bearer / cookie and returns the
// recorder + body. Redirects are NOT followed (recorder semantics) so the
// 303→login behaviour is observable.
func uiReq(t *testing.T, h http.Handler, method, path, bearer, cookie, body string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	var br io.Reader
	if body != "" {
		br = strings.NewReader(body)
	}
	r := httptest.NewRequestWithContext(context.Background(), method, path, br)
	r.RemoteAddr = "127.0.0.1:5555"
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	if cookie != "" {
		r.Header.Set("Cookie", uiCookieName+"="+cookie)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	b, _ := io.ReadAll(w.Body)
	return w, string(b)
}

type fakeRelayMetrics struct{}

func (fakeRelayMetrics) Snapshot(context.Context) (map[string]any, error) {
	return map[string]any{"connections": 7}, nil
}

// --- auth gating (Q-UI-AUTH default-safe model) --------------------------

func TestUI_AuthGate(t *testing.T) {
	s, store, _ := newServer(t)
	unlock(t, store)
	h := s.router()

	// No credential → browser is redirected to the login page.
	if w, _ := uiReq(t, h, "GET", "/admin/ui", "", "", ""); w.Code != http.StatusSeeOther ||
		w.Header().Get("Location") != "/admin/ui/login" {
		t.Fatalf("no cred: got %d loc=%q want 303 /admin/ui/login", w.Code, w.Header().Get("Location"))
	}
	// Valid read bearer (proxy/automation path) → 200.
	if w, _ := uiReq(t, h, "GET", "/admin/ui", readTok, "", ""); w.Code != http.StatusOK {
		t.Fatalf("read bearer: got %d want 200", w.Code)
	}
	// Control bearer implies read.
	if w, _ := uiReq(t, h, "GET", "/admin/ui", controlTok, "", ""); w.Code != http.StatusOK {
		t.Fatalf("control bearer: got %d want 200", w.Code)
	}
	// Bad bearer → 401 (not a redirect: an API caller, not a browser).
	if w, _ := uiReq(t, h, "GET", "/admin/ui", "nope", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("bad bearer: got %d want 401", w.Code)
	}
}

func TestUI_SessionLoginFlow(t *testing.T) {
	s, store, _ := newServer(t)
	unlock(t, store)
	h := s.router()

	if w, body := uiReq(t, h, "GET", "/admin/ui/login", "", "", ""); w.Code != http.StatusOK ||
		!strings.Contains(body, `action="/admin/ui/session"`) {
		t.Fatalf("login page: got %d", w.Code)
	}

	// Wrong credential → back to login, NO cookie set.
	w, _ := uiReq(t, h, "POST", "/admin/ui/session", "", "", "token=wrong-secret")
	if w.Code != http.StatusSeeOther || len(w.Result().Cookies()) != 0 {
		t.Fatalf("bad login: got %d cookies=%d want 303 0", w.Code, len(w.Result().Cookies()))
	}

	// Correct read token → session cookie + redirect to panel.
	w, _ = uiReq(t, h, "POST", "/admin/ui/session", "", "", "token="+readTok)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/ui" {
		t.Fatalf("login: got %d loc=%q", w.Code, w.Header().Get("Location"))
	}
	cs := w.Result().Cookies()
	if len(cs) != 1 || cs[0].Name != uiCookieName || !cs[0].HttpOnly ||
		cs[0].SameSite != http.SameSiteStrictMode || cs[0].Value == "" {
		t.Fatalf("session cookie attrs: %+v", cs)
	}
	sid := cs[0].Value

	// Cookie authorises the panel (no bearer).
	if w, _ := uiReq(t, h, "GET", "/admin/ui", "", sid, ""); w.Code != http.StatusOK {
		t.Fatalf("cookie auth: got %d want 200", w.Code)
	}
	// Logout drops the session; the same cookie no longer authorises.
	if w, _ := uiReq(t, h, "POST", "/admin/ui/logout", "", sid, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("logout: got %d want 303", w.Code)
	}
	if w, _ := uiReq(t, h, "GET", "/admin/ui", "", sid, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("post-logout: got %d want 303 (session invalidated)", w.Code)
	}
}

// --- LOCKED behaviour (admin.md §3/§8) -----------------------------------

func TestUI_LockedDashboardAndFailClosedData(t *testing.T) {
	s, store, _ := newServer(t)
	h := s.router()

	// Dashboard reachable under LOCKED, shows ONLY the lock state.
	w, body := uiReq(t, h, "GET", "/admin/ui", readTok, "", "")
	if w.Code != http.StatusOK || !strings.Contains(body, "LOCKED") ||
		strings.Contains(body, `href="/admin/ui/transactions"`) {
		t.Fatalf("locked dashboard: code=%d (must show LOCKED, hide data nav)", w.Code)
	}
	// Data pages fail closed 503 under LOCKED (shared s.lockGate).
	for _, p := range []string{"/admin/ui/transactions", "/admin/ui/audit", "/admin/ui/relay"} {
		if w, _ := uiReq(t, h, "GET", p, readTok, "", ""); w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s under LOCKED: got %d want 503", p, w.Code)
		}
	}
	// After unlock the overview exposes the data navigation.
	unlock(t, store)
	if w, body := uiReq(t, h, "GET", "/admin/ui", readTok, "", ""); w.Code != http.StatusOK ||
		!strings.Contains(body, "UNLOCKED") || !strings.Contains(body, `href="/admin/ui/transactions"`) {
		t.Fatalf("unlocked dashboard: code=%d", w.Code)
	}
}

// --- read views render the §1 data ---------------------------------------

func TestUI_TransactionsListDetailAndHTMXFragment(t *testing.T) {
	s, store, _ := newServer(t)
	unlock(t, store)
	seedRequest(t, store)
	h := s.router()

	// Full list page.
	w, body := uiReq(t, h, "GET", "/admin/ui/transactions", readTok, "", "")
	if w.Code != http.StatusOK || !strings.Contains(body, seedReqID) ||
		!strings.Contains(body, "<html") {
		t.Fatalf("tx list: code=%d full page expected", w.Code)
	}
	// HX-Request → fragment only (no full document chrome).
	r := httptest.NewRequestWithContext(context.Background(), "GET", "/admin/ui/transactions", nil)
	r.RemoteAddr = "127.0.0.1:1"
	r.Header.Set("Authorization", "Bearer "+readTok)
	r.Header.Set("HX-Request", "true")
	fw := httptest.NewRecorder()
	h.ServeHTTP(fw, r)
	fb, _ := io.ReadAll(fw.Body)
	if fw.Code != http.StatusOK || strings.Contains(string(fb), "<html") ||
		!strings.Contains(string(fb), `id="tx-body"`) {
		t.Fatalf("htmx fragment: code=%d must be partial", fw.Code)
	}

	// Filter by a non-matching group → empty result, still 200.
	if w, body := uiReq(t, h, "GET", "/admin/ui/transactions?group=nope", readTok, "", ""); w.Code != http.StatusOK ||
		!strings.Contains(body, "No matching requests") {
		t.Fatalf("filtered empty: code=%d", w.Code)
	}

	// Detail page: timeline + approvals + result.
	w, body = uiReq(t, h, "GET", "/admin/ui/transactions/"+seedReqID, readTok, "", "")
	if w.Code != http.StatusOK || !strings.Contains(body, "RETURNED") ||
		!strings.Contains(body, "Status timeline") || !strings.Contains(body, "m1") {
		t.Fatalf("tx detail: code=%d", w.Code)
	}
	// Unknown id → 404.
	if w, _ := uiReq(t, h, "GET", "/admin/ui/transactions/ghost", readTok, "", ""); w.Code != http.StatusNotFound {
		t.Fatalf("unknown id: got %d want 404", w.Code)
	}
}

func TestUI_AuditAndRelay(t *testing.T) {
	s, store, _ := newServer(t, WithRelayMetrics(fakeRelayMetrics{}))
	unlock(t, store)
	h := s.router()

	if w, body := uiReq(t, h, "GET", "/admin/ui/audit", readTok, "", ""); w.Code != http.StatusOK ||
		!strings.Contains(body, "Admin audit") {
		t.Fatalf("audit page: code=%d", w.Code)
	}
	if w, body := uiReq(t, h, "GET", "/admin/ui/relay", readTok, "", ""); w.Code != http.StatusOK ||
		!strings.Contains(body, "connections") {
		t.Fatalf("relay wired: code=%d", w.Code)
	}

	// Not wired → decoupled note, no fabricated values.
	s2, store2, _ := newServer(t)
	unlock(t, store2)
	if w, body := uiReq(t, s2.router(), "GET", "/admin/ui/relay", readTok, "", ""); w.Code != http.StatusOK ||
		!strings.Contains(body, "decoupled") {
		t.Fatalf("relay unwired: code=%d", w.Code)
	}
}

// --- assets, read-only surface, hardening reuse --------------------------

func TestUI_AssetsServed(t *testing.T) {
	s, store, _ := newServer(t)
	unlock(t, store)
	h := s.router()
	for _, c := range []struct{ path, ctype string }{
		{"/admin/ui/assets/htmx.min.js", "text/javascript"},
		{"/admin/ui/assets/tw.css", "text/css"},
	} {
		w, body := uiReq(t, h, "GET", c.path, "", "", "")
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != c.ctype || body == "" {
			t.Fatalf("%s: code=%d ctype=%q", c.path, w.Code, w.Header().Get("Content-Type"))
		}
	}
}

// TestUI_ReadOnlySurface asserts the panel exposes NO state-changing route
// (Q1 default: control operations are not in the UI).
func TestUI_ReadOnlySurface(t *testing.T) {
	s, store, _ := newServer(t)
	unlock(t, store)
	h := s.router()

	// No state-changing route exists under the UI namespace: a control POST
	// even with the control bearer never succeeds (≥400, no handler).
	if w, _ := uiReq(t, h, "POST", "/admin/ui/controls/ban-peer", controlTok, "", ""); w.Code < 400 {
		t.Fatalf("ui control POST must not succeed: got %d", w.Code)
	}
	// Read pages reject non-GET (registered GET-only).
	if w, _ := uiReq(t, h, "POST", "/admin/ui/transactions", controlTok, "", ""); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("tx POST: got %d want 405", w.Code)
	}
	// Security headers on rendered pages.
	w, _ := uiReq(t, h, "GET", "/admin/ui", readTok, "", "")
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'none'") ||
		w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing security headers: %v", w.Header())
	}
}

// TestUI_NetGateAndStrongAuthApplyToUI proves the UI is no weaker than the
// JSON surface: netGate (§5) precedes everything and StrongAuth (§4) runs
// before the session check.
func TestUI_NetGateAndStrongAuthApplyToUI(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "coord.db")
	store := coorddb.NewStore(dbPath)
	t.Cleanup(func() { _ = store.Close() })
	cfg := Config{
		Listen: "127.0.0.1:0", ReadToken: readTok, ControlToken: controlTok,
		AllowedCIDRs: []string{"10.0.0.0/8"},
	}
	s, err := New(cfg, store)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := store.Unlock(context.Background(), []byte(testPass)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	h := s.router()
	// Out-of-allowlist source → 403 before any UI auth (netGate outermost).
	r := httptest.NewRequestWithContext(context.Background(), "GET", "/admin/ui", nil)
	r.RemoteAddr = "192.0.2.5:9"
	r.Header.Set("Authorization", "Bearer "+readTok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("netGate on UI: got %d want 403", w.Code)
	}

	// StrongAuth runs before the session/bearer check.
	s2, store2, _ := newServer(t, WithStrongAuth(stubStrongAuth{principal: "alice@oidc"}))
	unlock(t, store2)
	h2 := s2.router()
	r2 := httptest.NewRequestWithContext(context.Background(), "GET", "/admin/ui", nil)
	r2.RemoteAddr = "127.0.0.1:1"
	r2.Header.Set("Authorization", "Bearer "+readTok)
	r2.Header.Set("X-Fail", "1")
	w2 := httptest.NewRecorder()
	h2.ServeHTTP(w2, r2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("strong-auth on UI: got %d want 401", w2.Code)
	}
}
