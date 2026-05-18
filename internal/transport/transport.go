package transport

import (
	"context"
	"errors"
	"fmt"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/pnet"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	relayclient "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	noise "github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	ma "github.com/multiformats/go-multiaddr"
)

// Config is the immutable construction input for a Transport. Only HostKey
// and PSK are mandatory; relays and discovery are optional capabilities of
// docs/design/contract/protocol.md §2.
type Config struct {
	// HostKey is the libp2p identity. Per docs/design/contract/protocol.md:32 the
	// peerID Noise authenticates IS this public key.
	HostKey crypto.PrivKey
	// PSK is the pnet private-network key (docs/design/contract/protocol.md:34):
	// without it a peer cannot even speak the protocol. Required.
	PSK pnet.PSK
	// ListenAddrs are libp2p listen multiaddrs. Empty means an ephemeral
	// loopback TCP address (sufficient for same-host / relayed paths).
	ListenAddrs []string
	// Relays are circuit-relay v2 relays to reserve a slot at so other peers
	// can reach this host when a direct dial fails
	// (docs/design/contract/protocol.md:36).
	Relays []peer.AddrInfo
	// Discovery is the rendezvous backend. When set, Transport advertises and
	// resolves peers under RendezvousNamespace(GroupSecret)
	// (docs/design/contract/protocol.md:35).
	Discovery discovery.Discovery
	// GroupSecret keys the rendezvous namespace; required iff Discovery is set.
	GroupSecret []byte
}

// Transport is the libp2p data-plane client. It owns the host and a single
// GossipSub instance; per-session topics and inbound filtering live on the
// Session returned by JoinSession.
type Transport struct {
	host      host.Host
	ps        *pubsub.PubSub
	discovery discovery.Discovery
	namespace string
	relays    []peer.AddrInfo
}

// New builds the libp2p host with exactly the docs/design/contract/protocol.md §2
// stack: Noise only (no TLS) so the authenticated peer identity is the public
// key, yamux multiplexing, a pnet PSK private network, and TCP transport (the
// pnet-clean choice; QUIC is intentionally not enabled). The circuit-relay v2
// client transport is enabled so relayed dials work.
func New(ctx context.Context, cfg Config) (*Transport, error) {
	if cfg.HostKey == nil {
		return nil, errors.New("transport: HostKey is required")
	}
	if len(cfg.PSK) == 0 {
		return nil, errors.New("transport: PSK is required")
	}
	if cfg.Discovery != nil && len(cfg.GroupSecret) == 0 {
		return nil, errors.New("transport: GroupSecret is required when Discovery is set")
	}

	listen := cfg.ListenAddrs
	if len(listen) == 0 {
		listen = []string{"/ip4/127.0.0.1/tcp/0"}
	}

	opts := []libp2p.Option{
		libp2p.Identity(cfg.HostKey),
		libp2p.PrivateNetwork(cfg.PSK),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Muxer(yamux.ID, yamux.DefaultTransport),
		libp2p.NoTransports,
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.ListenAddrStrings(listen...),
		libp2p.EnableRelay(),
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("transport: build libp2p host: %w", err)
	}

	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		_ = h.Close()
		return nil, fmt.Errorf("transport: start gossipsub: %w", err)
	}

	t := &Transport{host: h, ps: ps, discovery: cfg.Discovery, relays: cfg.Relays}
	if cfg.Discovery != nil {
		t.namespace = RendezvousNamespace(cfg.GroupSecret)
	}
	return t, nil
}

// Host exposes the underlying libp2p host (addresses, peerstore, lifecycle).
func (t *Transport) Host() host.Host { return t.host }

// ID is this transport's peerID (== HostKey public key).
func (t *Transport) ID() peer.ID { return t.host.ID() }

// Addrs are the host's current listen multiaddrs.
func (t *Transport) Addrs() []ma.Multiaddr { return t.host.Addrs() }

// Connect dials a peer directly, recording its addresses.
func (t *Transport) Connect(ctx context.Context, ai peer.AddrInfo) error {
	if err := t.host.Connect(ctx, ai); err != nil {
		return fmt.Errorf("transport: connect %s: %w", ai.ID, err)
	}
	return nil
}

// ReserveRelays reserves a slot at every configured relay so peers can reach
// this host through circuit-relay v2 when a direct dial fails
// (docs/design/contract/protocol.md:36). Relay addresses are added to the
// peerstore first so the reservation dial can find them.
func (t *Transport) ReserveRelays(ctx context.Context) error {
	for _, r := range t.Relays() {
		t.host.Peerstore().AddAddrs(r.ID, r.Addrs, peerstore.PermanentAddrTTL)
		if _, err := relayclient.Reserve(ctx, t.host, r); err != nil {
			return fmt.Errorf("transport: reserve relay %s: %w", r.ID, err)
		}
	}
	return nil
}

// Relays is the configured circuit-relay v2 relay set.
func (t *Transport) Relays() []peer.AddrInfo { return t.relays }

// ConnectVia dials target through relay using a /p2p-circuit address
// (docs/design/contract/protocol.md:36: relay only forwards the encrypted stream).
// The relay's own addresses must already be in the peerstore (ReserveRelays
// or an explicit AddAddrs).
func (t *Transport) ConnectVia(ctx context.Context, relay peer.ID, target peer.ID) error {
	circuit, err := ma.NewMultiaddr(fmt.Sprintf("/p2p/%s/p2p-circuit", relay))
	if err != nil {
		return fmt.Errorf("transport: build circuit addr: %w", err)
	}
	ai := peer.AddrInfo{ID: target, Addrs: []ma.Multiaddr{circuit}}
	if err := t.host.Connect(ctx, ai); err != nil {
		return fmt.Errorf("transport: connect %s via relay %s: %w", target, relay, err)
	}
	return nil
}

// Advertise registers this host under the rendezvous namespace
// (docs/design/contract/protocol.md:35). It is a no-op when Discovery is unset.
func (t *Transport) Advertise(ctx context.Context) error {
	if t.discovery == nil {
		return nil
	}
	util.Advertise(ctx, t.discovery, t.namespace)
	return nil
}

// FindPeers resolves group members registered under the rendezvous namespace
// (docs/design/contract/protocol.md:35). Returns an empty slice when Discovery is
// unset.
func (t *Transport) FindPeers(ctx context.Context) ([]peer.AddrInfo, error) {
	if t.discovery == nil {
		return nil, nil
	}
	peers, err := util.FindPeers(ctx, t.discovery, t.namespace)
	if err != nil {
		return nil, fmt.Errorf("transport: rendezvous find peers: %w", err)
	}
	return peers, nil
}

// Close shuts the host down; sessions must be closed first.
func (t *Transport) Close() error {
	if err := t.host.Close(); err != nil {
		return fmt.Errorf("transport: close host: %w", err)
	}
	return nil
}
