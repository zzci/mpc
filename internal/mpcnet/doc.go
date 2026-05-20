// Package mpcnet is the production network engine for the distributed MPC
// protocol (docs/design/mcp/distributed-mpc.md §6.2, distributed-mpc-impl.md §B
// DM-2). It drives one device's view of a real n-party keygen / signing /
// resharing session over a caller-supplied transport: the device holds only
// its own share_i, every other party runs an independent process, and tss
// WireBytes ride inside contract.MpcMessage so session isolation, senderAuth
// and protocol-version negotiation are enforced by the same C-001 + M-005 +
// protocol.md §2 path a production device uses.
//
// Relationship to the rest of the tree:
//   - internal/mpc (DM-1, ADDITIVE) exposes the single-party tss-lib parties
//     (NewKeygenParty / NewSignParty / NewReshareParty) — each manages one
//     party's protocol state with Start / Update / Done / Out().
//   - internal/transport is the libp2p Noise + pnet + circuit-relay v2 stack;
//     its *transport.Session is the canonical Transport implementation here.
//   - internal/cli/mpcnet.go is the legacy E2E carrier — DM-2 is a COPY +
//     GENERALIZE, not a move (distributed-mpc-impl.md §B DM-2 file ownership),
//     so the cli carrier keeps driving the existing E2E-001 / E2E-002 gates
//     unchanged.
//
// Wire layout is the same as cli/mpcnet.go so any future cross-implementation
// deployment stays compatible:
//   - keygen / signing: MpcMessage{From, To, IsBroadcast, Payload=tss.WireBytes}
//     where From / To carry the 1-based decimal device tag.
//   - resharing: From is committee-prefixed ("O"+tag for old, "N"+tag for new);
//     To is a one-char marker ("O" / "N" / "B") + tag, because
//     tss.ParseWireMessage strips committee-routing flags on the receive side.
package mpcnet
