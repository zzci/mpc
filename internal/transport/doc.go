// Package transport implements the libp2p data-plane client of
// docs/design/contract/protocol.md §2/§3: a host secured end-to-end with Noise
// (peerID == public key), yamux multiplexing, a pnet PSK private network, a
// circuit-relay v2 client, optional rendezvous discovery over the
// base32(HMAC(groupSecret,"tss-group")) namespace, and GossipSub broadcast;
// directed messages travel over per-protocol P2P streams.
//
// It carries contract.MpcMessage (no MPC payload is produced here — the
// payload is tss-lib WireBytes from internal/mpc). All session isolation,
// senderAuth and version handling reuse internal/contract; this package adds
// no cryptography of its own and uses only the standard libp2p stack
// (docs/design/contract/protocol.md §2/§3, docs/design/mcp/sdk.md §1).
//
// Authoritative baseline (read-only): docs/design/contract/protocol.md §2/§3,
// docs/design/mcp/sdk.md §1.
package transport
