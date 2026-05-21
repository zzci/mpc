package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zzci/mpc/internal/server"
)

// pairingAdmin builds a minimal admin Server wired with a PairingStore but
// no DB / no strong-auth. The route map is mounted on a plain mux so the
// tests can exercise both the JSON API and the htmx pages without spinning
// up the full Start() listener.
func pairingAdmin(t *testing.T, baseURL string) (*Server, http.Handler) {
	t.Helper()
	store := server.NewPairingStore(nil)
	s := &Server{
		now:     time.Now,
		pairing: pairingHooks{store: store, coordBaseURL: baseURL},
	}
	mux := http.NewServeMux()
	// API
	mux.HandleFunc("GET /api/pairing", s.hPairingList)
	mux.HandleFunc("POST /api/pairing", s.hPairingCreate)
	mux.HandleFunc("DELETE /api/pairing/{token}", s.hPairingDelete)
	mux.HandleFunc("GET /api/pairing/{token}/qr.png", s.hPairingQR)
	// UI (no auth gate so tests can drive directly)
	h := &uiHandler{s: s, sess: newUISessions(time.Now)}
	mux.HandleFunc("GET /pairing", h.hPairingPage)
	mux.HandleFunc("POST /pairing", h.hPairingUICreate)
	mux.HandleFunc("POST /pairing/{token}/delete", h.hPairingUIDelete)
	return s, mux
}

func TestPairingAPICreateAndList(t *testing.T) {
	_, mux := pairingAdmin(t, "https://coord.example.com")
	// Create
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/pairing",
		strings.NewReader(`{"groupId":"g1","label":"Alice","ttlSeconds":120}`))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "https://coord.example.com/v1/pairing/") {
		t.Fatalf("create response missing config URL: %s", rec.Body.String())
	}
	// List has the new ticket
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequestWithContext(context.Background(), "GET", "/api/pairing", nil)
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != 200 || !strings.Contains(rec2.Body.String(), "Alice") {
		t.Fatalf("list missing item: code=%d body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestPairingAPIDefaults(t *testing.T) {
	_, mux := pairingAdmin(t, "")
	// Empty body should default TTL to 10m.
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/pairing", nil)
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("default-ttl create code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPairingAPITTLCap(t *testing.T) {
	_, mux := pairingAdmin(t, "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/pairing",
		strings.NewReader(`{"ttlSeconds":999999999}`))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("ttl-cap should reject: code=%d", rec.Code)
	}
}

func TestPairingAPIDelete(t *testing.T) {
	s, mux := pairingAdmin(t, "")
	ticket, _ := s.pairing.store.Create("g", "l", time.Minute)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "DELETE",
		"/api/pairing/"+ticket.Token, nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatalf("delete code=%d", rec.Code)
	}
	if _, ok := s.pairing.store.Get(ticket.Token); ok {
		t.Fatalf("ticket should be gone")
	}
	// Second delete = 404.
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req)
	if rec2.Code != 404 {
		t.Fatalf("second delete code=%d", rec2.Code)
	}
}

func TestPairingQRRenders(t *testing.T) {
	s, mux := pairingAdmin(t, "https://coord.example.com")
	ticket, _ := s.pairing.store.Create("g", "l", time.Minute)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "GET",
		"/api/pairing/"+ticket.Token+"/qr.png", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("qr code=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("qr content-type = %q", rec.Header().Get("Content-Type"))
	}
	// PNG magic bytes: 89 50 4E 47 0D 0A 1A 0A.
	got := rec.Body.Bytes()
	if len(got) < 8 || got[0] != 0x89 || string(got[1:4]) != "PNG" {
		t.Fatalf("not a PNG: first 8 bytes = % X", got[:min(len(got), 8)])
	}
}

func TestPairingUIPageRenders(t *testing.T) {
	s, mux := pairingAdmin(t, "https://coord.example.com")
	ticket, _ := s.pairing.store.Create("groupA", "Alice", time.Minute)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/pairing", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("page code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Device pairing", "groupA", "Alice", "/api/pairing/" + ticket.Token + "/qr.png"} {
		if !strings.Contains(body, want) {
			t.Fatalf("page missing %q (have %d bytes)", want, len(body))
		}
	}
	_ = s
}

func TestPairingUICreateRedirect(t *testing.T) {
	s, mux := pairingAdmin(t, "")
	form := strings.NewReader("groupId=gX&label=lab&ttlSeconds=120")
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/pairing", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(rec, req)
	if rec.Code != 303 || rec.Header().Get("Location") != "/pairing" {
		t.Fatalf("create redirect code=%d loc=%s", rec.Code, rec.Header().Get("Location"))
	}
	if len(s.pairing.store.List()) != 1 {
		t.Fatalf("ticket not stored")
	}
}

func TestPairingDisabledIfNoStore(t *testing.T) {
	s := &Server{now: time.Now} // no pairing
	if s.pairingEnabled() {
		t.Fatalf("pairing should be disabled when store is nil")
	}
}
