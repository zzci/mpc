// Package relay implements the node relay role: circuit-relay v2 (HOP) +
// rendezvous + access control (pnet PSK + capability token + quotas),
// stateless, ciphertext-only relaying, no dependency on coord.
//
// Zero-trust is a constructive guarantee: member Noise sessions are
// end-to-end and not terminated at the relay; the relay only forwards
// ciphertext, so it can neither read MPC content nor forge a sender
// (server.md R2). The relay is stateless, holds no shares, has no DB,
// and does not depend on coord (server.md R5): this package never imports
// internal/server/coord, and relay startup needs no coord config.
//
// Layered access control (server.md R4): the pnet PSK is enforced by
// libp2p itself (no swarm key = cannot speak the protocol); the CapToken
// is presented over CapProtocolID after the (pnet+Noise) secure
// connection is established, then enforced at the circuit-relay ACL
// (relay-reserve) and the rendezvous handler (rendezvous-register) —
// ConnectionGater cannot see an application-layer token during
// connection establishment, so the enforcement point is the ACL/handler,
// not the gater, realizing the R4 intent within the libp2p API; quotas
// are per-token/per-group reservation caps + a circuitv2 per-circuit
// Data/Duration cap.
//
// Authoritative baseline (read-only): docs/design/server/server.md
// "Part 1: relay role", docs/design/contract/protocol.md §6.
// Implemented by N-002.
package relay
