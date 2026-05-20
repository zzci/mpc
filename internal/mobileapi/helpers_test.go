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

// testSessionID is the canonical sessionId every multi-party fabric uses for
// keygen / reshare. It is a stable string so test wires are easy to read; the
// R5 gate enforces equality across every device in one ring.
const testSessionID = "test-session-keygen-0001"

// testMembers is the canonical memberSet for the shared 3-party committee:
// stable strings the participantsFor() mapper consults to map memberId →
// 0-based partyIndex.
var testMembers = []string{"m1", "m2", "m3"}

// testRelay is the host-relay coordinates every configJSON ships; the SDK
// itself never dials, but the schema mandates non-empty values so any old
// configJSON missing it is hard-cut.
var testRelay = relayConfig{
	PeerID: "12D3KooWTestRelay000000000000000000000000000000",
	Addrs:  []string{"/ip4/127.0.0.1/tcp/0"},
}

// newTestSDK builds an SDK whose keygen/reshare uses tss-lib's bundled
// pre-param fixtures (the unexported test seam) so the suite does not pay the
// multi-minute safe-prime search. The fixtures are independent of (t, n);
// only their safe primes / Paillier key are consumed.
//
// partyIndex selects which fixture entry this SDK consumes — under DM-3 each
// device runs a single party and therefore needs only one LocalPreParams
// record (the one matching its position in the committee).
func newTestSDK(t *testing.T, partyIndex int) *SDK {
	t.Helper()
	sdk, err := NewSDK(t.TempDir())
	if err != nil {
		t.Fatalf("NewSDK: %v", err)
	}
	fx, _, err := tsskeygen.LoadKeygenTestFixtures(testParties)
	if err != nil {
		t.Fatalf("load keygen fixtures (run tss-lib keygen tests to generate them): %v", err)
	}
	if partyIndex < 0 || partyIndex >= len(fx) {
		t.Fatalf("partyIndex %d outside fixture range", partyIndex)
	}
	sdk.preParams = []tsskeygen.LocalPreParams{fx[partyIndex].LocalPreParams}
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

// kgAdapter / rsAdapter bridge the recorder to the string-summary OnResult
// shape KeyGenCallback / ReshareCallback require.
type kgAdapter struct{ *recorder }

func (a kgAdapter) OnResult(s string) { a.onResultStr(s) }

type rsAdapter struct{ *recorder }

func (a rsAdapter) OnResult(s string) { a.onResultStr(s) }

// signAdapter bridges the recorder to the []byte OnResult SignCallback needs.
type signAdapter struct{ *recorder }

func (a signAdapter) OnResult(b []byte) { a.onResultRSV(b) }

// --- ring fabric ---------------------------------------------------------

// ringFabric simulates the host transport layer for a committee of N SDKs.
// Each SDK plugs in a WireCallbacks whose OnWireMessage hands the bytes back
// to the fabric, which routes them to every addressed peer SDK via
// SDK.OnWireMessage. The fabric is the test-only stand-in for DM-5's PC CLI
// libp2p host and the mobile native bridge: it owns transport, the SDK only
// produces / consumes wire bytes.
type ringFabric struct {
	t    *testing.T
	sdks []*SDK
	// errs collects routing errors observed by OnWireMessage; checked at
	// test end so a stray malformed envelope doesn't pass silently.
	mu   sync.Mutex
	errs []error
}

func newRingFabric(t *testing.T, sdks []*SDK) *ringFabric {
	t.Helper()
	return &ringFabric{t: t, sdks: sdks}
}

// wcFor returns the WireCallbacks an SDK at index from plugs into KeyGen /
// Sign / Reshare. Routing inspects MpcMessage.To plus IsBroadcast: a
// broadcast is fanned out to every other SDK; a directed envelope goes to
// the addressed party (after stripping the reshare committee marker, if
// present).
func (f *ringFabric) wcFor(from int) WireCallbacks {
	return ringWC{f: f, from: from}
}

func (f *ringFabric) recordErr(err error) {
	if err == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs = append(f.errs, err)
}

// assertNoErrs reports any routing error the fabric recorded against t.
// Callers pass their own *testing.T because the package-level bootstrap
// fabric (buildSharedCommittee) has no test handle of its own.
func (f *ringFabric) assertNoErrs(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, err := range f.errs {
		t.Errorf("fabric routing error: %v", err)
	}
}

// deliver routes one outbound envelope to every recipient SDK in the ring
// (broadcast → every-other; directed → addressed parties). The wire payload
// is the JSON-encoded MpcMessage the SDK emitted via WireCallbacks.
func (f *ringFabric) deliver(from int, b []byte) {
	var mm contract.MpcMessage
	if err := json.Unmarshal(b, &mm); err != nil {
		f.recordErr(err)
		return
	}
	dests := map[int]bool{}
	if mm.IsBroadcast && len(mm.To) == 0 {
		for i := range f.sdks {
			if i == from {
				continue
			}
			dests[i] = true
		}
	} else {
		for _, t := range mm.To {
			if idx := destIndex(t); idx >= 0 && idx < len(f.sdks) {
				dests[idx] = true
			}
		}
	}
	for idx := range dests {
		// Re-marshal a fresh copy: SDKs may inspect the bytes byte-for-byte
		// on R5 / AcceptInbound, and we want to mirror real transport
		// fan-out which delivers each peer its own copy.
		out, err := json.Marshal(mm)
		if err != nil {
			f.recordErr(err)
			continue
		}
		if err := f.sdks[idx].OnWireMessage(out); err != nil {
			f.recordErr(err)
		}
	}
}

// destIndex parses one addressed-To tag into a 0-based party index, stripping
// the reshare committee marker ('O' / 'N' / 'B') if present. A malformed tag
// returns -1.
func destIndex(tag string) int {
	if tag == "" {
		return -1
	}
	if tag[0] == 'O' || tag[0] == 'N' || tag[0] == 'B' {
		tag = tag[1:]
	}
	n := 0
	for _, c := range tag {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return -1
	}
	return n - 1
}

// ringWC is one SDK's WireCallbacks implementation. The fabric routes the
// emitted bytes to every addressed peer SDK.
type ringWC struct {
	f    *ringFabric
	from int
}

func (rw ringWC) OnWireMessage(b []byte) {
	// Copy the bytes: the SDK may reuse its underlying buffer after the call
	// returns (gomobile-style fire-and-forget); the fabric routes a stable
	// copy so concurrent recipients all see the same payload.
	cp := append([]byte(nil), b...)
	rw.f.deliver(rw.from, cp)
}

// noopWire is a no-op WireCallbacks for tests that exercise only the
// pre-MPC paths (configJSON validation, security rejects); a Sign that
// never enters MPC never emits anything, so dropping outbound bytes is safe.
type noopWire struct{}

func (noopWire) OnWireMessage([]byte) {}

// --- shared committee ----------------------------------------------------

// committee is the single real distributed-MPC keygen the whole package
// shares. Running the heavy fast=false keygen exactly once across N SDKs
// (instead of per test that needs a signing committee) is the core
// robustness lever under the full-tree -race gate: it removes almost all of
// the package's CPU-bound MPC work and the contention that made the
// merged-tree run flake (B-001 RED retry).
type committee struct {
	sdks    []*SDK // one SDK per device, each holding only its own share_i
	fabric  *ringFabric
	summary keygenSummary // the partyIndex=0 device's summary
	groupID string
	pw      string
}

var (
	committeeOnce sync.Once
	sharedComm    *committee
	committeeErr  string
)

// sharedCommittee runs the one real distributed keygen for the package
// (once) and returns the cached result. Tests are not run in parallel within
// the package, so the sync.Once body is reached serially — there is no
// intra-package concurrency around it.
func sharedCommittee(t *testing.T) *committee {
	t.Helper()
	committeeOnce.Do(buildSharedCommittee)
	if sharedComm == nil {
		t.Fatalf("shared committee bootstrap failed: %s", committeeErr)
	}
	return sharedComm
}

func buildSharedCommittee() {
	groupID := "shared-grp-1"
	dirs := make([]string, testParties)
	for i := range dirs {
		d, err := os.MkdirTemp("", "mobileapi-committee")
		if err != nil {
			committeeErr = "mkdtemp: " + err.Error()
			return
		}
		dirs[i] = d
	}

	fx, _, err := tsskeygen.LoadKeygenTestFixtures(testParties)
	if err != nil {
		committeeErr = "load fixtures: " + err.Error()
		return
	}

	sdks := make([]*SDK, testParties)
	for i := 0; i < testParties; i++ {
		sdk, err := NewSDK(dirs[i])
		if err != nil {
			committeeErr = "NewSDK: " + err.Error()
			return
		}
		sdk.preParams = []tsskeygen.LocalPreParams{fx[i].LocalPreParams}
		sdks[i] = sdk
	}

	// Test-only fabric stand-in for testing.T: we cannot call newRingFabric
	// without a *testing.T, but the shared committee bootstrap runs once
	// for the package. We embed a minimal harness with its own error sink.
	fabric := &ringFabric{sdks: sdks}

	recs := make([]*recorder, testParties)
	wg := sync.WaitGroup{}
	wg.Add(testParties)
	for i := 0; i < testParties; i++ {
		recs[i] = newRecorder()
		cfg := keygenConfigPayload(groupID, testSessionID, i, testParties, testThreshold, testMembers, testRelay, testPassphrase)
		raw, err := json.Marshal(cfg)
		if err != nil {
			committeeErr = "marshal cfg: " + err.Error()
			return
		}
		idx := i
		go func() {
			defer wg.Done()
			sdks[idx].KeyGen(string(raw), fabric.wcFor(idx), kgAdapter{recs[idx]})
		}()
	}

	// Each recorder finishes when its KeyGen's OnResult/OnError fires.
	doneCh := make(chan struct{})
	go func() { wg.Wait(); close(doneCh) }()
	for i := 0; i < testParties; i++ {
		if !recs[i].waitFor(heavyWait) {
			committeeErr = "keygen did not finish within heavyWait"
			return
		}
		if code, _, _, _ := recs[i].result(); code != "" {
			_, msg, _, _ := recs[i].result()
			committeeErr = "keygen errored on party " + reshareTagFor(i) + ": " + code + " " + msg
			return
		}
	}
	<-doneCh

	var sum keygenSummary
	if _, _, s, _ := recs[0].result(); s != "" {
		if err := json.Unmarshal([]byte(s), &sum); err != nil {
			committeeErr = "parse summary: " + err.Error()
			return
		}
	}
	sharedComm = &committee{
		sdks:    sdks,
		fabric:  fabric,
		summary: sum,
		groupID: groupID,
		pw:      testPassphrase,
	}
}

// committeeSDKs returns the per-device SDK slice from the shared keygen, plus
// a fresh fabric scoped to the caller's *testing.T that wires them together
// for downstream Sign / Reshare runs. Tests that mutate state (e.g. Reshare
// overwrites the shares) must do so against this slice and remain mindful of
// test ordering — the package-level sharedComm is consumed by reference.
func committeeSDKs(t *testing.T) ([]*SDK, *ringFabric, *committee) {
	t.Helper()
	c := sharedCommittee(t)
	return c.sdks, newRingFabric(t, c.sdks), c
}

// keygenConfigPayload builds one device's KeyGen configJSON for the shared
// committee bootstrap.
func keygenConfigPayload(groupID, sessionID string, partyIndex, n, t int, members []string, relay relayConfig, passphrase string) keygenConfig {
	role := "keygen"
	return keygenConfig{
		GroupID:    sptr(groupID),
		SessionID:  sptr(sessionID),
		PartyIndex: iptr(partyIndex),
		N:          iptr(n),
		T:          iptr(t),
		MemberSet:  append([]string(nil), members...),
		Relay:      &relayConfig{PeerID: relay.PeerID, Addrs: append([]string(nil), relay.Addrs...)},
		Role:       sptr(role),
		Passphrase: passphrase,
	}
}

// reshareConfigPayload builds one device's Reshare configJSON.
func reshareConfigPayload(groupID, sessionID string, partyIndex, n, oldT, newT int, members []string, relay relayConfig, passphrase string) reshareConfig {
	role := "reshare"
	return reshareConfig{
		GroupID:    sptr(groupID),
		SessionID:  sptr(sessionID),
		PartyIndex: iptr(partyIndex),
		N:          iptr(n),
		OldT:       iptr(oldT),
		NewT:       iptr(newT),
		MemberSet:  append([]string(nil), members...),
		Relay:      &relayConfig{PeerID: relay.PeerID, Addrs: append([]string(nil), relay.Addrs...)},
		Role:       sptr(role),
		Passphrase: passphrase,
	}
}

// signConfigPayload builds one device's Sign configJSON wrapping the coord-
// delivered StartSigning.
func signConfigPayload(groupID string, partyIndex, n, t int, members []string, relay relayConfig, start *contract.StartSigning) signConfig {
	role := "signer"
	sid := start.RequestID
	return signConfig{
		GroupID:    sptr(groupID),
		SessionID:  sptr(sid),
		PartyIndex: iptr(partyIndex),
		N:          iptr(n),
		T:          iptr(t),
		MemberSet:  append([]string(nil), members...),
		Relay:      &relayConfig{PeerID: relay.PeerID, Addrs: append([]string(nil), relay.Addrs...)},
		Role:       sptr(role),
		Start:      start,
	}
}

func sptr(s string) *string { return &s }
func iptr(i int) *int       { return &i }

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
//
// signers is the memberId set that participates in this signing session
// (must be a subset of testMembers); the default test signing committee is
// the first t+1 = 2 members. The returned StartSigning's RequestID is the
// canonical signing sessionId.
func buildStart(t *testing.T, signers []string, mutate func(*contract.StartSigning, *btcec.PrivateKey)) *contract.StartSigning {
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
	if signers == nil {
		signers = append([]string(nil), testMembers[:testThreshold+1]...)
	}
	st := contract.StartSigning{
		RequestID:  env.RequestID,
		Envelope:   env,
		Signers:    signers,
		SelfRole:   true,
		Deadline:   now + 600_000,
		RelayHints: []string{testRelay.Addrs[0]},
	}
	if mutate != nil {
		mutate(&st, priv)
	}
	if st.Envelope.ProposerSig == nil {
		if err := contract.SignEnvelope(priv, &st.Envelope); err != nil {
			t.Fatalf("SignEnvelope: %v", err)
		}
	}
	return &st
}

// marshalConfig serializes any of the *Config payloads to a configJSON
// string. Centralized so a test cannot drift from how the SDK consumes the
// wire envelope.
func marshalConfig(t *testing.T, cfg any) string {
	t.Helper()
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal cfg: %v", err)
	}
	return string(b)
}
