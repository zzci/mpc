package mpcnet

import (
	"context"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zzci/mpc/internal/contract"
	"github.com/zzci/mpc/internal/transport"
)

// Transport is the seam the engine consumes for one MPC session. A directed
// send is delivered as a /tss/mpc stream to the addressed peer; inbound
// messages drain through Inbound() after the host has already enforced the
// receive-side contract gate (version + sessionId + senderAuth) — failures
// drop the message silently before it reaches the engine
// (docs/design/contract/protocol.md §2.bis,86).
//
// Every MPC message — broadcast included — is delivered as a directed relay-
// circuit send. Routing every round through a directed circuit stream keeps
// the "all traffic via the relay" invariant (zero-trust relay, protocol.md §2)
// true for broadcast rounds too, matching what the cli E2E carrier already
// exercises in internal/cli/mpcnet.go.
//
// Reshare wire-from contract: pumpReshare prefixes the sender's MpcMessage.From
// with the committee marker ('O' = old, 'N' = new) before sending; the host
// PeerResolver MUST therefore strip the marker before looking up the member's
// secp256k1 pubkey, otherwise reshare messages will be silently rejected by
// the transport's senderAuth gate. internal/cli/device.go applies this
// convention; the smoke test in this package mirrors it.
type Transport interface {
	SendTo(ctx context.Context, to peer.ID, msg *contract.MpcMessage) error
	Inbound() <-chan transport.Inbound
}

// PeerTable maps a device's party tag (PartyID.Id == "1".."n", the 1-based
// decimal index from internal/cli/ids.go / internal/mpc/singleparty.go) to its
// libp2p peer, so a directed tss message is delivered to the right device.
// The caller resolves the table once per session (typically right after the
// rendezvous discovery used by the cli carrier) and passes it verbatim to
// every RunKeygen / RunSign / RunReshare invocation.
type PeerTable map[string]peer.ID

// sessionAdapter wraps a *transport.Session to satisfy Transport. It is the
// canonical adapter the cli and PC-host code paths use; DM-5 plugs a different
// Transport implementation for the mobile bridge without touching the engine.
type sessionAdapter struct{ s *transport.Session }

// FromSession adapts a *transport.Session into the engine's Transport seam.
// The session must already have been started (transport.JoinSession returned
// successfully): the engine never observes the session lifecycle.
func FromSession(s *transport.Session) Transport { return sessionAdapter{s: s} }

func (a sessionAdapter) SendTo(ctx context.Context, to peer.ID, msg *contract.MpcMessage) error {
	return a.s.SendTo(ctx, to, msg)
}

func (a sessionAdapter) Inbound() <-chan transport.Inbound { return a.s.Inbound() }
