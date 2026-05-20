// Package hd implements offline non-hardened HD derivation for the wallet's
// flat single-layer index space (docs/design/mcp/address-derivation.md §1 F2,
// §2). Given the master xpub (Q_master, chaincode) a holder computes any
// child public key and three-chain address single-machine, zero-MPC,
// zero-network — no private-key material is ever reconstructed (§6 invariant).
package hd
