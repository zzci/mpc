package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/pnet"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/zzci/mpc/internal/contract"
	"github.com/zzci/mpc/internal/transport"
)

// HostTransport is the PC-side libp2p host adapter that satisfies the SDK's
// WireCallbacks contract (distributed-mpc-impl.md §B DM-5). It owns a single
// transport.Transport + transport.Session, ferries the SDK's outbound MpcMessage
// envelopes over the relay-circuit data path, and feeds verified inbound
// envelopes back into the running single-party engine via SDK.OnWireMessage.
//
// One HostTransport drives one MPC session (transport.Session is single-session-
// per-Transport, transport/session.go:71). Callers build a new HostTransport
// per keygen / sign / reshare and Close it when the protocol finishes.
//
// Wire shape — identical on both directions — is the canonical JSON encoding
// of contract.MpcMessage that internal/mobileapi/wirecallbacks.go emits
// (DM-3): {version, sessionId, from, to[], isBroadcast, payload, senderAuth?}.
// SenderAuth is signed by transport.Session.prepare on send, verified by
// transport.Session.deliver on receive (R5 gate, protocol.md §54/§86); the SDK
// only re-runs the version + sessionId isolation half on top.
//
// Mobile-bridge parity (sdk.md §5): the same SDK + WireCallbacks shape that the
// PC CLI drives here is what the mobile native bridge implements over its own
// transport. The SDK never observes which host wired it.
type HostTransport struct {
	host      *transport.Transport
	session   *transport.Session
	sessionID string

	// peers maps an MpcMessage.To tag (1-based decimal device index, ids.go:
	// partyTag) to that device's libp2p peer. Resharing pumps prefix the tag
	// with 'O' / 'N' / 'B' so the routing layer strips the marker before
	// lookup, matching internal/cli/mpcnet.go's wire convention.
	peers PeerTable

	// sdkSink is the SDK handle inbound envelopes are fed into. Pump installs
	// it once and the inbound goroutine consults it under the host mutex;
	// nil sdkSink (Pump never called) drops inbound messages on the floor —
	// this matches the transport's posture that an unjoined receiver is benign.
	mu         sync.Mutex
	sdkSink    onWireFeeder
	pumpCancel context.CancelFunc
	pumpDone   chan struct{}
	closed     bool

	// lastErr is the most recent fire-and-forget error from OnWireMessage /
	// Pump (a single failed destination, an unparsable inbound, etc.); drained
	// by LastError. Stored under h.mu.
	lastErr error
}

// onWireFeeder is the local view of the receive seam HostTransport pumps into.
// It is satisfied by *sdk.SDK and *internal/mobileapi.SDK; declaring the
// dependency as a 1-method interface keeps internal/cli free of an import from
// the gomobile re-export package and lets the host_transport_test.go inject a
// recording fake without spinning up a real keystore.
type onWireFeeder interface {
	OnWireMessage(b []byte) error
}

// PeerTable is an alias of the engine's table (mpcnet.PeerTable shape), kept
// local so external callers (cmd/cli, tests) need only the host_transport API.
type PeerTable map[string]peer.ID

// HostTransportConfig parameterises one HostTransport instance. SessionID
// strongly isolates the session (R5 gate, sdk.md §3 / protocol.md §53); every
// outbound envelope the SDK emits must carry the same SessionID, otherwise the
// receiver's gate drops it.
type HostTransportConfig struct {
	// HostKey is this device's libp2p identity. Per protocol.md §32 the
	// peerID Noise authenticates IS this public key.
	HostKey crypto.PrivKey

	// PSK is the pnet private-network key (protocol.md §34). Required.
	PSK pnet.PSK

	// ListenAddrs are libp2p listen multiaddrs. Empty == ephemeral loopback,
	// sufficient for same-host fabrics (the production deployment supplies a
	// public-facing addr or relies on relay reservations).
	ListenAddrs []string

	// Relays are circuit-relay v2 relays this host reserves a slot at so peers
	// can reach it through the relay (protocol.md §36). Empty == no reservation;
	// the engine then assumes a direct-dial mesh.
	Relays []peer.AddrInfo

	// MemberKey is the secp256k1 identity that signs senderAuth on every
	// outbound envelope and authenticates this device to peers. transport.
	// Session re-uses the same convention.
	MemberKey *btcec.PrivateKey

	// SessionID is the single MPC session this HostTransport scopes (matches
	// the SDK configJSON sessionID under DM-3 hard-cut).
	SessionID string

	// Peers maps each OTHER device's 1-based decimal tag to its libp2p peer
	// (rendezvous output). The local device's own tag may be present or
	// absent — the routing layer skips self either way.
	Peers PeerTable

	// PeerPubKeys maps each device's bare 1-based tag to its secp256k1
	// compressed public key bytes, used by transport.Session's PeerResolver
	// to verify inbound senderAuth. The reshare 'O' / 'N' marker is stripped
	// before lookup to match internal/cli/mpcnet.go's wire convention.
	PeerPubKeys map[string][]byte
}

// NewHostTransport builds the libp2p host, reserves any relays the config
// names, joins the named session and returns the host adapter ready to be
// passed to SDK.KeyGen / Sign / Reshare as a WireCallbacks. Callers MUST
// Close() the returned host (typically deferred) after the protocol finishes;
// the inbound pump (Pump) is shut down by Close as well.
func NewHostTransport(ctx context.Context, cfg HostTransportConfig) (*HostTransport, error) {
	if cfg.HostKey == nil {
		return nil, errors.New("cli: HostTransport HostKey is required")
	}
	if len(cfg.PSK) == 0 {
		return nil, errors.New("cli: HostTransport PSK is required")
	}
	if cfg.MemberKey == nil {
		return nil, errors.New("cli: HostTransport MemberKey is required")
	}
	if cfg.SessionID == "" {
		return nil, errors.New("cli: HostTransport SessionID is required")
	}

	tr, err := transport.New(ctx, transport.Config{
		HostKey:     cfg.HostKey,
		PSK:         cfg.PSK,
		ListenAddrs: cfg.ListenAddrs,
		Relays:      cfg.Relays,
	})
	if err != nil {
		return nil, fmt.Errorf("cli: build transport: %w", err)
	}

	if len(cfg.Relays) > 0 {
		if rerr := tr.ReserveRelays(ctx); rerr != nil {
			_ = tr.Close()
			return nil, fmt.Errorf("cli: reserve relays: %w", rerr)
		}
	}

	resolve := func(tag string) ([]byte, error) {
		// Resharing prefixes From with the sender's committee ('O' / 'N'); the
		// member identity is per device, so strip it back to the bare tag
		// before consulting the member-pubkey table — the same convention
		// internal/cli/device.go and internal/mpcnet apply.
		if len(tag) >= 2 && (tag[0] == 'O' || tag[0] == 'N') {
			tag = tag[1:]
		}
		pb, ok := cfg.PeerPubKeys[tag]
		if !ok {
			return nil, fmt.Errorf("cli: unknown member %q", tag)
		}
		return pb, nil
	}

	sess, err := tr.JoinSession(ctx, transport.SessionConfig{
		SessionID: cfg.SessionID,
		MemberKey: cfg.MemberKey,
		Resolve:   resolve,
	})
	if err != nil {
		_ = tr.Close()
		return nil, fmt.Errorf("cli: join session %q: %w", cfg.SessionID, err)
	}

	peers := make(PeerTable, len(cfg.Peers))
	for tag, pid := range cfg.Peers {
		peers[tag] = pid
	}
	return &HostTransport{
		host:      tr,
		session:   sess,
		sessionID: cfg.SessionID,
		peers:     peers,
	}, nil
}

// SessionID returns the canonical session id this host transport is scoped to.
// Wallet / test callers stitch it into the SDK configJSON under DM-3 hard-cut
// so the receive-side R5 gate accepts inbound envelopes.
func (h *HostTransport) SessionID() string { return h.sessionID }

// PeerID returns this host's libp2p peer ID — the value other devices stitch
// into their HostTransportConfig.Peers entry for this device.
func (h *HostTransport) PeerID() peer.ID { return h.host.ID() }

// ListenAddrs returns the live listen multiaddrs of the underlying transport.
// Useful for fabrics that need to mesh-dial; the relay-circuit path requires
// only the relay's addrs, not the device's.
func (h *HostTransport) ListenAddrs() []ma.Multiaddr { return h.host.Addrs() }

// ConnectVia opens a circuit-relay v2 connection from this host to another
// device through relay. ReserveRelays must have already succeeded for the
// relay slot to exist; callers that built the HostTransport with cfg.Relays
// set get that for free via NewHostTransport.
func (h *HostTransport) ConnectVia(ctx context.Context, relay peer.ID, target peer.ID) error {
	return h.host.ConnectVia(ctx, relay, target)
}

// Connect opens a direct libp2p connection. Tests that exercise the in-process
// loopback fabric use this; production callers prefer ConnectVia for the
// zero-trust relay path (protocol.md §36).
func (h *HostTransport) Connect(ctx context.Context, ai peer.AddrInfo) error {
	return h.host.Connect(ctx, ai)
}

// OnWireMessage is the sdk.WireCallbacks bridge (Go→host) the SDK invokes once
// per emitted MpcMessage. It JSON-decodes the envelope, looks each To-tag's
// libp2p peer up in the HostTransport's peer table and forwards via
// transport.Session.SendTo, which signs senderAuth and ships the bytes over
// the relay-circuit / direct stream. Errors are non-fatal: a single failed
// destination must not bring the protocol down (the dispatch goroutine inside
// the SDK is fire-and-forget by gomobile contract), but they are recorded
// against the last-error slot so a caller draining errors can inspect them.
//
// Wire-from convention (matches internal/mobileapi/wirecallbacks.go and
// internal/cli/mpcnet.go): every emitted message — broadcast included — is
// delivered as one directed envelope per destination. Resharing payloads carry
// committee markers ('O' / 'N' / 'B') the receiver strips before resolving.
func (h *HostTransport) OnWireMessage(b []byte) {
	var msg contract.MpcMessage
	if err := json.Unmarshal(b, &msg); err != nil {
		h.recordErr(fmt.Errorf("decode outbound: %w", err))
		return
	}
	if msg.SessionID != "" && msg.SessionID != h.sessionID {
		h.recordErr(fmt.Errorf("session id drift: msg %q want %q", msg.SessionID, h.sessionID))
		return
	}
	if len(msg.To) == 0 {
		// Per emitOutbound's wire shape, every envelope is per-destination;
		// an empty To means the SDK fan-out path was bypassed. We drop
		// silently — the transport gate would reject it on receive too.
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, tag := range msg.To {
		bare := tag
		if len(bare) >= 2 && (bare[0] == 'O' || bare[0] == 'N' || bare[0] == 'B') {
			bare = bare[1:]
		}
		to, ok := h.peers[bare]
		if !ok {
			h.recordErr(fmt.Errorf("no peer for tag %q", tag))
			continue
		}
		// Forward as the same envelope shape the receiver expects. SenderAuth
		// is signed by transport.Session.prepare; the SDK never sets it on
		// emit (wirecallbacks.go), and overwriting it here would be wrong.
		out := &contract.MpcMessage{
			Version:     msg.Version,
			SessionID:   msg.SessionID,
			From:        msg.From,
			To:          []string{tag},
			IsBroadcast: msg.IsBroadcast,
			Round:       msg.Round,
			Payload:     msg.Payload,
		}
		if err := h.session.SendTo(ctx, to, out); err != nil {
			h.recordErr(fmt.Errorf("send to %q: %w", tag, err))
		}
	}
}

// Pump starts the inbound goroutine that drains transport.Session.Inbound()
// and feeds each verified envelope into the supplied SDK handle (sdk.SDK /
// internal/mobileapi.SDK both satisfy onWireFeeder via OnWireMessage([]byte)).
// Pump must be called BEFORE the SDK starts emitting outbound (= before
// sdk.KeyGen / Sign / Reshare returns), otherwise peer messages that arrive
// during start-up race the engine's installSession call and the receive-side
// SDK gate drops them as "no live session" (sdk.md §3 — R5 isolation).
//
// Pump is safe to call exactly once per HostTransport. A second call returns
// an error rather than spawning a duplicate goroutine; Close stops the pump
// idempotently.
func (h *HostTransport) Pump(parent context.Context, sdk onWireFeeder) error {
	if sdk == nil {
		return errors.New("cli: HostTransport Pump: sdk is nil")
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return errors.New("cli: HostTransport Pump: already closed")
	}
	if h.sdkSink != nil {
		h.mu.Unlock()
		return errors.New("cli: HostTransport Pump: already running")
	}
	h.sdkSink = sdk
	ctx, cancel := context.WithCancel(parent)
	h.pumpCancel = cancel
	h.pumpDone = make(chan struct{})
	in := h.session.Inbound()
	done := h.pumpDone
	h.mu.Unlock()

	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case inbound, ok := <-in:
				if !ok {
					return
				}
				bz, err := json.Marshal(inbound.Msg)
				if err != nil {
					h.recordErr(fmt.Errorf("marshal inbound: %w", err))
					continue
				}
				// Inbound has already cleared the senderAuth + sessionId + version
				// gate on the transport layer; the SDK re-runs the version +
				// sessionId half on top (sdk.OnWireMessage). A drop here is
				// benign (e.g. the SDK has not installed its wireSession yet
				// because Pump ran first); the error is informational only.
				if err := sdk.OnWireMessage(bz); err != nil {
					h.recordErr(fmt.Errorf("feed sdk: %w", err))
				}
			}
		}
	}()
	return nil
}

// Close stops the inbound pump, closes the session and releases the libp2p
// host. Idempotent.
func (h *HostTransport) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	cancel := h.pumpCancel
	done := h.pumpDone
	h.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}

	var firstErr error
	if err := h.session.Close(); err != nil {
		firstErr = fmt.Errorf("cli: close session: %w", err)
	}
	if err := h.host.Close(); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("cli: close transport: %w", err)
	}
	return firstErr
}

// LastError returns and clears the most recently recorded send / decode
// error. Tests use it to assert the protocol ran clean; production callers
// drain it after a protocol finishes for diagnostics. A nil return means no
// error has been recorded since the last call.
func (h *HostTransport) LastError() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	err := h.lastErr
	h.lastErr = nil
	return err
}

// recordErr stashes err under the host mutex; OnWireMessage / Pump record
// fire-and-forget errors here so the caller can drain them with LastError.
func (h *HostTransport) recordErr(err error) {
	if err == nil {
		return
	}
	h.mu.Lock()
	h.lastErr = err
	h.mu.Unlock()
}
