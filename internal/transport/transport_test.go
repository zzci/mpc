package transport

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/libp2p/go-libp2p"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/pnet"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	noise "github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"

	"github.com/royqta/mcp-wallet/internal/contract"
)

// --- helpers ---

func testPSK(t *testing.T) pnet.PSK {
	t.Helper()
	psk := make([]byte, 32)
	if _, err := rand.Read(psk); err != nil {
		t.Fatalf("psk: %v", err)
	}
	return psk
}

func genHostKey(t *testing.T) libp2pcrypto.PrivKey {
	t.Helper()
	priv, _, err := libp2pcrypto.GenerateSecp256k1Key(rand.Reader)
	if err != nil {
		t.Fatalf("host key: %v", err)
	}
	return priv
}

func genMemberKey(t *testing.T) *btcec.PrivateKey {
	t.Helper()
	k, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("member key: %v", err)
	}
	return k
}

func newTestTransport(ctx context.Context, t *testing.T, psk pnet.PSK, relays ...peer.AddrInfo) *Transport {
	t.Helper()
	tr, err := New(ctx, Config{HostKey: genHostKey(t), PSK: psk, Relays: relays})
	if err != nil {
		t.Fatalf("New transport: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return tr
}

// newRelayHost builds a bare circuit-relay v2 service host on the same pnet so
// it can only forward the encrypted stream (docs/design/contract/protocol.md:36).
func newRelayHost(t *testing.T, psk pnet.PSK) host.Host {
	t.Helper()
	h, err := libp2p.New(
		libp2p.Identity(genHostKey(t)),
		libp2p.PrivateNetwork(psk),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Muxer(yamux.ID, yamux.DefaultTransport),
		libp2p.NoTransports,
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
		libp2p.EnableRelay(),
		libp2p.EnableRelayService(),
		// The relay v2 service only starts once the host is judged publicly
		// reachable; force it so the loopback test relay actually serves HOP.
		libp2p.ForceReachabilityPublic(),
	)
	if err != nil {
		t.Fatalf("relay host: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func connect(ctx context.Context, t *testing.T, a, b *Transport) {
	t.Helper()
	if err := a.Connect(ctx, peer.AddrInfo{ID: b.ID(), Addrs: b.Addrs()}); err != nil {
		t.Fatalf("connect: %v", err)
	}
}

func resolver(keys map[string]*btcec.PrivateKey) PeerResolver {
	return func(partyID string) ([]byte, error) {
		k, ok := keys[partyID]
		if !ok {
			return nil, contract.ErrBadSignature
		}
		return k.PubKey().SerializeCompressed(), nil
	}
}

func joinSession(ctx context.Context, t *testing.T, tr *Transport, sid, self string, keys map[string]*btcec.PrivateKey) *Session {
	t.Helper()
	s, err := tr.JoinSession(ctx, SessionConfig{
		SessionID: sid,
		MemberKey: keys[self],
		Resolve:   resolver(keys),
	})
	if err != nil {
		t.Fatalf("JoinSession: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func recvWithin(s *Session, d time.Duration) (Inbound, bool) {
	select {
	case in := <-s.Inbound():
		return in, true
	case <-time.After(d):
		return Inbound{}, false
	}
}

func twoKeys(t *testing.T) map[string]*btcec.PrivateKey {
	return map[string]*btcec.PrivateKey{"A": genMemberKey(t), "B": genMemberKey(t)}
}

// --- tests ---

func TestPointToPoint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	psk := testPSK(t)
	keys := twoKeys(t)

	a := newTestTransport(ctx, t, psk)
	b := newTestTransport(ctx, t, psk)
	connect(ctx, t, a, b)

	sb := joinSession(ctx, t, b, "sess-1", "B", keys)
	sa := joinSession(ctx, t, a, "sess-1", "A", keys)

	msg := &contract.MpcMessage{From: "A", To: []string{"B"}, Round: 3, Payload: []byte("round-3-wire")}
	if err := sa.SendTo(ctx, b.ID(), msg); err != nil {
		t.Fatalf("SendTo: %v", err)
	}

	in, ok := recvWithin(sb, 5*time.Second)
	if !ok {
		t.Fatal("directed message not received")
	}
	if in.From != a.ID() {
		t.Fatalf("From = %s, want %s", in.From, a.ID())
	}
	if in.Msg.From != "A" || in.Msg.Round != 3 || string(in.Msg.Payload) != "round-3-wire" {
		t.Fatalf("payload mismatch: %+v", in.Msg)
	}
	if in.Msg.SessionID != "sess-1" || in.Msg.Version != contract.MpcVersionV1 {
		t.Fatalf("session/version not stamped: %+v", in.Msg)
	}
}

func TestBroadcast(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	psk := testPSK(t)
	keys := twoKeys(t)

	a := newTestTransport(ctx, t, psk)
	b := newTestTransport(ctx, t, psk)
	connect(ctx, t, a, b)

	sb := joinSession(ctx, t, b, "sess-bcast", "B", keys)
	sa := joinSession(ctx, t, a, "sess-bcast", "A", keys)

	// GossipSub needs a heartbeat or two to form the mesh; publish until the
	// subscriber sees it or the deadline passes.
	deadline := time.After(20 * time.Second)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		msg := &contract.MpcMessage{From: "A", IsBroadcast: true, Round: 1, Payload: []byte("bcast")}
		if err := sa.Broadcast(ctx, msg); err != nil {
			t.Fatalf("Broadcast: %v", err)
		}
		select {
		case in := <-sb.Inbound():
			if string(in.Msg.Payload) != "bcast" || in.Msg.From != "A" {
				t.Fatalf("bad broadcast payload: %+v", in.Msg)
			}
			return
		case <-tick.C:
			continue
		case <-deadline:
			t.Fatal("broadcast not received before deadline")
		}
	}
}

func TestCrossSessionDrop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	psk := testPSK(t)
	keys := twoKeys(t)

	a := newTestTransport(ctx, t, psk)
	b := newTestTransport(ctx, t, psk)
	connect(ctx, t, a, b)

	sb := joinSession(ctx, t, b, "sess-keep", "B", keys)
	sa := joinSession(ctx, t, a, "sess-other", "A", keys)

	// A sends on its own session; B's gate expects "sess-keep" and must drop
	// the "sess-other" message (docs/design/contract/protocol.md:53).
	msg := &contract.MpcMessage{From: "A", To: []string{"B"}, Round: 1, Payload: []byte("x")}
	if err := sa.SendTo(ctx, b.ID(), msg); err != nil {
		t.Fatalf("SendTo: %v", err)
	}
	if _, ok := recvWithin(sb, 2*time.Second); ok {
		t.Fatal("cross-session message must be dropped, but was delivered")
	}
}

func TestVersionRejectProtocolNegotiation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	psk := testPSK(t)
	keys := twoKeys(t)

	a := newTestTransport(ctx, t, psk)
	b := newTestTransport(ctx, t, psk)
	connect(ctx, t, a, b)
	joinSession(ctx, t, b, "sess-1", "B", keys) // registers only /tss/mpc/1.0.0

	// An unrecognised /tss/mpc major must be rejected by multistream, not
	// silently downgraded (docs/design/contract/protocol.md:86).
	_, err := a.Host().NewStream(ctx, b.ID(), protocol.ID(contract.ProtocolMPCPrefix+"2.0.0"))
	if err == nil {
		t.Fatal("opening an unsupported /tss/mpc version must fail")
	}
}

func TestVersionRejectMessage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	psk := testPSK(t)
	keys := twoKeys(t)

	a := newTestTransport(ctx, t, psk)
	b := newTestTransport(ctx, t, psk)
	connect(ctx, t, a, b)
	sb := joinSession(ctx, t, b, "sess-1", "B", keys)

	// Hand-craft a frame on the valid protocol but with an unsupported
	// MpcMessage.Version: the receive gate must drop it.
	bad := &contract.MpcMessage{Version: 99, SessionID: "sess-1", From: "A", Round: 1, Payload: []byte("x")}
	if err := contract.SignSenderAuth(keys["A"], bad); err != nil {
		t.Fatalf("sign: %v", err)
	}
	stream, err := a.Host().NewStream(ctx, b.ID(), protocol.ID(contract.ProtocolMPCV1))
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	if err := writeFrame(stream, bad); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	_ = stream.Close()

	if _, ok := recvWithin(sb, 2*time.Second); ok {
		t.Fatal("unsupported-version message must be dropped")
	}
}

func TestSenderAuthRejectsForgedMember(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	psk := testPSK(t)
	keys := twoKeys(t)

	a := newTestTransport(ctx, t, psk)
	b := newTestTransport(ctx, t, psk)
	connect(ctx, t, a, b)
	sb := joinSession(ctx, t, b, "sess-1", "B", keys)

	// Sign with a key that is not "A"'s registered member key: senderAuth
	// verification must fail and the message be dropped
	// (docs/design/contract/protocol.md:54).
	forged := &contract.MpcMessage{Version: contract.MpcVersionV1, SessionID: "sess-1", From: "A", Round: 1, Payload: []byte("x")}
	if err := contract.SignSenderAuth(genMemberKey(t), forged); err != nil {
		t.Fatalf("sign: %v", err)
	}
	stream, err := a.Host().NewStream(ctx, b.ID(), protocol.ID(contract.ProtocolMPCV1))
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	if err := writeFrame(stream, forged); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	_ = stream.Close()

	if _, ok := recvWithin(sb, 2*time.Second); ok {
		t.Fatal("forged senderAuth message must be dropped")
	}
}

func TestRelayMediatedPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	psk := testPSK(t)
	keys := twoKeys(t)

	relay := newRelayHost(t, psk)
	relayInfo := peer.AddrInfo{ID: relay.ID(), Addrs: relay.Addrs()}

	a := newTestTransport(ctx, t, psk, relayInfo)
	b := newTestTransport(ctx, t, psk, relayInfo)

	if err := b.ReserveRelays(ctx); err != nil {
		t.Fatalf("B reserve: %v", err)
	}
	if err := a.ReserveRelays(ctx); err != nil {
		t.Fatalf("A reserve: %v", err)
	}

	sb := joinSession(ctx, t, b, "sess-relay", "B", keys)
	sa := joinSession(ctx, t, a, "sess-relay", "A", keys)

	// A reaches B only through the relay circuit (A was never given B's
	// direct addrs): docs/design/contract/protocol.md:36.
	if err := a.ConnectVia(ctx, relay.ID(), b.ID()); err != nil {
		t.Fatalf("ConnectVia: %v", err)
	}

	msg := &contract.MpcMessage{From: "A", To: []string{"B"}, Round: 7, Payload: []byte("via-relay")}
	if err := sa.SendTo(ctx, b.ID(), msg); err != nil {
		t.Fatalf("SendTo via relay: %v", err)
	}
	in, ok := recvWithin(sb, 10*time.Second)
	if !ok {
		t.Fatal("relayed message not received")
	}
	if string(in.Msg.Payload) != "via-relay" || in.Msg.Round != 7 {
		t.Fatalf("relayed payload mismatch: %+v", in.Msg)
	}
}
