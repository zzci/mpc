package transport

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/btcsuite/btcd/btcec/v2"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/zzci/mpc/internal/contract"
)

// PeerResolver maps an MpcMessage.From (a tss PartyID, protocol.md:50) to that
// member's secp256k1 public key bytes so the recipient can verify senderAuth
// (docs/design/contract/protocol.md:54). It returns an error for an unknown member;
// such messages are dropped.
type PeerResolver func(partyID string) ([]byte, error)

// SessionConfig parameterises one MPC session's transport. SessionID strongly
// isolates the session (docs/design/contract/protocol.md:53): inbound messages for
// any other session are dropped. MemberKey signs the outbound senderAuth;
// Resolve verifies the inbound one.
type SessionConfig struct {
	SessionID string
	MemberKey *btcec.PrivateKey
	Resolve   PeerResolver
}

// Inbound is a verified MpcMessage delivered to the application: it passed
// version, sessionId and senderAuth checks. From is the libp2p peer the bytes
// arrived on (Noise-authenticated), distinct from Msg.From (the tss PartyID).
type Inbound struct {
	Msg  *contract.MpcMessage
	From peer.ID
}

// Session is the per-MPC-session view of the transport. Directed messages go
// over a /tss/mpc/x.y.z stream to the target peer; broadcast messages go over
// the GossipSub topic named by SessionID (docs/design/contract/protocol.md:55).
type Session struct {
	t       *Transport
	cfg     SessionConfig
	topic   *pubsub.Topic
	sub     *pubsub.Subscription
	inbound chan Inbound

	cancel context.CancelFunc
	wg     sync.WaitGroup
	done   chan struct{}

	closeOnce sync.Once
}

// JoinSession sets the directed-stream handler for this session, joins its
// GossipSub broadcast topic, and starts the inbound pipeline. Every inbound
// message — stream or broadcast — passes through contract.AcceptInbound
// (version + sessionId isolation) and contract.VerifySenderAuth before
// reaching Inbound(); any failure drops the message silently per
// docs/design/contract/protocol.md:53,54,86.
//
// One active session per Transport: the directed handler is registered on the
// shared ProtocolMPCV1 ID, so a second concurrent JoinSession on the same
// Transport replaces the first session's handler and its Close removes it for
// both. This matches the single-pump device model (docs/design/mcp/sdk.md §1/§5);
// run sequential sessions, or one Transport per concurrent session.
func (t *Transport) JoinSession(ctx context.Context, cfg SessionConfig) (*Session, error) {
	if cfg.SessionID == "" {
		return nil, errors.New("transport: SessionID is required")
	}
	if cfg.MemberKey == nil {
		return nil, errors.New("transport: MemberKey is required")
	}
	if cfg.Resolve == nil {
		return nil, errors.New("transport: Resolve is required")
	}

	topic, err := t.ps.Join(cfg.SessionID)
	if err != nil {
		return nil, fmt.Errorf("transport: join topic %q: %w", cfg.SessionID, err)
	}
	sub, err := topic.Subscribe()
	if err != nil {
		_ = topic.Close()
		return nil, fmt.Errorf("transport: subscribe topic %q: %w", cfg.SessionID, err)
	}

	sctx, cancel := context.WithCancel(ctx)
	s := &Session{
		t:       t,
		cfg:     cfg,
		topic:   topic,
		sub:     sub,
		inbound: make(chan Inbound, 64),
		cancel:  cancel,
		done:    make(chan struct{}),
	}

	// Directed messages: one handler per build, version-negotiated by libp2p
	// multistream on ProtocolMPCV1 (docs/design/contract/protocol.md:41,86).
	t.host.SetStreamHandler(protocol.ID(contract.ProtocolMPCV1), s.handleStream)

	s.wg.Add(1)
	go s.broadcastLoop(sctx)

	return s, nil
}

// Inbound delivers verified, session-scoped messages.
func (s *Session) Inbound() <-chan Inbound { return s.inbound }

// SendTo signs senderAuth and sends msg directly to a peer over a
// version-negotiated /tss/mpc stream (docs/design/contract/protocol.md:55).
func (s *Session) SendTo(ctx context.Context, to peer.ID, msg *contract.MpcMessage) error {
	if err := s.prepare(msg); err != nil {
		return err
	}
	// circuit-relay v2 connections are "limited"; MPC streams are designed to
	// traverse the relay (docs/design/contract/protocol.md:36), so explicitly
	// permit a limited connection rather than waiting for a direct one.
	ctx = network.WithAllowLimitedConn(ctx, "tss-mpc")
	stream, err := s.t.host.NewStream(ctx, to, s.negotiate(to))
	if err != nil {
		return fmt.Errorf("transport: open stream to %s: %w", to, err)
	}
	defer func() { _ = stream.Close() }()
	if err := writeFrame(stream, msg); err != nil {
		_ = stream.Reset()
		return err
	}
	return nil
}

// Broadcast signs senderAuth and publishes msg on the session GossipSub topic
// (topic == SessionID, docs/design/contract/protocol.md:55).
func (s *Session) Broadcast(ctx context.Context, msg *contract.MpcMessage) error {
	if err := s.prepare(msg); err != nil {
		return err
	}
	b, err := marshalMessage(msg)
	if err != nil {
		return err
	}
	if err := s.topic.Publish(ctx, b); err != nil {
		return fmt.Errorf("transport: publish broadcast: %w", err)
	}
	return nil
}

// prepare stamps the session/version invariants and signs senderAuth so the
// member identity is bound to (sessionId,round,payload) above the tss-lib
// layer (docs/design/contract/protocol.md:54).
func (s *Session) prepare(msg *contract.MpcMessage) error {
	msg.Version = contract.MpcVersionV1
	msg.SessionID = s.cfg.SessionID
	if err := contract.SignSenderAuth(s.cfg.MemberKey, msg); err != nil {
		return fmt.Errorf("transport: sign senderAuth: %w", err)
	}
	return nil
}

// negotiate picks the highest common /tss/mpc protocol ID via
// contract.NegotiateMPCProtocol against the peer's advertised protocols
// (docs/design/contract/protocol.md:86). It is a best-effort pre-selection: the
// peerstore can lag identify, so when no /tss/mpc entry is known yet it falls
// back to ProtocolMPCV1 and lets libp2p multistream perform the authoritative
// negotiation — which itself rejects (never downgrade-guesses) a peer that
// does not speak this build's version.
func (s *Session) negotiate(to peer.ID) protocol.ID {
	supported, err := s.t.host.Peerstore().GetProtocols(to)
	if err != nil || len(supported) == 0 {
		return protocol.ID(contract.ProtocolMPCV1)
	}
	remote := make([]string, len(supported))
	for i, p := range supported {
		remote[i] = string(p)
	}
	chosen, err := contract.NegotiateMPCProtocol([]string{contract.ProtocolMPCV1}, remote)
	if err != nil {
		return protocol.ID(contract.ProtocolMPCV1)
	}
	return protocol.ID(chosen)
}

// handleStream reads one directed frame, gates it, and delivers it.
func (s *Session) handleStream(stream network.Stream) {
	defer func() { _ = stream.Close() }()
	msg, err := readFrame(stream)
	if err != nil {
		_ = stream.Reset()
		return
	}
	s.deliver(msg, stream.Conn().RemotePeer())
}

// broadcastLoop pumps the GossipSub subscription through the same gate.
func (s *Session) broadcastLoop(ctx context.Context) {
	defer s.wg.Done()
	for {
		pm, err := s.sub.Next(ctx)
		if err != nil {
			return // context cancelled or subscription closed
		}
		msg, err := unmarshalMessage(pm.GetData())
		if err != nil {
			continue
		}
		s.deliver(msg, pm.GetFrom())
	}
}

// deliver applies the receive-side contract gate (version + sessionId
// isolation, then senderAuth membership) and forwards survivors. Any failure
// drops the message (docs/design/contract/protocol.md:53,54,86).
func (s *Session) deliver(msg *contract.MpcMessage, from peer.ID) {
	if err := contract.AcceptInbound(msg, s.cfg.SessionID); err != nil {
		return
	}
	pub, err := s.cfg.Resolve(msg.From)
	if err != nil {
		return
	}
	if err := contract.VerifySenderAuth(msg, pub); err != nil {
		return
	}
	select {
	case s.inbound <- Inbound{Msg: msg, From: from}:
	case <-s.done:
	}
}

// Close removes the stream handler, cancels the broadcast loop, and releases
// the topic. It is idempotent.
func (s *Session) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.t.host.RemoveStreamHandler(protocol.ID(contract.ProtocolMPCV1))
		close(s.done)
		s.cancel()
		s.sub.Cancel()
		s.wg.Wait()
		if e := s.topic.Close(); e != nil {
			err = fmt.Errorf("transport: close topic: %w", e)
		}
	})
	return err
}
