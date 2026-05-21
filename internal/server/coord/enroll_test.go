package coord

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zzci/mpc/internal/server"
)

func nopLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// validPubkeyHex33 returns a 33-byte hex-encoded compressed secp256k1
// pubkey. The byte content does not need to be on-curve for these tests —
// the enroll handler only enforces length + hex; on-curve validation lives
// further down the membership write path (not exercised in these tests).
func validPubkeyHex33() string {
	b := make([]byte, 33)
	b[0] = 0x02
	for i := 1; i < 33; i++ {
		b[i] = byte(i)
	}
	return hexlify(b)
}

func hexlify(b []byte) string {
	const h = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = h[c>>4]
		out[i*2+1] = h[c&0xF]
	}
	return string(out)
}

// pairingTestServer mounts only the pairing routes (no DB, no MPC) on a
// minimal mux so the tests are isolated from the rest of coord. The two
// hooks we exercise are: GET /v1/pairing/{token}/config (no consume) and
// POST /v1/pairing/{token}/enroll (consume). Errors come back via the
// shared writeErr envelope.
func pairingTestServer(t *testing.T, store *server.PairingStore) (*Coord, http.Handler) {
	t.Helper()
	c := &Coord{pairing: store, pairingInfo: PairingPublicInfo{
		CoordBaseURL: "https://coord.example.com",
		RelayPeerID:  "12D3KooWTEST",
		RelayAddrs:   []string{"/ip4/198.51.100.1/tcp/4001"},
	}}
	// Minimal init for the writeErr / log path; the actual coord wiring
	// does this in New, but only the bits used by the pairing handlers
	// are needed here.
	c.log = nopLogger()
	mux := http.NewServeMux()
	c.registerPairingRoutes(mux)
	return c, mux
}

func TestPairingConfigOK(t *testing.T) {
	now := func() time.Time { return time.Unix(1_700_000_000, 0) }
	store := server.NewPairingStore(now)
	c, mux := pairingTestServer(t, store)
	_ = c

	ticket, err := store.Create("g1", "Alice's iPhone", 10*time.Minute)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "GET",
		"/v1/pairing/"+ticket.Token+"/config", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp pairingPublicResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if resp.Token != ticket.Token {
		t.Fatalf("token mismatch: %q vs %q", resp.Token, ticket.Token)
	}
	if resp.GroupID != "g1" || resp.Label != "Alice's iPhone" {
		t.Fatalf("ticket fields lost: %+v", resp)
	}
	if resp.CoordBaseURL != "https://coord.example.com" {
		t.Fatalf("coordBaseURL = %q, want configured value", resp.CoordBaseURL)
	}
	if resp.RelayPeerID != "12D3KooWTEST" || len(resp.RelayAddrs) != 1 {
		t.Fatalf("relay bootstrap lost: %+v", resp)
	}
	// Config GET must NOT consume the ticket — a subsequent enroll still works.
	if _, ok := store.Get(ticket.Token); !ok {
		t.Fatalf("ticket vanished from store after a GET config")
	}
}

func TestPairingConfigUnknown(t *testing.T) {
	store := server.NewPairingStore(nil)
	_, mux := pairingTestServer(t, store)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "GET",
		"/v1/pairing/deadbeef/config", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("unknown token: code=%d", rec.Code)
	}
}

func TestPairingEnrollHappyPath(t *testing.T) {
	now := func() time.Time { return time.Unix(1_700_000_000, 0) }
	store := server.NewPairingStore(now)
	_, mux := pairingTestServer(t, store)
	ticket, _ := store.Create("g1", "label", 10*time.Minute)
	body, _ := json.Marshal(enrollRequest{IdentityPubkey: validPubkeyHex33()})
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "POST",
		"/v1/pairing/"+ticket.Token+"/enroll", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("enroll code=%d body=%s", rec.Code, rec.Body.String())
	}
	// Ticket must now be marked Used.
	got, _ := store.Get(ticket.Token)
	if got.UsedAt == nil || got.UsedBy != validPubkeyHex33() {
		t.Fatalf("ticket not consumed: %+v", got)
	}
}

func TestPairingEnrollBadIdentity(t *testing.T) {
	store := server.NewPairingStore(nil)
	_, mux := pairingTestServer(t, store)
	ticket, _ := store.Create("g1", "label", 10*time.Minute)
	for _, bad := range []string{"", "zz", "abcd"} {
		body, _ := json.Marshal(enrollRequest{IdentityPubkey: bad})
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), "POST",
			"/v1/pairing/"+ticket.Token+"/enroll", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != 400 {
			t.Fatalf("bad identity %q: code=%d", bad, rec.Code)
		}
	}
}

func TestPairingEnrollReplayRefused(t *testing.T) {
	store := server.NewPairingStore(nil)
	_, mux := pairingTestServer(t, store)
	ticket, _ := store.Create("g1", "label", 10*time.Minute)
	body, _ := json.Marshal(enrollRequest{IdentityPubkey: validPubkeyHex33()})
	first := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "POST",
		"/v1/pairing/"+ticket.Token+"/enroll", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(first, req)
	if first.Code != 200 {
		t.Fatalf("first enroll code=%d body=%s", first.Code, first.Body.String())
	}
	// Second use must surface as STATE_CONFLICT (409).
	second := httptest.NewRecorder()
	req2 := httptest.NewRequestWithContext(context.Background(), "POST",
		"/v1/pairing/"+ticket.Token+"/enroll", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(second, req2)
	if second.Code != 409 {
		t.Fatalf("replay code=%d body=%s", second.Code, second.Body.String())
	}
	if !strings.Contains(second.Body.String(), "consumed") {
		t.Fatalf("replay message wrong: %s", second.Body.String())
	}
}

func TestPairingEnrollExpired(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := &now
	store := server.NewPairingStore(func() time.Time { return *clock })
	_, mux := pairingTestServer(t, store)
	ticket, _ := store.Create("g1", "label", time.Minute)
	*clock = clock.Add(2 * time.Minute)
	body, _ := json.Marshal(enrollRequest{IdentityPubkey: validPubkeyHex33()})
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "POST",
		"/v1/pairing/"+ticket.Token+"/enroll", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rec, req)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "expired") {
		t.Fatalf("expired: code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPairingEnrollNotConfigured(t *testing.T) {
	// nil store ⇒ routes are not registered, so any request 404s.
	mux := http.NewServeMux()
	c := &Coord{pairing: nil, log: nopLogger()}
	c.registerPairingRoutes(mux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "POST",
		"/v1/pairing/deadbeef/enroll", strings.NewReader(`{}`))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("no-pairing-store code=%d", rec.Code)
	}
}
