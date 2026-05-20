package cli

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/pnet"

	"github.com/zzci/mpc/internal/contract"
)

// DM-5 host_transport tests (distributed-mpc-impl.md §B DM-5): the host-side
// libp2p adapter that satisfies sdk.WireCallbacks. The unit-level tests cover
// configuration validation, outbound routing (decode + per-tag dispatch) and
// the Pump feeder; the libp2p smoke proves a single envelope round-trips
// through real Noise + pnet + TCP to the addressed peer and back into a
// recording SDK fake. The "real 3-process keygen+sign+reshare" gate lives in
// the §G acceptance suite under tests/e2e/test/e2e-distributed-mpc (DM-6).

// fakeFeeder records every byte slice handed to OnWireMessage so the Pump
// test can assert what landed and in what order. It is the test-side stand-
// in for sdk.SDK / internal/mobileapi.SDK.
type fakeFeeder struct {
	mu     sync.Mutex
	got    [][]byte
	failNo int // when > 0, the N-th call returns a non-nil error
}

func (f *fakeFeeder) OnWireMessage(b []byte) error {
	cp := append([]byte(nil), b...)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, cp)
	if f.failNo > 0 && len(f.got) == f.failNo {
		return errSeam
	}
	return nil
}

// snapshot returns a copy of the recorded payloads so the assertion path does
// not hold the mutex while comparing.
func (f *fakeFeeder) snapshot() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]byte, len(f.got))
	for i, b := range f.got {
		out[i] = append([]byte(nil), b...)
	}
	return out
}

// errSeam is the canonical fake-feeder error so the test can distinguish a
// recorded handler failure from any other error in the chain.
var errSeam = &seamError{}

type seamError struct{}

func (*seamError) Error() string { return "test-seam: induced feed failure" }

// --- unit: NewHostTransport input validation ----------------------------

func TestNewHostTransport_RejectsEmptyConfig(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cases := []struct {
		name string
		mut  func(*HostTransportConfig)
		want string
	}{
		{"missing HostKey", func(c *HostTransportConfig) { c.HostKey = nil }, "HostKey is required"},
		{"missing PSK", func(c *HostTransportConfig) { c.PSK = nil }, "PSK is required"},
		{"missing MemberKey", func(c *HostTransportConfig) { c.MemberKey = nil }, "MemberKey is required"},
		{"missing SessionID", func(c *HostTransportConfig) { c.SessionID = "" }, "SessionID is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := goodHostCfg(t)
			tc.mut(&cfg)
			h, err := NewHostTransport(ctx, cfg)
			if err == nil {
				_ = h.Close()
				t.Fatalf("NewHostTransport: want error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("NewHostTransport: error %q, want substring %q", err, tc.want)
			}
		})
	}
}

// --- unit: OnWireMessage decode failures are recorded, not raised ---------

func TestOnWireMessage_RecordsDecodeFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h := mustHost(t, ctx, goodHostCfg(t))
	defer func() { _ = h.Close() }()

	h.OnWireMessage([]byte("{not-json"))
	if err := h.LastError(); err == nil || !strings.Contains(err.Error(), "decode outbound") {
		t.Fatalf("OnWireMessage: bad-json error = %v, want decode failure", err)
	}
}

func TestOnWireMessage_RecordsSessionDrift(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h := mustHost(t, ctx, goodHostCfg(t))
	defer func() { _ = h.Close() }()

	bz, err := json.Marshal(contract.MpcMessage{
		Version:   contract.MpcVersionV1,
		SessionID: "other-session",
		From:      "1",
		To:        []string{"2"},
		Payload:   []byte("ignored"),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	h.OnWireMessage(bz)
	if err := h.LastError(); err == nil || !strings.Contains(err.Error(), "session id drift") {
		t.Fatalf("OnWireMessage: drift error = %v, want session id drift", err)
	}
}

func TestOnWireMessage_RecordsUnknownPeer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h := mustHost(t, ctx, goodHostCfg(t))
	defer func() { _ = h.Close() }()

	bz, err := json.Marshal(contract.MpcMessage{
		Version:   contract.MpcVersionV1,
		SessionID: h.SessionID(),
		From:      "1",
		To:        []string{"999"}, // not in goodHostCfg peer table
		Payload:   []byte("ignored"),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	h.OnWireMessage(bz)
	if err := h.LastError(); err == nil || !strings.Contains(err.Error(), `no peer for tag "999"`) {
		t.Fatalf("OnWireMessage: unknown-peer error = %v", err)
	}
}

// --- unit: Pump is single-shot and refuses double Pump --------------------

func TestPump_RejectsNilFeeder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h := mustHost(t, ctx, goodHostCfg(t))
	defer func() { _ = h.Close() }()
	if err := h.Pump(ctx, nil); err == nil || !strings.Contains(err.Error(), "sdk is nil") {
		t.Fatalf("Pump(nil): err=%v, want sdk-is-nil", err)
	}
}

func TestPump_RejectsDoubleStart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h := mustHost(t, ctx, goodHostCfg(t))
	defer func() { _ = h.Close() }()

	if err := h.Pump(ctx, &fakeFeeder{}); err != nil {
		t.Fatalf("Pump#1: %v", err)
	}
	if err := h.Pump(ctx, &fakeFeeder{}); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("Pump#2: err=%v, want already-running", err)
	}
}

// --- libp2p smoke: directed send → recipient feeder receives JSON ---------

// TestHostTransport_LoopbackLibp2p is the DM-5 round-trip smoke: one envelope
// emitted by host A's OnWireMessage flows over real libp2p (Noise + pnet +
// TCP loopback) to host B's session, drains through B's Pump, and lands in
// a recording fake feeder. The senderAuth gate is exercised end-to-end (B's
// PeerResolver consults A's member pubkey, gate accepts, message reaches the
// feeder). No mpc / sdk imports here: this asserts the transport seam only.
func TestHostTransport_LoopbackLibp2p(t *testing.T) {
	if testing.Short() {
		t.Skip("host_transport libp2p smoke: real Noise + pnet + TCP; skipped in -short")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Two parties: "1" (A) and "2" (B); each runs one HostTransport.
	psk := makeSmokePSK(t)
	memberA := mustMemberKey(t)
	memberB := mustMemberKey(t)
	pubA := memberA.PubKey().SerializeCompressed()
	pubB := memberB.PubKey().SerializeCompressed()
	hostA := mustHostKey(t)
	hostB := mustHostKey(t)
	pidA, err := peer.IDFromPrivateKey(hostA)
	if err != nil {
		t.Fatalf("pidA: %v", err)
	}
	pidB, err := peer.IDFromPrivateKey(hostB)
	if err != nil {
		t.Fatalf("pidB: %v", err)
	}

	a, err := NewHostTransport(ctx, HostTransportConfig{
		HostKey:     hostA,
		PSK:         psk,
		MemberKey:   memberA,
		SessionID:   "smoke-loopback",
		Peers:       PeerTable{"1": pidA, "2": pidB},
		PeerPubKeys: map[string][]byte{"1": pubA, "2": pubB},
	})
	if err != nil {
		t.Fatalf("NewHostTransport A: %v", err)
	}
	defer func() { _ = a.Close() }()

	b, err := NewHostTransport(ctx, HostTransportConfig{
		HostKey:     hostB,
		PSK:         psk,
		MemberKey:   memberB,
		SessionID:   "smoke-loopback",
		Peers:       PeerTable{"1": pidA, "2": pidB},
		PeerPubKeys: map[string][]byte{"1": pubA, "2": pubB},
	})
	if err != nil {
		t.Fatalf("NewHostTransport B: %v", err)
	}
	defer func() { _ = b.Close() }()

	// Dial A→B directly so the directed-stream path can open.
	if err := a.Connect(ctx, peer.AddrInfo{ID: b.PeerID(), Addrs: b.ListenAddrs()}); err != nil {
		t.Fatalf("connect A->B: %v", err)
	}

	feeder := &fakeFeeder{}
	if err := b.Pump(ctx, feeder); err != nil {
		t.Fatalf("Pump B: %v", err)
	}

	mm := contract.MpcMessage{
		Version:   contract.MpcVersionV1,
		SessionID: "smoke-loopback",
		From:      "1",
		To:        []string{"2"},
		Payload:   []byte("hello-DM5"),
	}
	bz, err := json.Marshal(mm)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	a.OnWireMessage(bz)

	// Wait for one delivery (network + senderAuth + AcceptInbound).
	deadline := time.Now().Add(10 * time.Second)
	for {
		if got := feeder.snapshot(); len(got) >= 1 {
			var rt contract.MpcMessage
			if err := json.Unmarshal(got[0], &rt); err != nil {
				t.Fatalf("unmarshal delivered: %v", err)
			}
			if rt.SessionID != "smoke-loopback" || rt.From != "1" || string(rt.Payload) != "hello-DM5" {
				t.Fatalf("delivered envelope drift: %+v", rt)
			}
			if len(rt.SenderAuth) == 0 {
				t.Fatal("delivered envelope missing senderAuth (transport must sign on send)")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no inbound delivered: A.last=%v B.last=%v", a.LastError(), b.LastError())
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := a.LastError(); err != nil {
		t.Fatalf("A LastError: %v", err)
	}
	if err := b.LastError(); err != nil {
		t.Fatalf("B LastError: %v", err)
	}
}

// --- helpers --------------------------------------------------------------

// goodHostCfg is a well-formed HostTransportConfig for negative-test mutation.
// PeerPubKeys carries a single peer "1" (== self by tag) so the resolver has
// at least one valid entry; the Peers table is empty so OnWireMessage's
// unknown-peer path is reachable for negative tests.
func goodHostCfg(t *testing.T) HostTransportConfig {
	t.Helper()
	mk := mustMemberKey(t)
	return HostTransportConfig{
		HostKey:     mustHostKey(t),
		PSK:         makeSmokePSK(t),
		MemberKey:   mk,
		SessionID:   "unit-session",
		Peers:       PeerTable{},
		PeerPubKeys: map[string][]byte{"1": mk.PubKey().SerializeCompressed()},
	}
}

func mustHost(t *testing.T, ctx context.Context, cfg HostTransportConfig) *HostTransport {
	t.Helper()
	h, err := NewHostTransport(ctx, cfg)
	if err != nil {
		t.Fatalf("NewHostTransport: %v", err)
	}
	return h
}

func makeSmokePSK(t *testing.T) pnet.PSK {
	t.Helper()
	psk := make([]byte, 32)
	if _, err := rand.Read(psk); err != nil {
		t.Fatalf("psk: %v", err)
	}
	return psk
}

func mustMemberKey(t *testing.T) *btcec.PrivateKey {
	t.Helper()
	k, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("member key: %v", err)
	}
	return k
}

func mustHostKey(t *testing.T) crypto.PrivKey {
	t.Helper()
	priv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	if err != nil {
		t.Fatalf("host key: %v", err)
	}
	return priv
}
