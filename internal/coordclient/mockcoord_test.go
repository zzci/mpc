package coordclient

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"

	"github.com/zzci/mpc/internal/contract"
)

// mockCoord is an independent, spec-faithful coord stand-in: it re-verifies
// the B1 member-auth signature exactly as internal/server/coord does (ts
// window, single-use nonce, secp256k1 over the documented digest) and lets a
// test script the response per route. It is interface-level (httptest) — no
// real network, no DB, no sqlcipher — matching the B-002 acceptance note.
type mockCoord struct {
	t         *testing.T
	srv       *httptest.Server
	memberPub []byte
	groupID   string

	mu       sync.Mutex
	nonces   map[string]bool
	authCnt  int // successful auth count
	handlers map[string]func(w http.ResponseWriter, r *http.Request, params []byte)
}

const mockAuthWindow = 5 * time.Minute

func newMockCoord(t *testing.T, memberPub []byte, groupID string) *mockCoord {
	m := &mockCoord{
		t:         t,
		memberPub: memberPub,
		groupID:   groupID,
		nonces:    map[string]bool{},
		handlers:  map[string]func(http.ResponseWriter, *http.Request, []byte){},
	}
	mux := http.NewServeMux()
	register := func(pat, verb string, get bool) {
		mux.HandleFunc(pat, func(w http.ResponseWriter, r *http.Request) {
			var params []byte
			if get {
				params = []byte(r.URL.RawQuery)
			} else {
				b, _ := io.ReadAll(r.Body)
				params = b
			}
			effVerb := verb
			if verb == "B4:decision:" {
				var d decisionBody
				_ = json.Unmarshal(params, &d)
				effVerb = "B4:decision:" + d.Decision
			}
			if !m.checkAuth(w, r, effVerb, params) {
				return
			}
			key := r.Method + " " + routeKey(r)
			h := m.handlers[key]
			if h == nil {
				w.WriteHeader(http.StatusNotImplemented)
				return
			}
			h(w, r, params)
		})
	}
	register("PUT /v1/members/self/push", "B2:push", false)
	register("POST /v1/members/self/heartbeat", "B5:heartbeat", false)
	register("GET /v1/groups/{groupId}/pending", "B3:pending", true)
	register("POST /v1/requests/{requestId}/decision", "B4:decision:", false)
	register("GET /v1/groups/{groupId}/dispatch", "B6:dispatch", true)
	register("POST /v1/requests/{requestId}/result", "B7:result", false)

	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)
	return m
}

// routeKey collapses a concrete request to the registered handler key.
func routeKey(r *http.Request) string {
	switch {
	case r.URL.Path == "/v1/members/self/push":
		return "push"
	case r.URL.Path == "/v1/members/self/heartbeat":
		return "heartbeat"
	case hasSuffix(r.URL.Path, "/pending"):
		return "pending"
	case hasSuffix(r.URL.Path, "/decision"):
		return "decision"
	case hasSuffix(r.URL.Path, "/dispatch"):
		return "dispatch"
	case hasSuffix(r.URL.Path, "/result"):
		return "result"
	}
	return r.URL.Path
}

func hasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}

// checkAuth mirrors internal/server/coord verifyMemberAuth: ts window, nonce
// single-use, then EC verify over the documented digest. On failure it writes
// the api.md error envelope and returns false.
func (m *mockCoord) checkAuth(w http.ResponseWriter, r *http.Request, verb string, params []byte) bool {
	memberID := r.Header.Get(headerMemberID)
	tsStr := r.Header.Get(headerMemberTS)
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		m.writeErr(w, http.StatusUnauthorized, CodeUnauthenticated, "bad ts")
		return false
	}
	nonce, err := base64.StdEncoding.DecodeString(r.Header.Get(headerMemberNonce))
	if err != nil || len(nonce) == 0 {
		m.writeErr(w, http.StatusUnauthorized, CodeUnauthenticated, "bad nonce")
		return false
	}
	sig, err := base64.StdEncoding.DecodeString(r.Header.Get(headerMemberSig))
	if err != nil || len(sig) == 0 {
		m.writeErr(w, http.StatusUnauthorized, CodeUnauthenticated, "bad sig")
		return false
	}
	now := time.Now().UnixMilli()
	if d := now - ts; d > mockAuthWindow.Milliseconds() || d < -mockAuthWindow.Milliseconds() {
		m.writeErr(w, http.StatusUnauthorized, CodeUnauthenticated, "stale ts")
		return false
	}
	nk := memberID + ":" + base64.StdEncoding.EncodeToString(nonce)
	m.mu.Lock()
	if m.nonces[nk] {
		m.mu.Unlock()
		m.writeErr(w, http.StatusUnauthorized, CodeUnauthenticated, "nonce replay")
		return false
	}
	m.nonces[nk] = true
	m.mu.Unlock()

	// Independent digest reconstruction (no production helper).
	bound := []byte(verb + "|" + m.groupID + "|")
	bound = append(bound, params...)
	bh := sha256.Sum256(bound)
	lp := func(b, v []byte) []byte {
		var p [4]byte
		binary.BigEndian.PutUint32(p[:], uint32(len(v)))
		return append(append(b, p[:]...), v...)
	}
	var pre []byte
	pre = append(pre, []byte("TSS-COORD-MEMBER-AUTH-v1")...)
	pre = append(pre, 0x00)
	pre = lp(pre, []byte(memberID))
	pre = lp(pre, []byte(verb))
	pre = lp(pre, bh[:])
	var tb [8]byte
	binary.BigEndian.PutUint64(tb[:], uint64(ts))
	pre = append(pre, tb[:]...)
	pre = lp(pre, nonce)
	digest := sha256.Sum256(pre)

	if err := contract.VerifyDigest(m.memberPub, digest, sig); err != nil {
		m.writeErr(w, http.StatusUnauthorized, CodeUnauthenticated, "bad member signature")
		return false
	}
	m.mu.Lock()
	m.authCnt++
	m.mu.Unlock()
	return true
}

func (m *mockCoord) writeErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"code": code, "message": msg},
	})
}

func (m *mockCoord) on(method, key string, h func(w http.ResponseWriter, r *http.Request, params []byte)) {
	m.handlers[method+" "+key] = h
}

func (m *mockCoord) authCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.authCnt
}

// newTestClient wires a Client to the mock with a fixed-but-fresh clock and a
// fast retry policy so backoff tests stay quick.
func newTestClient(t *testing.T, m *mockCoord, priv *btcec.PrivateKey) *Client {
	c, err := New(m.srv.URL, m.groupID, "m1", priv,
		WithRetryPolicy(RetryPolicy{MaxAttempts: 4, BaseDelay: time.Millisecond, MaxDelay: 4 * time.Millisecond}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}
