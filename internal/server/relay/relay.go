package relay

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	yamux "github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	circuitv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	noise "github.com/libp2p/go-libp2p/p2p/security/noise"

	"github.com/royqta/mcp-wallet/internal/node"
)

// Relay is a running relay-role instance.
type Relay struct {
	host  host.Host
	relay *circuitv2.Relay
	log   *slog.Logger
}

// New builds the relay host and wires circuit-relay v2, the cap-grant service
// and the rendezvous service from a node.Config. The config must have
// relay.enable=true and have passed node.Config.Validate (secret references
// resolved). No coord field is read: the relay is coord-independent.
//
// Access control layering (server.md R4):
//   - layer 1 pnet PSK: enforced by libp2p itself — a peer without the swarm
//     key cannot speak the protocol, so it never reaches the gater/ACL.
//   - layer 2 CapToken: presented over CapProtocolID after the (pnet+Noise)
//     secured connection is up, then enforced at the circuit-relay ACL
//     (relay-reserve) and the rendezvous handler (rendezvous-register). A
//     libp2p ConnectionGater cannot see an application token at connection
//     time, so token enforcement is intentionally at the ACL/handler, not the
//     gater; this realizes server.md R4's intent within the libp2p API.
//   - layer 3 quota: per-token/per-group reservation caps in the ACL plus
//     circuitv2 per-circuit Data/Duration limits.
func New(cfg node.Config, log *slog.Logger) (*Relay, error) {
	if !cfg.Relay.Enable {
		return nil, errors.New("relay: relay.enable is false")
	}
	if log == nil {
		log = slog.Default()
	}
	rc := cfg.Relay
	if len(rc.Listen) == 0 {
		return nil, errors.New("relay: relay.listen is empty (a relay with no listen address is undialable)")
	}

	psk, err := resolvePSK(rc.PnetPSKRef)
	if err != nil {
		return nil, fmt.Errorf("relay: pnet psk: %w", err)
	}
	anchors, err := newTrustAnchors(rc.TokenVerify.Source, rc.TokenVerify.GroupPubkeys)
	if err != nil {
		return nil, err
	}
	bwPerConn, err := parseBytesPerSec(rc.Limits.BandwidthPerConn)
	if err != nil {
		return nil, err
	}
	circuitDur, err := parseCircuitDuration(rc.Limits.CircuitMaxDuration)
	if err != nil {
		return nil, err
	}

	// Ephemeral relay identity: N-001 carries no identity field and the relay
	// is stateless (server.md R5); clients hold a configurable relay list and
	// fail over. Stable identity persistence would need a node.Config identity
	// field, owned outside this package, so it is left to that owner (H-003
	// scope is internal/server/relay only).
	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("relay: generate identity: %w", err)
	}

	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.PrivateNetwork(psk),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Muxer(yamux.ID, yamux.DefaultTransport),
		libp2p.ListenAddrStrings(rc.Listen...),
	)
	if err != nil {
		return nil, fmt.Errorf("relay: build libp2p host: %w", err)
	}

	az := newAuthz(rc.Limits.ReservationPerToken, rc.Limits.ReservationPerGroup)
	(&capService{anchors: anchors, az: az, log: log}).register(h)
	rv := newRendezvousService(az, log)
	if rc.Rendezvous.Enable {
		rv.register(h)
	}

	res := circuitv2.DefaultResources()
	if bwPerConn > 0 || circuitDur > 0 {
		lim := circuitv2.DefaultLimit()
		// Relax the per-circuit reset window first (keygen-aware, security.md
		// #10): a proof-enabled networked keygen/reshare is minutes-long and
		// the libp2p default (2m) would reset the circuit mid-keygen.
		if circuitDur > 0 {
			lim.Duration = circuitDur
		}
		// Data budget = rate × window, so a longer keygen window scales the
		// byte cap proportionally and preserves the anti-DoS rate bound.
		if bwPerConn > 0 {
			lim.Data = bwPerConn * int64(lim.Duration.Seconds())
		}
		res.Limit = lim
	}
	acl := &relayACL{az: az, log: log}
	cr, err := circuitv2.New(h, circuitv2.WithACL(acl), circuitv2.WithResources(res))
	if err != nil {
		_ = h.Close()
		return nil, fmt.Errorf("relay: enable circuit-relay v2: %w", err)
	}

	// Release a peer's grant/quota and rendezvous registrations once it is
	// fully disconnected, so caps never leak (authz.go quota model).
	h.Network().Notify(&network.NotifyBundle{
		DisconnectedF: func(n network.Network, c network.Conn) {
			p := c.RemotePeer()
			if n.Connectedness(p) == network.Connected {
				return
			}
			az.release(p)
			rv.drop(p)
		},
	})

	log.Info("relay: started",
		slog.String("peer", h.ID().String()),
		slog.Any("addrs", h.Addrs()),
		slog.Bool("rendezvous", rc.Rendezvous.Enable))
	return &Relay{host: h, relay: cr, log: log}, nil
}

// ID is the relay's libp2p peer ID.
func (r *Relay) ID() peer.ID { return r.host.ID() }

// Run blocks until ctx is cancelled, then shuts the relay down. The relay is
// event-driven (libp2p handles connections in the background); Run only owns
// the lifecycle.
func (r *Relay) Run(ctx context.Context) error {
	<-ctx.Done()
	return r.Close()
}

// Close stops the circuit-relay service and the libp2p host.
func (r *Relay) Close() error {
	relayErr := r.relay.Close()
	hostErr := r.host.Close()
	r.log.Info("relay: stopped")
	if relayErr != nil {
		return fmt.Errorf("relay: close circuit-relay: %w", relayErr)
	}
	if hostErr != nil {
		return fmt.Errorf("relay: close host: %w", hostErr)
	}
	return nil
}
