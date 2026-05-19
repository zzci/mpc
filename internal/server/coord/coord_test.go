package coord

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	becdsa "github.com/btcsuite/btcd/btcec/v2/ecdsa"

	"github.com/zzci/mpc/internal/contract"
	"github.com/zzci/mpc/internal/server/coorddb"
)

// testClock is a settable clock for the C6 expiry gates (no sleeping).
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type harness struct {
	co       *Coord
	srv      *httptest.Server
	clk      *testClock
	store    *coorddb.Store
	presence *coorddb.Presence
	cbMu     sync.Mutex
	cbBodies []callbackBody
}

func newHarness(t *testing.T, opts ...Option) *harness {
	t.Helper()
	h := &harness{clk: &testClock{t: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}}

	// Capturing webhook so A4 callback delivery is asserted in-process.
	cb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b callbackBody
		_ = json.NewDecoder(r.Body).Decode(&b)
		h.cbMu.Lock()
		h.cbBodies = append(h.cbBodies, b)
		h.cbMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(cb.Close)

	dbPath := filepath.Join(t.TempDir(), "coord.db")
	store := coorddb.NewStore(dbPath)
	if err := store.Unlock(context.Background(), []byte("test-passphrase")); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	presence, err := coorddb.NewPresence(60*time.Second, time.Hour)
	if err != nil {
		t.Fatalf("presence: %v", err)
	}
	t.Cleanup(func() { _ = presence.Close() })

	cfg := Config{
		Listen:          "127.0.0.1:0",
		DBPath:          dbPath,
		APIKey:          "secret-key",
		CallbackURL:     cb.URL,
		CallbackAPIKey:  "test-callback-bearer",
		NotifyWebhook:   cb.URL,
		SkewTolerance:   0,
		SignerSelect:    signerStable,
		DispatchTimeout: 2 * time.Minute,
	}
	co, err := New(cfg, store, presence, append([]Option{WithClock(h.clk)}, opts...)...)
	if err != nil {
		t.Fatalf("new coord: %v", err)
	}
	h.co, h.store, h.presence = co, store, presence
	h.srv = httptest.NewServer(co.router())
	t.Cleanup(h.srv.Close)
	return h
}

func (h *harness) callbacks() []callbackBody {
	h.cbMu.Lock()
	defer h.cbMu.Unlock()
	return append([]callbackBody(nil), h.cbBodies...)
}

func newKey(t *testing.T) (*btcec.PrivateKey, []byte, []byte) {
	t.Helper()
	k, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	return k, k.PubKey().SerializeUncompressed(), k.PubKey().SerializeCompressed()
}

// --- group provisioning helper (S-002 self-attesting) --------------------

type testGroup struct {
	groupID   string
	mainPriv  *btcec.PrivateKey
	mainPub   []byte // uncompressed (RSV recover anchor)
	capPriv   *btcec.PrivateKey
	capPubC   []byte // compressed (group-key anchor)
	members   map[string]*btcec.PrivateKey
	memberPub map[string][]byte
	t, n      int
}

func (h *harness) provision(t *testing.T, groupID string, thr, n int) *testGroup {
	t.Helper()
	mainPriv, mainPub, _ := newKey(t)
	capPriv, _, capPubC := newKey(t)
	g := &testGroup{
		groupID: groupID, mainPriv: mainPriv, mainPub: mainPub,
		capPriv: capPriv, capPubC: capPubC,
		members: map[string]*btcec.PrivateKey{}, memberPub: map[string][]byte{},
		t: thr, n: n,
	}
	var entries []memberEntry
	for i := 0; i < n; i++ {
		id := "m" + strconv.Itoa(i)
		mp, _, mpc := newKey(t)
		g.members[id] = mp
		g.memberPub[id] = mpc
		entries = append(entries, memberEntry{MemberID: id, IdentityPubkey: mpc})
	}
	p := groupProvisioning{
		Version: contract.EnvelopeVersionV1, GroupID: groupID,
		ECDSAPubkey: mainPub, GroupPubkey: capPubC,
		ThresholdT: thr, PartiesN: n, Members: entries,
		CreatedAt: h.clk.Now().Format(time.RFC3339Nano),
	}
	dig, err := groupProvisionDigest(&p)
	if err != nil {
		t.Fatalf("provision digest: %v", err)
	}
	p.GroupSig = contract.SignDigest(capPriv, dig)
	for i := 0; i < thr; i++ {
		id := "m" + strconv.Itoa(i)
		p.MemberCoSigs = append(p.MemberCoSigs, coSig{
			MemberID: id, Sig: contract.SignDigest(g.members[id], dig),
		})
	}
	body, _ := json.Marshal(p)
	resp := h.do(t, http.MethodPost, "/v1/groups", nil, body)
	if resp.code != http.StatusCreated {
		t.Fatalf("provision status %d: %s", resp.code, resp.text())
	}
	return g
}

// --- envelope helper -----------------------------------------------------

func (h *harness) envelope(t *testing.T, g *testGroup, proposer *btcec.PrivateKey, ttl time.Duration) *contract.SigningRequest {
	t.Helper()
	digest := make([]byte, 32)
	_, _ = rand.Read(digest)
	now := h.clk.Now()
	env := &contract.SigningRequest{
		Version:    contract.EnvelopeVersionV1,
		RequestID:  newUUID(),
		GroupID:    g.groupID,
		Chain:      "eth",
		UnsignedTx: []byte("rawtx"),
		Digest32:   digest,
		Proposer:   hex.EncodeToString(proposer.PubKey().SerializeCompressed()),
		CreatedAt:  now.UnixMilli(),
		Expiry:     now.Add(ttl).UnixMilli(),
	}
	mh, _ := contract.MetaHash(nil)
	env.MetaHash = mh[:]
	if err := contract.SignEnvelope(proposer, env); err != nil {
		t.Fatalf("sign envelope: %v", err)
	}
	return env
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- HTTP helpers --------------------------------------------------------

// hresp is a response with its body already read and closed, so test call
// sites never hold an open Body (keeps bodyclose satisfied without a Close at
// every site).
type hresp struct {
	code int
	body []byte
}

func (r *hresp) text() string { return string(r.body) }

func (h *harness) do(t *testing.T, method, path string, hdr map[string]string, body []byte) *hresp {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, h.srv.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("req: %v", err)
	}
	req.Header.Set("X-API-Key", "secret-key")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return &hresp{code: resp.StatusCode, body: b}
}

// memberHdr builds the X-Member-* headers for B endpoints, signing the same
// preimage memberGate reconstructs: memberAuthDigest(id, method,
// hash(method|groupID|params), ts, nonce).
func (h *harness) memberHdr(g *testGroup, id, method, groupID string, params []byte) map[string]string {
	priv := g.members[id]
	ts := h.clk.Now().UnixMilli()
	nonce := make([]byte, 12)
	_, _ = rand.Read(nonce)
	bound := append([]byte(method+"|"+groupID+"|"), params...)
	dig := memberAuthDigest(id, method, hash(bound), ts, nonce)
	sig := contract.SignDigest(priv, dig)
	return map[string]string{
		"X-Member-Id":    id,
		"X-Member-Ts":    strconv.FormatInt(ts, 10),
		"X-Member-Nonce": base64.StdEncoding.EncodeToString(nonce),
		"X-Member-Sig":   base64.StdEncoding.EncodeToString(sig),
		"Content-Type":   "application/json",
	}
}

func (h *harness) heartbeat(t *testing.T, g *testGroup, id string) {
	t.Helper()
	body, _ := json.Marshal(heartbeatBody{GroupID: g.groupID, MemberID: id, RelayPeerID: "peer-" + id})
	resp := h.do(t, http.MethodPost, "/v1/members/self/heartbeat",
		h.memberHdr(g, id, "B5:heartbeat", g.groupID, body), body)
	if resp.code != http.StatusNoContent {
		t.Fatalf("heartbeat %s: %d %s", id, resp.code, resp.text())
	}
}

func (h *harness) decide(t *testing.T, g *testGroup, id, reqID, decision string) {
	t.Helper()
	body, _ := json.Marshal(decisionBody{MemberID: id, Decision: decision})
	resp := h.do(t, http.MethodPost, "/v1/requests/"+reqID+"/decision",
		h.memberHdr(g, id, "B4:decision:"+decision, g.groupID, body), body)
	if resp.code != http.StatusOK {
		t.Fatalf("decide %s: %d %s", id, resp.code, resp.text())
	}
}

// rsvFor produces the 65-byte [V+27||R||S] the device would report: a
// secp256k1 signature by the group main key over the request digest (in-process
// stand-in for the threshold output; coord only verifies recover==pubkey).
func rsvFor(g *testGroup, digest []byte) []byte {
	return signCompact(g.mainPriv, digest)
}

func signCompact(k *btcec.PrivateKey, digest []byte) []byte {
	sig, err := becdsa.SignCompact(k, digest, false)
	if err != nil {
		panic(err)
	}
	return sig
}

// === C10 end-to-end ======================================================

func TestC10_EndToEnd(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-e2e", 2, 3)
	proposer, _, _ := newKey(t)

	env := h.envelope(t, g, proposer, time.Hour)
	raw, _ := json.Marshal(env)
	resp := h.do(t, http.MethodPost, "/v1/requests", nil, raw)
	if resp.code != http.StatusAccepted {
		t.Fatalf("ingest: %d %s", resp.code, resp.text())
	}

	// heartbeat + approve two members -> quorum.
	for _, id := range []string{"m0", "m1"} {
		h.heartbeat(t, g, id)
		h.decide(t, g, id, env.RequestID, "approved")
	}
	h.co.engine.evaluate(context.Background(), env.RequestID) // deterministic

	// B6: a signer pulls START with the full envelope.
	hdr := h.memberHdr(g, "m0", "B6:dispatch", g.groupID, []byte(""))
	resp = h.do(t, http.MethodGet, "/v1/groups/"+g.groupID+"/dispatch", hdr, nil)
	var st contract.StartSigning
	if err := json.Unmarshal(resp.body, &st); err != nil {
		t.Fatalf("decode START: %v", err)
	}
	if st.RequestID != env.RequestID || len(st.Signers) != 2 {
		t.Fatalf("bad START: %+v", st)
	}
	if err := contract.VerifyProposerSig(&st.Envelope, mustPub(proposer)); err != nil {
		t.Fatalf("START envelope proposerSig invalid: %v", err)
	}

	// B7: a signer reports a valid {R,S,V}.
	rsv := rsvFor(g, env.Digest32)
	rb, _ := json.Marshal(resultBody{MemberID: "m0", RSV: rsv})
	resp = h.do(t, http.MethodPost, "/v1/requests/"+env.RequestID+"/result",
		h.memberHdr(g, "m0", "B7:result", g.groupID, rb), rb)
	if resp.code != http.StatusOK {
		t.Fatalf("result: %d %s", resp.code, resp.text())
	}

	waitFor(t, func() bool {
		for _, b := range h.callbacks() {
			if b.RequestID == env.RequestID && b.Status == stReturned && b.RSV != "" {
				return true
			}
		}
		return false
	})

	// A3 reflects RETURNED with the result.
	resp = h.do(t, http.MethodGet, "/v1/requests/"+env.RequestID, nil, nil)
	var got map[string]any
	_ = json.Unmarshal(resp.body, &got)
	if got["status"] != stReturned {
		t.Fatalf("A3 status = %v", got["status"])
	}
}

func mustPub(k *btcec.PrivateKey) []byte { return k.PubKey().SerializeCompressed() }

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}
