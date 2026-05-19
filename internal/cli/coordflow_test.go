package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	btcec "github.com/btcsuite/btcd/btcec/v2"
	"golang.org/x/text/unicode/norm"

	"github.com/zzci/mpc/internal/contract"
	"github.com/zzci/mpc/internal/mpc"
	"github.com/zzci/mpc/internal/server/coord"
	"github.com/zzci/mpc/internal/server/coorddb"
	"github.com/zzci/mpc/internal/txdecode"

	"github.com/bnb-chain/tss-lib/v3/ecdsa/keygen"
)

// Test 2 — the external-service envelope flow against the REAL coord role
// (X-001), end to end: external service submits a real ETH EIP-155 envelope ->
// coord ingests (proposerSig/metaHash/expiry) -> members heartbeat + approve
// -> quorum -> START (envelope re-verified) -> device tx-decode recomputes the
// chain digest == digest32 -> threshold {R,S,V} -> coord verifies it recovers
// the group master key -> external longpoll gets RETURNED. coord never holds a
// share and never runs MPC.
//
// FORCED DEVIATION (documented for L2): cmd/server's coord role starts LOCKED
// and only the admin-api (A-001, NOT a CLI-001 dependency, unmerged) can
// inject the unlock passphrase — a spawned `node` coord subprocess 503s every
// data endpoint, so the full envelope flow is unreachable through it within
// CLI-001's dependency closure. The carrier therefore runs the *real* X-001
// coord in-process with a test passphrase (the standard coord test seam,
// coord_test.go newHarness). The threshold signature is a real in-process
// tss-lib 2-of-3 (mpc.Keygen/Sign, full proofs); the relay-transported MPC is
// proven separately and exhaustively by TestE2EMultiProcessKeygenSignReshare
// ViaRelay. B-002 (coord client) is also unmerged, so the member-side coord
// calls are hand-rolled here per dispatch ("otherwise drive the coord HTTP API directly").

// --- S-002 group-provisioning canonical (verbatim from coord/
// provision_canonical.go; that package's symbols are unexported) ----------

var cfGroupProvisionDomain = append([]byte("TSS-GROUP-PROVISIONING-CANONICAL-v1"), 0x00)
var cfMemberAuthDomain = append([]byte("TSS-COORD-MEMBER-AUTH-v1"), 0x00)

type cfMemberEntry struct {
	MemberID       string `json:"memberId"`
	IdentityPubkey []byte `json:"identityPubkey"`
}
type cfCoSig struct {
	MemberID string `json:"memberId"`
	Sig      []byte `json:"sig"`
}
type cfGroupProvisioning struct {
	Version      uint64          `json:"version"`
	GroupID      string          `json:"groupId"`
	ECDSAPubkey  []byte          `json:"ecdsaPubkey"`
	GroupPubkey  []byte          `json:"groupPubkey"`
	ThresholdT   int             `json:"thresholdT"`
	PartiesN     int             `json:"partiesN"`
	Members      []cfMemberEntry `json:"members"`
	CreatedAt    string          `json:"createdAt"`
	GroupSig     []byte          `json:"groupSig"`
	MemberCoSigs []cfCoSig       `json:"memberCoSigs"`
}

func cfPutU64(b []byte, v uint64) []byte {
	var t [8]byte
	binary.BigEndian.PutUint64(t[:], v)
	return append(b, t[:]...)
}
func cfPutI64(b []byte, v int64) []byte { return cfPutU64(b, uint64(v)) }
func cfPutLP(b, v []byte) []byte {
	var lp [4]byte
	binary.BigEndian.PutUint32(lp[:], uint32(len(v)))
	return append(append(b, lp[:]...), v...)
}
func cfPutStr(b []byte, s string) []byte { return cfPutLP(b, norm.NFC.Bytes([]byte(s))) }

func cfGroupProvisionDigest(p *cfGroupProvisioning) [32]byte {
	ms, _ := time.Parse(time.RFC3339Nano, p.CreatedAt)
	var b []byte
	b = append(b, cfGroupProvisionDomain...)
	b = cfPutU64(b, p.Version)
	b = cfPutStr(b, p.GroupID)
	b = cfPutLP(b, p.ECDSAPubkey)
	b = cfPutLP(b, p.GroupPubkey)
	b = cfPutI64(b, int64(p.ThresholdT))
	b = cfPutI64(b, int64(p.PartiesN))
	b = binary.BigEndian.AppendUint32(b, uint32(len(p.Members)))
	for _, m := range p.Members {
		b = cfPutStr(b, m.MemberID)
		b = cfPutLP(b, m.IdentityPubkey)
	}
	b = cfPutI64(b, ms.UTC().UnixMilli())
	return sha256.Sum256(b)
}

// cfMemberAuthDigest reproduces coord/auth.go memberAuthDigest.
func cfMemberAuthDigest(memberID, method string, params []byte, ts int64, nonce []byte) [32]byte {
	var b []byte
	b = append(b, cfMemberAuthDomain...)
	b = cfPutLP(b, []byte(memberID))
	b = cfPutLP(b, []byte(method))
	b = cfPutLP(b, params)
	var tb [8]byte
	binary.BigEndian.PutUint64(tb[:], uint64(ts))
	b = append(b, tb[:]...)
	b = cfPutLP(b, nonce)
	return sha256.Sum256(b)
}

func cfHash(b []byte) []byte { h := sha256.Sum256(b); return h[:] }

type coordClient struct {
	t       *testing.T
	baseURL string
	apiKey  string
	groupID string
}

func (c *coordClient) do(method, path string, hdr map[string]string, body []byte) (int, []byte) {
	c.t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, c.baseURL+path, rdr)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("X-API-Key", c.apiKey)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("coord %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// memberHdr signs the B1 member authenticator the coord memberGate verifies.
func (c *coordClient) memberHdr(id string, key *btcec.PrivateKey, method string, params []byte) map[string]string {
	ts := time.Now().UnixMilli()
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	bound := append([]byte(method+"|"+c.groupID+"|"), params...)
	dig := cfMemberAuthDigest(id, method, cfHash(bound), ts, nonce)
	sig := contract.SignDigest(key, dig)
	return map[string]string{
		"X-Member-Id":    id,
		"X-Member-Ts":    strconv.FormatInt(ts, 10),
		"X-Member-Nonce": base64.StdEncoding.EncodeToString(nonce),
		"X-Member-Sig":   base64.StdEncoding.EncodeToString(sig),
		"Content-Type":   "application/json",
	}
}

// coordUp probes /healthz until it answers 200 or the budget elapses; it never
// fails the test (callers retry on a new port), so it is non-fatal by design.
func coordUp(cc *coordClient, budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, cc.baseURL+"/healthz", nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func TestE2ECoordEnvelopeFlowRealCoord(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E coord envelope flow: skipped in -short")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	// Real in-process 2-of-3 keygen (full proofs, fixture pre-params): this
	// is the wallet master key the threshold signature must recover to.
	fx, _, err := keygen.LoadKeygenTestFixtures(3)
	if err != nil {
		t.Fatalf("load keygen fixtures: %v", err)
	}
	pre := make([]keygen.LocalPreParams, 3)
	for i := range pre {
		pre[i] = fx[i].LocalPreParams
	}
	shares, err := mpc.Keygen(ctx, mpc.KeygenConfig{Threshold: 1, Parties: 3, PreParams: pre})
	if err != nil {
		t.Fatalf("mpc.Keygen: %v", err)
	}
	masterPub, err := groupPubUncompressed(shares[0])
	if err != nil {
		t.Fatal(err)
	}

	// Real coord (X-001) in-process, store test-unlocked (forced deviation,
	// see file header). longpoll result delivery (no external webhook needed).
	dbPath := filepath.Join(t.TempDir(), "coord.db")
	store := coorddb.NewStore(dbPath)
	if err := store.Unlock(ctx, []byte("cli-e2e-passphrase")); err != nil {
		t.Fatalf("coord store unlock: %v", err)
	}
	defer func() { _ = store.Close() }()
	presence, err := coorddb.NewPresence(90*time.Second, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = presence.Close() }()

	// coord binds its own listener from a string addr (it does not expose the
	// chosen port), so a free port is grabbed then released for it. Under a
	// full-tree -race run many packages bind concurrently and that window can
	// be lost — retry on a fresh port until the server actually answers.
	srvCtx, srvCancel := context.WithCancel(ctx)
	defer srvCancel()
	var cc *coordClient
	var co *coord.Coord
	for attempt := 0; attempt < 6; attempt++ {
		ln, lerr := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
		if lerr != nil {
			t.Fatal(lerr)
		}
		addr := ln.Addr().String()
		_ = ln.Close()

		c, nerr := coord.New(coord.Config{
			Listen:          addr,
			DBPath:          dbPath,
			ExternalAuth:    "api_key",
			APIKey:          "ext-secret",
			ResultCallback:  "longpoll",
			SkewTolerance:   2 * time.Minute,
			SignerSelect:    "stable",
			DispatchTimeout: 2 * time.Minute,
		}, store, presence)
		if nerr != nil {
			t.Fatalf("coord.New: %v", nerr)
		}
		go func() { _ = c.Start(srvCtx) }()
		client := &coordClient{t: t, baseURL: "http://" + addr, apiKey: "ext-secret", groupID: "wallet-coord-e2e"}
		if coordUp(client, 12*time.Second) {
			co, cc = c, client
			break
		}
		c.Stop()
	}
	if cc == nil {
		t.Fatal("coord did not come up on any port after retries")
	}
	defer co.Stop()

	// --- S-002 provisioning: register the group + members ---
	capKey, _ := btcec.NewPrivateKey()
	memberKeys := map[string]*btcec.PrivateKey{}
	var entries []cfMemberEntry
	for i := 0; i < 3; i++ {
		id := "m" + strconv.Itoa(i)
		k, _ := btcec.NewPrivateKey()
		memberKeys[id] = k
		entries = append(entries, cfMemberEntry{MemberID: id, IdentityPubkey: k.PubKey().SerializeCompressed()})
	}
	prov := cfGroupProvisioning{
		Version: contract.EnvelopeVersionV1, GroupID: cc.groupID,
		ECDSAPubkey: masterPub, GroupPubkey: capKey.PubKey().SerializeCompressed(),
		ThresholdT: 2, PartiesN: 3, Members: entries,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	pdig := cfGroupProvisionDigest(&prov)
	prov.GroupSig = contract.SignDigest(capKey, pdig)
	for i := 0; i < 2; i++ {
		id := "m" + strconv.Itoa(i)
		prov.MemberCoSigs = append(prov.MemberCoSigs, cfCoSig{MemberID: id, Sig: contract.SignDigest(memberKeys[id], pdig)})
	}
	pbody, _ := json.Marshal(prov)
	if code, b := cc.do(http.MethodPost, "/v1/groups", map[string]string{"Content-Type": "application/json"}, pbody); code != http.StatusCreated {
		t.Fatalf("provision group: %d %s", code, b)
	}

	// --- external service submits a real ETH EIP-155 envelope ---
	proposer, _ := btcec.NewPrivateKey()
	unsignedTx, _ := hex.DecodeString(eip155RLP)
	digest32, _ := hex.DecodeString(eip155Digest)
	now := time.Now()
	env := &contract.SigningRequest{
		Version:    contract.EnvelopeVersionV1,
		RequestID:  hex.EncodeToString([]byte("req-cli-e2e-0001")),
		GroupID:    cc.groupID,
		Chain:      "eth",
		UnsignedTx: unsignedTx,
		Digest32:   digest32,
		Proposer:   hex.EncodeToString(proposer.PubKey().SerializeCompressed()),
		CreatedAt:  now.UnixMilli(),
		Expiry:     now.Add(time.Hour).UnixMilli(),
	}
	mh, _ := contract.MetaHash(nil)
	env.MetaHash = mh[:]
	if err := contract.SignEnvelope(proposer, env); err != nil {
		t.Fatalf("sign envelope: %v", err)
	}
	raw, _ := json.Marshal(env)
	if code, b := cc.do(http.MethodPost, "/v1/requests", map[string]string{"Content-Type": "application/json"}, raw); code != http.StatusAccepted {
		t.Fatalf("ingest envelope: %d %s", code, b)
	}

	// --- members: heartbeat + approve -> quorum ---
	for _, id := range []string{"m0", "m1"} {
		hb, _ := json.Marshal(map[string]string{"groupId": cc.groupID, "memberId": id, "relayPeerID": "peer-" + id})
		if code, b := cc.do(http.MethodPost, "/v1/members/self/heartbeat",
			cc.memberHdr(id, memberKeys[id], "B5:heartbeat", hb), hb); code != http.StatusNoContent {
			t.Fatalf("heartbeat %s: %d %s", id, code, b)
		}
		db, _ := json.Marshal(map[string]string{"memberId": id, "decision": "approved"})
		if code, b := cc.do(http.MethodPost, "/v1/requests/"+env.RequestID+"/decision",
			cc.memberHdr(id, memberKeys[id], "B4:decision:approved", db), db); code != http.StatusOK {
			t.Fatalf("decision %s: %d %s", id, code, b)
		}
	}

	// --- B6: a signer pulls START, re-verifies the envelope, tx-decodes ---
	var st contract.StartSigning
	deadline := time.Now().Add(30 * time.Second)
	for {
		code, b := cc.do(http.MethodGet, "/v1/groups/"+cc.groupID+"/dispatch?wait=5s",
			cc.memberHdr("m0", memberKeys["m0"], "B6:dispatch", []byte("wait=5s")), nil)
		if code == http.StatusOK && json.Unmarshal(b, &st) == nil && st.RequestID == env.RequestID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never received START for %s (last code %d body %s)", env.RequestID, code, b)
		}
	}
	if err := contract.VerifyProposerSig(&st.Envelope, proposer.PubKey().SerializeCompressed()); err != nil {
		t.Fatalf("START envelope proposerSig invalid: %v", err)
	}
	dec, err := txdecode.New().Decode(&st.Envelope)
	if err != nil || !dec.DigestVerified {
		t.Fatalf("device tx-decode did not bind digest32: dec=%v err=%v", dec, err)
	}

	// --- threshold sign the bound digest; coord verifies recover==masterPub ---
	sig, err := mpc.Sign(ctx, mpc.SignConfig{
		SessionID: env.RequestID, Threshold: 1,
		Shares: []mpc.Share{shares[0], shares[1]}, Digest: st.Envelope.Digest32,
	})
	if err != nil {
		t.Fatalf("mpc.Sign: %v", err)
	}
	rb, _ := json.Marshal(map[string]any{"memberId": "m0", "rsv": sig.Compact()})
	if code, b := cc.do(http.MethodPost, "/v1/requests/"+env.RequestID+"/result",
		cc.memberHdr("m0", memberKeys["m0"], "B7:result", rb), rb); code != http.StatusOK {
		t.Fatalf("post result: %d %s", code, b)
	}

	// --- external service longpolls and gets the returned {R,S,V} ---
	code, b := cc.do(http.MethodGet, "/v1/requests/"+env.RequestID+"/result?wait=10s", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("result longpoll: %d %s", code, b)
	}
	var rr struct {
		Status string `json:"status"`
		RSV    string `json:"rsv"`
	}
	if err := json.Unmarshal(b, &rr); err != nil {
		t.Fatalf("decode result: %v (%s)", err, b)
	}
	if rr.Status != "RETURNED" {
		t.Fatalf("external service did not get RETURNED: status=%q body=%s", rr.Status, b)
	}
	gotRSV, _ := base64.StdEncoding.DecodeString(rr.RSV)
	if len(gotRSV) == 0 {
		t.Fatalf("RETURNED but empty rsv: %s", b)
	}
}
