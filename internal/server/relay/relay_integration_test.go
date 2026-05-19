package relay

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	btcec "github.com/btcsuite/btcd/btcec/v2"
	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	yamux "github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	circuitclient "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	noise "github.com/libp2p/go-libp2p/p2p/security/noise"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/zzci/mpc/internal/contract"
	"github.com/zzci/mpc/internal/server"
)

const testPSKHex = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func testPSK(t *testing.T) []byte {
	t.Helper()
	k, err := hex.DecodeString(testPSKHex)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// makeRelay builds a relay from a Relay-only server.Config (no coord field set):
// this is itself the coord-independence assertion (server.md R5).
func makeRelay(t *testing.T, perToken, perGroup int, groupPubB64 string, rdv bool) *Relay {
	t.Helper()
	t.Setenv("TEST_RELAY_PSK", testPSKHex)
	cfg := server.Config{
		Relay: server.RelayConfig{
			Enable:  true,
			Listen:  []string{"/ip4/127.0.0.1/tcp/0"},
			PnetPSK: "env:TEST_RELAY_PSK",
			TokenVerify: server.TokenVerifyConfig{
				GroupPubkeys: []string{groupPubB64},
			},
			Rendezvous: server.RendezvousConfig{Enable: rdv},
			Limits: server.RelayLimitsConfig{
				ReservationPerToken: perToken,
				ReservationPerGroup: perGroup,
				BandwidthPerConn:    "1MiB/s",
			},
		},
	}
	r, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("relay.New: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func relayInfo(r *Relay) peer.AddrInfo {
	return peer.AddrInfo{ID: r.host.ID(), Addrs: r.host.Addrs()}
}

func makeClient(t *testing.T, psk []byte) host.Host {
	t.Helper()
	opts := []libp2p.Option{
		libp2p.Security(noise.ID, noise.New),
		libp2p.Muxer(yamux.ID, yamux.DefaultTransport),
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
		libp2p.EnableRelay(),
	}
	if psk != nil {
		opts = append(opts, libp2p.PrivateNetwork(psk))
	}
	h, err := libp2p.New(opts...)
	if err != nil {
		t.Fatalf("client host: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func capTokenFor(t *testing.T, priv *btcec.PrivateKey, group, member string, scope contract.CapScope) *contract.CapToken {
	t.Helper()
	now := time.Now().UnixMilli()
	tok := &contract.CapToken{
		GroupID: group, MemberID: member, Scope: scope,
		NotBefore: now - 1000, NotAfter: now + 120_000, Nonce: []byte(member + "-nonce"),
	}
	return signedToken(t, priv, tok)
}

// presentCap connects h to the relay and presents tok over CapProtocolID.
// Returns the relay's status byte (0 = accepted).
func presentCap(t *testing.T, ctx context.Context, h host.Host, ai peer.AddrInfo, tok *contract.CapToken) byte {
	t.Helper()
	if err := h.Connect(ctx, ai); err != nil {
		t.Fatalf("connect relay: %v", err)
	}
	s, err := h.NewStream(ctx, ai.ID, CapProtocolID)
	if err != nil {
		t.Fatalf("open cap stream: %v", err)
	}
	defer func() { _ = s.Close() }()
	raw, _ := json.Marshal(tok)
	if _, err := s.Write(raw); err != nil {
		t.Fatalf("write cap: %v", err)
	}
	_ = s.CloseWrite()
	buf := make([]byte, 1)
	if _, err := io.ReadFull(s, buf); err != nil {
		t.Fatalf("read cap status: %v", err)
	}
	return buf[0]
}

func TestRelayStartsCoordIndependent(t *testing.T) {
	_, pub := mustGroupKey(t)
	r := makeRelay(t, 4, 8, pub, true)
	if r.ID() == "" {
		t.Fatal("relay has no peer ID")
	}
	// server.Config above has a zero-value Coord (Enable=false): the relay
	// constructed and runs without any coord configuration.
}

func TestNoPSKCannotConnect(t *testing.T) {
	_, pub := mustGroupKey(t)
	r := makeRelay(t, 4, 8, pub, false)

	noPSK := makeClient(t, nil) // no private network key
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := noPSK.Connect(ctx, relayInfo(r)); err == nil {
		t.Fatal("peer without pnet PSK must not connect to the relay")
	}
}

func TestReservationRequiresCapToken(t *testing.T) {
	priv, pub := mustGroupKey(t)
	r := makeRelay(t, 4, 8, pub, false)
	ai := relayInfo(r)

	c := makeClient(t, testPSK(t))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// No cap presented → reservation refused by the ACL.
	if err := c.Connect(ctx, ai); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := circuitclient.Reserve(ctx, c, ai); err == nil {
		t.Fatal("reservation without CapToken must be refused")
	}

	// Untrusted signer → cap rejected, reservation still refused.
	other, _ := btcec.NewPrivateKey()
	bad := capTokenFor(t, other, "g", "m", contract.ScopeRelayReserve)
	if st := presentCap(t, ctx, c, ai, bad); st == 0 {
		t.Fatal("untrusted CapToken must be rejected by cap service")
	}
	if _, err := circuitclient.Reserve(ctx, c, ai); err == nil {
		t.Fatal("reservation with invalid CapToken must be refused")
	}

	// Valid relay-reserve cap → reservation succeeds.
	good := capTokenFor(t, priv, "g", "m", contract.ScopeRelayReserve)
	if st := presentCap(t, ctx, c, ai, good); st != 0 {
		t.Fatalf("valid CapToken rejected (status %d)", st)
	}
	if _, err := circuitclient.Reserve(ctx, c, ai); err != nil {
		t.Fatalf("reservation with valid CapToken must succeed: %v", err)
	}
}

func TestReservationQuotaExceeded(t *testing.T) {
	priv, pub := mustGroupKey(t)
	r := makeRelay(t, 4, 1, pub, false) // per-group = 1
	ai := relayInfo(r)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	c1 := makeClient(t, testPSK(t))
	if st := presentCap(t, ctx, c1, ai, capTokenFor(t, priv, "g", "m1", contract.ScopeRelayReserve)); st != 0 {
		t.Fatal("c1 cap rejected")
	}
	if _, err := circuitclient.Reserve(ctx, c1, ai); err != nil {
		t.Fatalf("c1 reserve: %v", err)
	}

	c2 := makeClient(t, testPSK(t))
	if st := presentCap(t, ctx, c2, ai, capTokenFor(t, priv, "g", "m2", contract.ScopeRelayReserve)); st != 0 {
		t.Fatal("c2 cap rejected")
	}
	if _, err := circuitclient.Reserve(ctx, c2, ai); err == nil {
		t.Fatal("c2 reservation must hit per-group quota")
	}
}

func TestRendezvousAccessControl(t *testing.T) {
	priv, pub := mustGroupKey(t)
	r := makeRelay(t, 4, 8, pub, true)
	ai := relayInfo(r)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	ns := "base32-group-namespace"

	// No grant → register unauthorized.
	c0 := makeClient(t, testPSK(t))
	if err := c0.Connect(ctx, ai); err != nil {
		t.Fatal(err)
	}
	if resp := rendezvousRPC(t, ctx, c0, ai.ID, rendezvousRequest{Op: opRegister, Namespace: ns, Addrs: []string{"/ip4/1.2.3.4/tcp/1"}}); resp.OK {
		t.Fatal("register without rendezvous-register grant must fail")
	}

	// Grant rendezvous-register → register ok.
	c1 := makeClient(t, testPSK(t))
	if st := presentCap(t, ctx, c1, ai, capTokenFor(t, priv, "g", "m1", contract.ScopeRendezvousRegister)); st != 0 {
		t.Fatal("c1 rendezvous cap rejected")
	}
	a1 := c1.Addrs()[0].String()
	if resp := rendezvousRPC(t, ctx, c1, ai.ID, rendezvousRequest{Op: opRegister, Namespace: ns, Addrs: []string{a1}}); !resp.OK {
		t.Fatalf("authorized register failed: %s", resp.Error)
	}

	// c2 (relay-reserve grant gives discover access) finds c1.
	c2 := makeClient(t, testPSK(t))
	if st := presentCap(t, ctx, c2, ai, capTokenFor(t, priv, "g", "m2", contract.ScopeRelayReserve)); st != 0 {
		t.Fatal("c2 cap rejected")
	}
	resp := rendezvousRPC(t, ctx, c2, ai.ID, rendezvousRequest{Op: opDiscover, Namespace: ns})
	if !resp.OK || len(resp.Peers) != 1 || resp.Peers[0].PeerID != c1.ID().String() {
		t.Fatalf("discover did not return c1: ok=%v peers=%+v", resp.OK, resp.Peers)
	}
}

func rendezvousRPC(t *testing.T, ctx context.Context, h host.Host, relayID peer.ID, req rendezvousRequest) rendezvousResponse {
	t.Helper()
	s, err := h.NewStream(ctx, relayID, RendezvousProtocolID)
	if err != nil {
		t.Fatalf("rendezvous stream: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := json.NewEncoder(s).Encode(req); err != nil {
		t.Fatalf("encode req: %v", err)
	}
	_ = s.CloseWrite()
	var resp rendezvousResponse
	if err := json.NewDecoder(s).Decode(&resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	return resp
}

// teeProxy is a transparent TCP proxy in front of the relay that records every
// byte in both directions, so a test can assert the relay only ever moves
// ciphertext (server.md R7 / protocol.md §8: "a relay packet capture sees only ciphertext").
type teeProxy struct {
	ln  net.Listener
	mu  sync.Mutex
	buf []byte
}

func startTeeProxy(t *testing.T, backend string) *teeProxy {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &teeProxy{ln: ln}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go p.handle(c, backend)
		}
	}()
	return p
}

func (p *teeProxy) handle(client net.Conn, backend string) {
	srv, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", backend)
	if err != nil {
		_ = client.Close()
		return
	}
	go p.pipe(client, srv)
	p.pipe(srv, client)
}

func (p *teeProxy) pipe(dst, src net.Conn) {
	b := make([]byte, 32<<10)
	for {
		n, err := src.Read(b)
		if n > 0 {
			p.mu.Lock()
			p.buf = append(p.buf, b[:n]...)
			p.mu.Unlock()
			if _, werr := dst.Write(b[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			_ = dst.Close()
			_ = src.Close()
			return
		}
	}
}

func (p *teeProxy) contains(needle []byte) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.buf) > 0 && strings.Contains(string(p.buf), string(needle))
}

func (p *teeProxy) size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.buf)
}

// proxyAddr returns the relay's peer AddrInfo rewritten to point at the tee
// proxy, so all client↔relay traffic is captured.
func proxyAddr(t *testing.T, r *Relay, p *teeProxy) peer.AddrInfo {
	t.Helper()
	_, port, _ := net.SplitHostPort(p.ln.Addr().String())
	maddr, err := ma.NewMultiaddr("/ip4/127.0.0.1/tcp/" + port)
	if err != nil {
		t.Fatal(err)
	}
	return peer.AddrInfo{ID: r.host.ID(), Addrs: []ma.Multiaddr{maddr}}
}

func relayBackendTCP(t *testing.T, r *Relay) string {
	t.Helper()
	for _, a := range r.host.Addrs() {
		if v, err := a.ValueForProtocol(ma.P_TCP); err == nil {
			ip, _ := a.ValueForProtocol(ma.P_IP4)
			return net.JoinHostPort(ip, v)
		}
	}
	t.Fatal("relay has no tcp addr")
	return ""
}

func TestRelayForwardsOnlyCiphertext(t *testing.T) {
	priv, pub := mustGroupKey(t)
	r := makeRelay(t, 4, 8, pub, false)
	proxy := startTeeProxy(t, relayBackendTCP(t, r))
	relayAI := proxyAddr(t, r, proxy)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	sentinel := []byte("SENTINEL-PLAINTEXT-MUST-NEVER-APPEAR-ON-RELAY-WIRE")

	dst := makeClient(t, testPSK(t)) // reachable via relay
	got := make(chan []byte, 1)
	dst.SetStreamHandler("/sentinel/1.0.0", func(s network.Stream) {
		defer func() { _ = s.Close() }()
		b, _ := io.ReadAll(io.LimitReader(s, 256))
		got <- b
	})

	// dst presents cap (via proxy) and reserves a relay slot.
	if st := presentCap(t, ctx, dst, relayAI, capTokenFor(t, priv, "g", "dst", contract.ScopeRelayReserve)); st != 0 {
		t.Fatal("dst cap rejected")
	}
	if _, err := circuitclient.Reserve(ctx, dst, relayAI); err != nil {
		t.Fatalf("dst reserve: %v", err)
	}

	// src presents cap (via proxy), then dials dst through the circuit.
	src := makeClient(t, testPSK(t))
	if st := presentCap(t, ctx, src, relayAI, capTokenFor(t, priv, "g", "src", contract.ScopeRelayReserve)); st != 0 {
		t.Fatal("src cap rejected")
	}
	circuitAddr, err := ma.NewMultiaddr(relayAI.Addrs[0].String() + "/p2p/" + relayAI.ID.String() + "/p2p-circuit")
	if err != nil {
		t.Fatal(err)
	}
	cctx, ccancel := context.WithTimeout(ctx, 10*time.Second)
	defer ccancel()
	if err := src.Connect(cctx, peer.AddrInfo{ID: dst.ID(), Addrs: []ma.Multiaddr{circuitAddr}}); err != nil {
		t.Fatalf("src connect dst via circuit: %v", err)
	}
	// The path must be the relayed circuit (not a direct fallback), else the
	// ciphertext assertion below would be meaningless.
	conns := src.Network().ConnsToPeer(dst.ID())
	if len(conns) == 0 || !strings.Contains(conns[0].RemoteMultiaddr().String(), "p2p-circuit") {
		t.Fatalf("src->dst is not via the relay circuit: %v", conns)
	}
	sctx, scancel := context.WithTimeout(ctx, 8*time.Second)
	defer scancel()
	s, err := src.NewStream(network.WithAllowLimitedConn(sctx, "test"), dst.ID(), "/sentinel/1.0.0")
	if err != nil {
		t.Fatalf("open sentinel stream: %v", err)
	}
	if _, err := s.Write(sentinel); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	_ = s.CloseWrite()

	select {
	case b := <-got:
		if string(b) != string(sentinel) {
			t.Fatalf("dst got %q, want sentinel", b)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("dst never received sentinel through relay")
	}

	if proxy.size() == 0 {
		t.Fatal("tee proxy captured no relay traffic")
	}
	if proxy.contains(sentinel) {
		t.Fatal("SENTINEL PLAINTEXT APPEARED ON RELAY WIRE — Noise not end-to-end / relay terminates encryption")
	}
}
