package mobileapi

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"

	"github.com/zzci/mpc/internal/contract"
	"github.com/zzci/mpc/internal/mpc"

	tsskeygen "github.com/bnb-chain/tss-lib/v3/ecdsa/keygen"
)

// heavyWait is the per-operation callback wait ceiling. It is deliberately far
// larger than an isolated run needs: under the binding gate
// (`rtk test go test -race -count=1 ./...`) every package test binary runs
// concurrently, and the race detector slows the in-process MPC math roughly
// 3x while CPU is contended by the heavy internal/mpc binary — so the margin
// must absorb a multi-x slowdown without ever firing early. The whole package
// performs only ONE real keygen (shared, see committeeOnce) plus one reshare
// and two signs, so even at a large slowdown the package finishes well within
// `go test`'s 10m per-binary default.
const heavyWait = 8 * time.Minute

const (
	testParties   = 3
	testThreshold = 1 // 2-of-3
)

// testPassphrase is the at-rest seal passphrase used wherever a mobileapi test
// reaches keystore seal (keygen/reshare share sealing). It must satisfy the
// H-001 keystore strength policy now on main (>= 12 chars, many distinct
// runes, multiple character classes, not a common lowercase phrase); a weak
// literal like "pw" is hard-rejected by seal() post-H-001 merge.
const testPassphrase = "Mobileapi-Test-Pass-9x!"

// newTestSDK builds an SDK whose keygen/reshare uses tss-lib's bundled
// pre-param fixtures (the unexported test seam) so the suite does not pay the
// multi-minute safe-prime search. The fixtures are independent of (t, n);
// only their safe primes / Paillier key are consumed.
func newTestSDK(t *testing.T) *SDK {
	t.Helper()
	sdk, err := NewSDK(t.TempDir())
	if err != nil {
		t.Fatalf("NewSDK: %v", err)
	}
	fx, _, err := tsskeygen.LoadKeygenTestFixtures(testParties)
	if err != nil {
		t.Fatalf("load keygen fixtures (run tss-lib keygen tests to generate them): %v", err)
	}
	pre := make([]tsskeygen.LocalPreParams, testParties)
	for i := range pre {
		pre[i] = fx[i].LocalPreParams
	}
	sdk.preParams = pre
	return sdk
}

// recorder is a thread-safe callback spy implementing KeyGenCallback,
// SignCallback and ReshareCallback. It records call order and blocks the test
// until a terminal (OnResult/OnError) call.
type recorder struct {
	mu       sync.Mutex
	order    []string
	progress []string
	summary  string
	rsv      []byte
	decoded  []string
	code     string
	msg      string
	done     chan struct{}
	once     sync.Once
	decCh    chan struct{}
	decOnce  sync.Once
}

func newRecorder() *recorder {
	return &recorder{done: make(chan struct{}), decCh: make(chan struct{})}
}

// waitDecoded blocks until OnDecoded fired (or the test times out).
func (r *recorder) waitDecoded(t *testing.T) {
	t.Helper()
	select {
	case <-r.decCh:
	case <-r.done: // terminal without decode (security reject) — caller asserts
	case <-time.After(heavyWait):
		t.Fatal("OnDecoded never fired")
	}
}

func (r *recorder) finish() { r.once.Do(func() { close(r.done) }) }

func (r *recorder) OnProgress(stage string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, "progress")
	r.progress = append(r.progress, stage)
}

func (r *recorder) OnResult(arg interface{}) {
	r.mu.Lock()
	r.order = append(r.order, "result")
	switch v := arg.(type) {
	case string:
		r.summary = v
	case []byte:
		r.rsv = v
	}
	r.mu.Unlock()
	r.finish()
}

// OnResult has two flavors across the callback interfaces (string summary vs
// []byte rsv); recorder satisfies both via dedicated methods.
func (r *recorder) onResultStr(s string) { r.OnResult(s) }
func (r *recorder) onResultRSV(b []byte) { r.OnResult(b) }

func (r *recorder) OnDecoded(aFactsJSON, bInfoJSON, mismatchJSON string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, "decoded")
	r.decoded = []string{aFactsJSON, bInfoJSON, mismatchJSON}
	r.decOnce.Do(func() { close(r.decCh) })
}

func (r *recorder) OnError(code, msg string) {
	r.mu.Lock()
	r.order = append(r.order, "error")
	r.code = code
	r.msg = msg
	r.mu.Unlock()
	r.finish()
}

func (r *recorder) wait(t *testing.T) {
	t.Helper()
	if !r.waitFor(heavyWait) {
		t.Fatalf("callback did not reach a terminal state in time; order=%v", r.snapOrder())
	}
}

// waitFor blocks until a terminal callback or d elapses; ok is false on
// timeout. It takes no *testing.T so the shared-committee bootstrap can use it
// off the first caller's test goroutine.
func (r *recorder) waitFor(d time.Duration) (ok bool) {
	select {
	case <-r.done:
		return true
	case <-time.After(d):
		return false
	}
}

func (r *recorder) snapOrder() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// result returns the terminal outcome fields under the lock, so tests never
// read recorder state from the test goroutine while the callback goroutine
// may still be writing it (keeps the suite clean under -race).
func (r *recorder) result() (code, msg, summary string, rsv []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.code, r.msg, r.summary, append([]byte(nil), r.rsv...)
}

func (r *recorder) progressSnap() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.progress...)
}

// kgAdapter / rsAdapter bridge the recorder to the string-summary OnResult
// shape KeyGenCallback / ReshareCallback require.
type kgAdapter struct{ *recorder }

func (a kgAdapter) OnResult(s string) { a.onResultStr(s) }

type rsAdapter struct{ *recorder }

func (a rsAdapter) OnResult(s string) { a.onResultStr(s) }

// signAdapter bridges the recorder to the []byte OnResult SignCallback needs.
type signAdapter struct{ *recorder }

func (a signAdapter) OnResult(b []byte) { a.onResultRSV(b) }

// committee is the single real in-process keygen the whole package shares.
// Running the heavy fast=false keygen exactly once (instead of per test that
// needs a signing committee) is the core robustness lever under the full-tree
// -race gate: it removes almost all of the package's CPU-bound MPC work and
// the contention that made the merged-tree run flake (B-001 RED retry).
type committee struct {
	sdk      *SDK // the SDK that ran the real flat KeyGen (keystore sealed)
	keystore string
	summary  keygenSummary
	order    []string
	progress []string
	pw       string
}

var (
	committeeOnce sync.Once
	sharedComm    *committee
	committeeErr  string
)

// sharedCommittee runs the one real flat-API keygen for the package (once) and
// returns the cached result. Tests are not run in parallel within the package,
// so the sync.Once body is reached serially — there is no intra-package
// concurrency around it.
func sharedCommittee(t *testing.T) *committee {
	t.Helper()
	committeeOnce.Do(buildSharedCommittee)
	if sharedComm == nil {
		t.Fatalf("shared committee bootstrap failed: %s", committeeErr)
	}
	return sharedComm
}

func buildSharedCommittee() {
	dir, err := os.MkdirTemp("", "mobileapi-committee")
	if err != nil {
		committeeErr = "mkdtemp: " + err.Error()
		return
	}
	sdk, err := NewSDK(dir)
	if err != nil {
		committeeErr = "NewSDK: " + err.Error()
		return
	}
	fx, _, err := tsskeygen.LoadKeygenTestFixtures(testParties)
	if err != nil {
		committeeErr = "load fixtures: " + err.Error()
		return
	}
	pre := make([]tsskeygen.LocalPreParams, testParties)
	for i := range pre {
		pre[i] = fx[i].LocalPreParams
	}
	sdk.preParams = pre

	r := newRecorder()
	cfg, _ := json.Marshal(keygenConfig{Threshold: testThreshold, Parties: testParties, Passphrase: testPassphrase})
	sdk.KeyGen(string(cfg), kgAdapter{r})
	if !r.waitFor(heavyWait) {
		committeeErr = "keygen did not finish within heavyWait"
		return
	}
	code, msg, summary, _ := r.result()
	if code != "" {
		committeeErr = "keygen errored: " + code + " " + msg
		return
	}
	var s keygenSummary
	if err := json.Unmarshal([]byte(summary), &s); err != nil {
		committeeErr = "parse summary: " + err.Error()
		return
	}
	sharedComm = &committee{
		sdk:      sdk,
		keystore: dir,
		summary:  s,
		order:    r.snapOrder(),
		progress: r.progressSnap(),
		pw:       testPassphrase,
	}
}

// committeeSDK returns a fresh SDK (own temp keystore) pre-loaded with the
// shared committee's shares, so Sign/Reshare tests exercise their real flow
// without paying for another keygen.
func committeeSDK(t *testing.T) *SDK {
	t.Helper()
	c := sharedCommittee(t)
	sdk, err := NewSDK(t.TempDir())
	if err != nil {
		t.Fatalf("NewSDK: %v", err)
	}
	fx, _, err := tsskeygen.LoadKeygenTestFixtures(testParties)
	if err != nil {
		t.Fatalf("load fixtures: %v", err)
	}
	pre := make([]tsskeygen.LocalPreParams, testParties)
	for i := range pre {
		pre[i] = fx[i].LocalPreParams
	}
	sdk.preParams = pre

	src, _, ok := c.sdk.snapshotShares()
	if !ok {
		t.Fatal("shared committee holds no shares")
	}
	shares := make([]mpc.Share, len(src))
	copy(shares, src)
	sdk.setGroup(shares, testThreshold, c.summary.GroupPubKey)
	return sdk
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex %q: %v", s, err)
	}
	return b
}

func hexEncode(b []byte) string { return hex.EncodeToString(b) }

// EIP-155 spec external anchor (nonce=9, gasPrice=20e9, gas=21000,
// to=0x3535..35, value=1e18, chainId=1): the signing RLP and its keccak256
// are fixed by the spec, so this is a real digest-bound envelope body.
const (
	eip155RLP    = "ec098504a817c800825208943535353535353535353535353535353535353535880de0b6b3a764000080018080"
	eip155Digest = "daf5a779ae972f972197303d7b574746c7ef83eadac0f2791ad23db92e4c8e53"
)

// buildStart assembles a START whose envelope passes every device check:
// version, metaHash (absent → EmptyMetaHash), proposerSig over the canonical
// preimage, and a recomputable EVM digest == digest32. mutate may tamper with
// the envelope/START before (re)signing decisions are finalized.
func buildStart(t *testing.T, mutate func(*contract.StartSigning, *btcec.PrivateKey)) string {
	t.Helper()
	priv, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("priv: %v", err)
	}
	const reqID = "11111111-1111-1111-1111-111111111111"
	now := time.Now().UnixMilli()
	env := contract.SigningRequest{
		Version:    contract.EnvelopeVersionV1,
		RequestID:  reqID,
		GroupID:    "grp-1",
		Chain:      "eth",
		UnsignedTx: mustHex(t, eip155RLP),
		Digest32:   mustHex(t, eip155Digest),
		Proposer:   hexEncode(priv.PubKey().SerializeCompressed()),
		CreatedAt:  now - 1000,
		Expiry:     now + 600_000,
		MetaHash:   contract.EmptyMetaHash[:],
	}
	st := contract.StartSigning{
		RequestID: env.RequestID,
		Envelope:  env,
		Signers:   []string{"m1", "m2"},
		SelfRole:  true,
		Deadline:  now + 600_000,
	}
	if mutate != nil {
		mutate(&st, priv)
	}
	if st.Envelope.ProposerSig == nil {
		if err := contract.SignEnvelope(priv, &st.Envelope); err != nil {
			t.Fatalf("SignEnvelope: %v", err)
		}
	}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal START: %v", err)
	}
	return string(b)
}
