package coord

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/zzci/mpc/internal/contract"
)

// P6 hardening (docs/design/security.md §5, docs/design/server/server.md C7/C8). These
// assert the added abuse controls and the strengthened C8 row defences without
// regressing X-001's C1-C10 semantics (the X-001 suite still runs unchanged).

// --- external-service auth hardening (C7) --------------------------------

// api_key: an absent or wrong X-API-Key is an explicit 401; a correct key
// passes (constant-time compared in checkAPIKey).
func TestExtAuthAPIKeyHardening(t *testing.T) {
	h := newHarness(t)

	// Raw client so we control the header (h.do always sets the key).
	req := func(key string) int {
		r, err := http.NewRequestWithContext(context.Background(),
			http.MethodGet, h.srv.URL+"/v1/requests/none", nil)
		if err != nil {
			t.Fatalf("req: %v", err)
		}
		if key != "" {
			r.Header.Set("X-API-Key", key)
		}
		resp, err := h.srv.Client().Do(r)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if c := req(""); c != http.StatusUnauthorized {
		t.Fatalf("missing key: want 401 got %d", c)
	}
	if c := req("wrong-key"); c != http.StatusUnauthorized {
		t.Fatalf("wrong key: want 401 got %d", c)
	}
	if c := req("secret-key"); c == http.StatusUnauthorized {
		t.Fatalf("correct key: unexpected 401")
	}
}

// External auth is fixed api_key (user ruling 2026-05-19): the mtls
// option and its hardening test were removed. TestExtAuthAPIKeyHardening
// above is the sole external-auth gate test.

// --- rate limiting (anti-abuse) ------------------------------------------

// External (A) per-IP limiter trips with 429 once the window budget is spent
// (here tuned to 2 via Option). The gate runs before auth so a burst is shed
// regardless of the per-request outcome.
func TestRateLimitExternal(t *testing.T) {
	h := newHarness(t, WithExternalRateLimit(2))
	codes := []int{}
	for i := 0; i < 3; i++ {
		codes = append(codes, h.do(t, http.MethodGet, "/v1/requests/none", nil, nil).code)
	}
	if codes[0] == http.StatusTooManyRequests || codes[1] == http.StatusTooManyRequests {
		t.Fatalf("limiter tripped too early: %v", codes)
	}
	if codes[2] != http.StatusTooManyRequests {
		t.Fatalf("3rd external request: want 429 got %d (%v)", codes[2], codes)
	}
}

// Member (B) per-IP limiter trips with 429; a heartbeat flood from one origin
// is capped (keyed by IP, not the claimed memberId).
func TestRateLimitMember(t *testing.T) {
	h := newHarness(t, WithMemberRateLimit(2))
	g := h.provision(t, "grp-rl", 2, 3)
	body, _ := json.Marshal(heartbeatBody{GroupID: g.groupID, MemberID: "m0", RelayPeerID: "p"})

	var last int
	for i := 0; i < 3; i++ {
		// fresh nonce each call so the replay guard is not what trips it
		hdr := h.memberHdr(g, "m0", "B5:heartbeat", g.groupID, body)
		last = h.do(t, http.MethodPost, "/v1/members/self/heartbeat", hdr, body).code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("3rd member request: want 429 got %d", last)
	}
}

// --- proposerSig strong validation ---------------------------------------

// Structurally invalid envelopes are rejected 400 before any signature work
// (short digest32, expiry<=createdAt, oversized unsignedTx, bad proposer key
// length); none are enqueued.
func TestProposerSigStrongValidation(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-sv", 2, 3)
	proposer, _, _ := newKey(t)

	mutate := func(f func(e *contract.SigningRequest)) *hresp {
		e := h.envelope(t, g, proposer, time.Hour)
		f(e) // mutate AFTER signing: shape gate must catch it pre-crypto
		raw, _ := json.Marshal(e)
		return h.do(t, http.MethodPost, "/v1/requests", nil, raw)
	}

	cases := map[string]func(e *contract.SigningRequest){
		"short digest32":   func(e *contract.SigningRequest) { e.Digest32 = e.Digest32[:16] },
		"expiry<=created":  func(e *contract.SigningRequest) { e.Expiry = e.CreatedAt },
		"empty unsignedTx": func(e *contract.SigningRequest) { e.UnsignedTx = nil },
		"oversized tx": func(e *contract.SigningRequest) {
			e.UnsignedTx = make([]byte, maxUnsignedTxBytes+1)
		},
		"empty proposer": func(e *contract.SigningRequest) { e.Proposer = "" },
		"bad proposer key len": func(e *contract.SigningRequest) {
			e.Proposer = "deadbeef" // hex-valid, not a 33/65B secp256k1 key
		},
	}
	for name, f := range cases {
		resp := mutate(f)
		if resp.code != http.StatusBadRequest {
			t.Fatalf("%s: want 400 got %d %s", name, resp.code, resp.text())
		}
	}
}

// --- C8 attack-surface rows ----------------------------------------------

// C8 "falsely claim a member is online": a heartbeat forged with a non-member key fails the B1
// identity-sig gate (401), so coord cannot fake a member online and quorum
// never dispatches.
func TestC8_FalseOnlineForgedHeartbeat(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-fo", 2, 3)
	body, _ := json.Marshal(heartbeatBody{GroupID: g.groupID, MemberID: "m0", RelayPeerID: "p"})

	// Header claims m0 but is signed by an attacker key, not m0's identity key.
	forged, _, _ := newKey(t)
	ts := h.clk.Now().UnixMilli()
	nonce := make([]byte, 12)
	_, _ = rand.Read(nonce)
	bound := append([]byte("B5:heartbeat|"+g.groupID+"|"), body...)
	dig := memberAuthDigest("m0", "B5:heartbeat", hash(bound), ts, nonce)
	hdr := map[string]string{
		"X-Member-Id":    "m0",
		"X-Member-Ts":    strconv.FormatInt(ts, 10),
		"X-Member-Nonce": base64.StdEncoding.EncodeToString(nonce),
		"X-Member-Sig":   base64.StdEncoding.EncodeToString(contract.SignDigest(forged, dig)),
		"Content-Type":   "application/json",
	}
	resp := h.do(t, http.MethodPost, "/v1/members/self/heartbeat", hdr, body)
	if resp.code != http.StatusUnauthorized {
		t.Fatalf("forged heartbeat: want 401 got %d %s", resp.code, resp.text())
	}
	online, err := h.presence.Online(context.Background(), g.groupID)
	if err != nil {
		t.Fatalf("online: %v", err)
	}
	if len(online) != 0 {
		t.Fatalf("forged heartbeat marked a member online: %+v", online)
	}
}

// C8 "send different envelopes to different members": coord rebuilds ONE envelope from the stored row
// and publishes it to every signer — the START each signer pulls is
// byte-identical (same digest32 and proposerSig), so coord cannot split.
func TestC8_NoEnvelopeSplit(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-split", 2, 3)
	proposer, _, _ := newKey(t)
	env := h.envelope(t, g, proposer, time.Hour)
	raw, _ := json.Marshal(env)
	h.do(t, http.MethodPost, "/v1/requests", nil, raw)
	for _, id := range []string{"m0", "m1"} {
		h.heartbeat(t, g, id)
		h.decide(t, g, id, env.RequestID, "approved")
	}
	h.co.engine.evaluate(context.Background(), env.RequestID)

	pull := func(id string) contract.StartSigning {
		hdr := h.memberHdr(g, id, "B6:dispatch", g.groupID, []byte(""))
		resp := h.do(t, http.MethodGet, "/v1/groups/"+g.groupID+"/dispatch", hdr, nil)
		var st contract.StartSigning
		if err := json.Unmarshal(resp.body, &st); err != nil {
			t.Fatalf("decode START for %s: %v", id, err)
		}
		return st
	}
	a, b := pull("m0"), pull("m1")
	da, _ := contract.EnvelopeDigest(&a.Envelope)
	db, _ := contract.EnvelopeDigest(&b.Envelope)
	if da != db {
		t.Fatal("coord sent different envelope digests to two signers (split attack)")
	}
	if string(a.Envelope.Digest32) != string(b.Envelope.Digest32) ||
		string(a.Envelope.ProposerSig) != string(b.Envelope.ProposerSig) {
		t.Fatal("signers received non-identical envelopes")
	}
}

// C8 "tamper businessInfo (phishing)": businessInfo altered after signing breaks
// metaHash==H(businessInfo) -> 400, never enqueued (proposerSig+metaHash).
func TestC8_TamperedBusinessInfo(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-bi", 2, 3)
	proposer, _, _ := newKey(t)

	bi := &contract.BusinessInfo{Title: "pay invoice #1234", Requester: "svc"}
	mh, err := contract.MetaHash(bi)
	if err != nil {
		t.Fatalf("metahash: %v", err)
	}
	now := h.clk.Now()
	env := &contract.SigningRequest{
		Version:      contract.EnvelopeVersionV1,
		RequestID:    newUUID(),
		GroupID:      g.groupID,
		Chain:        "eth",
		UnsignedTx:   []byte("rawtx"),
		Digest32:     make([]byte, 32),
		Proposer:     hex.EncodeToString(proposer.PubKey().SerializeCompressed()),
		CreatedAt:    now.UnixMilli(),
		Expiry:       now.Add(time.Hour).UnixMilli(),
		BusinessInfo: bi,
		MetaHash:     mh[:],
	}
	if err := contract.SignEnvelope(proposer, env); err != nil {
		t.Fatalf("sign: %v", err)
	}
	// Tamper AFTER signing: metaHash no longer matches businessInfo.
	env.BusinessInfo = &contract.BusinessInfo{Title: "pay attacker", Requester: "evil"}
	raw, _ := json.Marshal(env)

	resp := h.do(t, http.MethodPost, "/v1/requests", nil, raw)
	if resp.code != http.StatusBadRequest {
		t.Fatalf("tampered businessInfo: want 400 got %d %s", resp.code, resp.text())
	}
	if _, found, _ := h.co.db.requestStatus(context.Background(), env.RequestID); found {
		t.Fatal("tampered-businessInfo envelope was enqueued")
	}
}

// C8 "replay an old request": a requestId that reached a terminal state is one-time
// (server/server.md C6(d)); a resubmit returns the original terminal status
// and is never reset to PENDING / re-enqueued.
func TestC8_TerminalRequestIdNoReuse(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-reuse", 2, 3)
	proposer, _, _ := newKey(t)
	env := h.envelope(t, g, proposer, 30*time.Second)
	raw, _ := json.Marshal(env)

	if r := h.do(t, http.MethodPost, "/v1/requests", nil, raw); r.code != http.StatusAccepted {
		t.Fatalf("ingest: %d", r.code)
	}
	h.clk.advance(time.Minute)
	h.co.engine.evaluate(context.Background(), env.RequestID)
	if st, _, _ := h.co.db.requestStatus(context.Background(), env.RequestID); st != stExpired {
		t.Fatalf("want EXPIRED got %s", st)
	}

	// Replay the exact same (now-expired) envelope: rejected 400 at submit
	// (C6(a)) and, crucially, the terminal row is never resurrected to PENDING
	// — the requestId is one-time (C6(d)).
	resp := h.do(t, http.MethodPost, "/v1/requests", nil, raw)
	if resp.code != http.StatusBadRequest {
		t.Fatalf("replay of expired requestId: want 400 got %d %s", resp.code, resp.text())
	}
	if st, _, _ := h.co.db.requestStatus(context.Background(), env.RequestID); st != stExpired {
		t.Fatalf("replayed requestId resurrected from EXPIRED to %s", st)
	}
}
