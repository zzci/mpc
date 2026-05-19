// Package txdecode provides read-only ETH/BSC/TRON transaction decoding,
// per-chain signing-digest recomputation with a ==digest32 assertion,
// verified A-zone fact output, A/B declarative cross-check and
// unrecognized-input degradation.
//
// Security-critical: the A-zone is the sole authoritative basis for fund
// safety; a decoder bug must not degrade into a mis-sign. Dual-binding
// invariant (docs/design/mcp/sdk.md §4, docs/design/PLAN.md §2/§3):
//
//   - EVM: parse unsignedTx → recompute Keccak256(RLP / 0x02‖typed-RLP)
//     from the PARSED structured fields → assert ==digest32. A parse
//     error makes the recomputed digest differ from digest32, hence
//     "reject rather than mis-sign" (strong dual binding).
//   - TRON: the chain signature is sha256(raw_data) itself and the
//     byte-exact protobuf cannot be unambiguously rebuilt from fields,
//     so recompute = sha256(unsignedTx) asserted ==digest32: the binding
//     is bytes↔digest32↔envelope (covered by proposerSig). protobuf
//     parsing is for A-zone display only; its correctness is backed by
//     real corpora + fuzzing and "do not fabricate the unrecognized"
//     (this asymmetry is chain-inherent — see docs/design/PLAN.md §5
//     risk 9 and §4 "TRON: sha256(raw_data)").
//
// Any security-class failure (invalid envelope / digest mismatch / parse
// cannot bind) is a hard reject: it returns an error and no displayable
// A-zone, and the caller must reject and not enter MPC
// (docs/design/mcp/sdk.md §5). A pluggable override (ChainDecoder) may
// replace the decoder, but the ==digest32 assertion is enforced by this
// package's framework, so an override is automatically under the same
// binding constraint.
//
// Scope boundary: decoding is in-scope; transaction construction /
// calldata encoding / broadcast are out-of-scope (external business
// service). Authoritative baseline (read-only): docs/design/mcp/sdk.md
// §4, docs/design/PLAN.md §2/§3, docs/design/contract/protocol.md
// (digest32 / envelope semantics), internal/contract.
package txdecode
