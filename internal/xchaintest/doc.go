// Package xchaintest is the integration-test package for three-chain
// real-digest {R,S,V} cross-verification (test-only, exports no
// implementation). End-to-end it chains internal/txdecode (real
// unsignedTx decode + recomputed chain-digest ==digest32 assertion) →
// internal/mpc (in-process threshold signing producing {R,S,V}) →
// secp256k1 ecrecover / stdlib verify → internal/addr address derivation
// cross-consistency.
//
// Covers ETH (legacy/EIP-155), BSC (EIP-1559), TRON (native
// TransferContract); asserts low-S normalization (S<=N/2) and a correct
// recovery id V (the exact V recovers the group master pubkey, a flipped
// V does not). Authoritative baseline (read-only):
// docs/design/testing.md §3, docs/design/PLAN.md §2,
// docs/design/mcp/sdk.md §4. This package changes no
// mpc/txdecode/addr/contract implementation.
package xchaintest
