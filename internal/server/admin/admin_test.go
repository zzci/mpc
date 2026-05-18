package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/royqta/mcp-wallet/internal/server/coorddb"
)

const (
	testPass    = "operator-passphrase-correct"
	readTok     = "read-token-aaaaaaaaaaaaaaaa"
	controlTok  = "control-token-bbbbbbbbbbbbbbbb"
	seedGroupID = "grp-1"
	seedReqID   = "req-1"
)

// fakeClock is a test-controlled time source for unlock backoff.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// newServer builds a LOCKED store + admin Server with the given options.
func newServer(t *testing.T, opts ...Option) (*Server, *coorddb.Store, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "coord.db")
	store := coorddb.NewStore(dbPath)
	t.Cleanup(func() { _ = store.Close() })
	cfg := Config{Listen: "127.0.0.1:0", ReadToken: readTok, ControlToken: controlTok}
	s, err := New(cfg, store, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, store, dbPath
}

func do(t *testing.T, h http.Handler, method, path, token, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequestWithContext(context.Background(), method, path, nil)
	} else {
		r = httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	}
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

func unlock(t *testing.T, store *coorddb.Store) {
	t.Helper()
	if err := store.Unlock(context.Background(), []byte(testPass)); err != nil {
		t.Fatalf("store.Unlock: %v", err)
	}
}

// seedRequest inserts a full signing_requests row + a request_events timeline
// + approvals via the D-001 public WithTx (no coorddb modification).
func seedRequest(t *testing.T, store *coorddb.Store) {
	t.Helper()
	if err := store.ProvisionGroup(context.Background(),
		coorddb.GroupRecord{
			GroupID: seedGroupID, ECDSAPubkey: []byte{1}, GroupPubkey: []byte{2},
			ThresholdT: 2, PartiesN: 3, CreatedAt: "1700000000000",
		},
		[]coorddb.MemberRecord{{MemberID: "m1", IdentityPubkey: []byte{3}}}); err != nil {
		t.Fatalf("provision: %v", err)
	}
	ctx := context.Background()
	err := store.WithTx(ctx, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx,
			`INSERT INTO signing_requests
			 (request_id, group_id, chain, unsigned_tx, digest32, proposer,
			  business_info, meta_hash, proposer_sig, status, created_at, expiry,
			  signers, result_rsv)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			seedReqID, seedGroupID, "eth", []byte("RAWTX"), make([]byte, 32), "prop-1",
			`{"title":"pay invoice"}`, []byte("MH"), []byte("PS"), "RETURNED",
			"1700000005000", "1700000999000", `["m1","m2"]`, []byte("RSVRSV")); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx,
			`INSERT INTO request_events (request_id, from_status, to_status, actor, detail, at)
			 VALUES (?,?,?,?,?,?)`,
			seedReqID, "PENDING", "RETURNED", "coord", `{"k":1}`, "1700000006000"); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx,
			`INSERT INTO request_approvals (request_id, member_id, decision, sig, decided_at)
			 VALUES (?,?,?,?,?)`,
			seedReqID, "m1", "approved", []byte("S"), "1700000004000")
		return e
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// --- auth / privilege separation (admin.md §4, §7bis) --------------------

func TestAuth_ScopeSeparation(t *testing.T) {
	s, store, _ := newServer(t)
	unlock(t, store)
	h := s.router()

	// no token → 401
	if w, _ := do(t, h, "GET", "/admin/transactions", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no token: got %d want 401", w.Code)
	}
	// read token on read endpoint → ok
	if w, _ := do(t, h, "GET", "/admin/transactions", readTok, ""); w.Code != http.StatusOK {
		t.Fatalf("read token read: got %d want 200", w.Code)
	}
	// read token on control endpoint → 403 (separation)
	if w, _ := do(t, h, "POST", "/admin/controls/rotate-psk", readTok, ""); w.Code != http.StatusForbidden {
		t.Fatalf("read token control: got %d want 403", w.Code)
	}
	// control token implies read
	if w, _ := do(t, h, "GET", "/admin/transactions", controlTok, ""); w.Code != http.StatusOK {
		t.Fatalf("control token read: got %d want 200", w.Code)
	}
	// bogus token → 401
	if w, _ := do(t, h, "GET", "/admin/transactions", "nope", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("bad token: got %d want 401", w.Code)
	}
}

func TestConfig_RejectsSharedTokens(t *testing.T) {
	_, err := New(Config{Listen: ":0", ReadToken: "x", ControlToken: "x"},
		coorddb.NewStore(filepath.Join(t.TempDir(), "d.db")))
	if err == nil {
		t.Fatal("expected error: read/control tokens must differ")
	}
}

// --- LOCKED lifecycle (admin.md §8, server.md C9b) -----------------------

func TestLocked_FailClosedAndUnlockRelock(t *testing.T) {
	s, store, _ := newServer(t)
	h := s.router()

	// LOCKED: data + control 503; health + lock-status + unlock reachable.
	if w, _ := do(t, h, "GET", "/admin/transactions", readTok, ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("locked data: got %d want 503", w.Code)
	}
	if w, _ := do(t, h, "POST", "/admin/controls/rotate-psk", controlTok, ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("locked control: got %d want 503", w.Code)
	}
	if w, m := do(t, h, "GET", "/admin/lock-status", readTok, ""); w.Code != http.StatusOK || m["locked"] != true {
		t.Fatalf("lock-status under LOCKED: %d %v", w.Code, m)
	}
	if w, _ := do(t, h, "GET", "/healthz", "", ""); w.Code != http.StatusOK {
		t.Fatalf("healthz: got %d want 200", w.Code)
	}

	// Unlock (creates the encrypted db) → UNLOCKED, data served.
	if w, m := do(t, h, "POST", "/admin/unlock", controlTok, `{"passphrase":"`+testPass+`"}`); w.Code != http.StatusOK || m["locked"] != false {
		t.Fatalf("unlock: %d %v", w.Code, m)
	}
	if !store.IsUnlocked() {
		t.Fatal("store should be UNLOCKED")
	}
	if w, _ := do(t, h, "GET", "/admin/transactions", readTok, ""); w.Code != http.StatusOK {
		t.Fatalf("post-unlock data: got %d want 200", w.Code)
	}

	// Relock → LOCKED again, data 503.
	if w, m := do(t, h, "POST", "/admin/relock", controlTok, ""); w.Code != http.StatusOK || m["locked"] != true {
		t.Fatalf("relock: %d %v", w.Code, m)
	}
	if w, _ := do(t, h, "GET", "/admin/transactions", readTok, ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("post-relock data: got %d want 503", w.Code)
	}
}

func TestUnlock_BadPassphraseRateLimited(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	s, store, _ := newServer(t, WithClock(clk.now))
	h := s.router()

	// Initialize the encrypted db with the correct passphrase, then relock.
	unlock(t, store)
	if err := store.Relock(); err != nil {
		t.Fatalf("relock: %v", err)
	}

	// Wrong passphrase → 401.
	if w, _ := do(t, h, "POST", "/admin/unlock", controlTok, `{"passphrase":"WRONG"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("bad pass: got %d want 401", w.Code)
	}
	// Immediate retry (clock not advanced) → 429 backoff.
	if w, m := do(t, h, "POST", "/admin/unlock", controlTok, `{"passphrase":"WRONG"}`); w.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limit: got %d %v want 429", w.Code, m)
	}
	// After backoff window, the correct passphrase succeeds.
	clk.advance(unlockBackoffCap)
	if w, m := do(t, h, "POST", "/admin/unlock", controlTok, `{"passphrase":"`+testPass+`"}`); w.Code != http.StatusOK || m["locked"] != false {
		t.Fatalf("post-backoff unlock: %d %v", w.Code, m)
	}
}

// --- read queries (admin.md §1 / §7bis) ----------------------------------

func TestQueries_TransactionsFilterAndDetail(t *testing.T) {
	s, store, _ := newServer(t)
	unlock(t, store)
	seedRequest(t, store)
	h := s.router()

	// Filter by group + status + proposer + time window.
	w, m := do(t, h, "GET",
		"/admin/transactions?group=grp-1&status=RETURNED&proposer=prop-1&from=1700000000000&to=1700001000000",
		readTok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("transactions: got %d", w.Code)
	}
	items, _ := m["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %v", m["items"])
	}
	// A non-matching proposer filter yields nothing.
	if _, m2 := do(t, h, "GET", "/admin/transactions?proposer=other", readTok, ""); len(m2["items"].([]any)) != 0 {
		t.Fatalf("proposer filter leaked rows: %v", m2["items"])
	}

	// Detail: full record + timeline + approvals + result, NO shares anywhere.
	wd, md := do(t, h, "GET", "/admin/transactions/"+seedReqID, readTok, "")
	if wd.Code != http.StatusOK {
		t.Fatalf("detail: got %d", wd.Code)
	}
	if md["timeline"] == nil || len(md["timeline"].([]any)) != 1 {
		t.Fatalf("missing timeline: %v", md["timeline"])
	}
	if md["approvals"] == nil || len(md["approvals"].([]any)) != 1 {
		t.Fatalf("missing approvals: %v", md["approvals"])
	}
	if md["result"] == nil {
		t.Fatalf("missing result")
	}
	raw, _ := json.Marshal(md)
	for _, banned := range []string{"share", "Share", "\"key\"", "secret", "private"} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("detail leaked %q: %s", banned, raw)
		}
	}
	// Unknown request → 404.
	if w404, _ := do(t, h, "GET", "/admin/transactions/nope", readTok, ""); w404.Code != http.StatusNotFound {
		t.Fatalf("unknown req: got %d want 404", w404.Code)
	}
}

func TestRelayMetrics_DecoupledNote(t *testing.T) {
	s, store, _ := newServer(t)
	unlock(t, store)
	w, m := do(t, s.router(), "GET", "/admin/relay/metrics", readTok, "")
	if w.Code != http.StatusOK || m["available"] != false {
		t.Fatalf("relay metrics decoupled: %d %v", w.Code, m)
	}
}

// --- controls + audit immutability (admin.md §1 可控 / §7bis) -------------

type stubRelay struct{ banned, quotas int }

func (s *stubRelay) BanPeer(context.Context, string) error           { s.banned++; return nil }
func (s *stubRelay) RevokeReservation(context.Context, string) error { return nil }
func (s *stubRelay) RotatePSK(context.Context) error                 { return nil }
func (s *stubRelay) SetQuota(context.Context, map[string]int) error  { s.quotas++; return nil }

func TestControls_EnforcedAuditedAndImmutable(t *testing.T) {
	rc := &stubRelay{}
	s, store, _ := newServer(t, WithRelayController(rc))
	unlock(t, store)
	h := s.router()

	// Ban + quota propagate to the controller and are audited.
	if w, m := do(t, h, "POST", "/admin/controls/ban-peer", controlTok, `{"peerId":"12D3Koo"}`); w.Code != http.StatusOK || m["enforced"] != true {
		t.Fatalf("ban: %d %v", w.Code, m)
	}
	if w, _ := do(t, h, "POST", "/admin/controls/quota", controlTok, `{"params":{"reservation_per_token":2}}`); w.Code != http.StatusOK {
		t.Fatalf("quota: got %d", w.Code)
	}
	if rc.banned != 1 || rc.quotas != 1 {
		t.Fatalf("controller not invoked: %+v", rc)
	}

	// admin_audit reflects both ops; there is NO mutate/delete route.
	w, m := do(t, h, "GET", "/admin/audit", readTok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("audit list: %d", w.Code)
	}
	items := m["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("want 2 audit rows, got %d (%v)", len(items), items)
	}
	for _, method := range []string{"DELETE", "PUT", "PATCH"} {
		if wm, _ := do(t, h, method, "/admin/audit", controlTok, ""); wm.Code != http.StatusMethodNotAllowed && wm.Code != http.StatusNotFound {
			t.Fatalf("%s /admin/audit must not be routable: got %d", method, wm.Code)
		}
		if wm, _ := do(t, h, method, "/admin/audit/1", controlTok, ""); wm.Code != http.StatusMethodNotAllowed && wm.Code != http.StatusNotFound {
			t.Fatalf("%s /admin/audit/1 must not be routable: got %d", method, wm.Code)
		}
	}

	// No-secret discipline: rotate-psk audited without any key material.
	if w, _ := do(t, h, "POST", "/admin/controls/rotate-psk", controlTok, ""); w.Code != http.StatusOK {
		t.Fatalf("rotate-psk: got %d", w.Code)
	}
	w2, m2 := do(t, h, "GET", "/admin/audit", readTok, "")
	if w2.Code != http.StatusOK {
		t.Fatalf("audit list 2: %d", w2.Code)
	}
	raw, _ := json.Marshal(m2)
	for _, banned := range []string{testPass, "passphrase"} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("audit leaked secret-ish %q: %s", banned, raw)
		}
	}
}

func TestControls_NoControllerStillAudited(t *testing.T) {
	s, store, _ := newServer(t) // no RelayController
	unlock(t, store)
	h := s.router()
	w, m := do(t, h, "POST", "/admin/controls/ban-peer", controlTok, `{"peerId":"p"}`)
	if w.Code != http.StatusAccepted || m["recorded"] != true || m["enforced"] != false {
		t.Fatalf("no-controller ban: %d %v", w.Code, m)
	}
	_, ma := do(t, h, "GET", "/admin/audit", readTok, "")
	if len(ma["items"].([]any)) != 1 {
		t.Fatalf("directive not audited: %v", ma)
	}
}
