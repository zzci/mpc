package txdecode

import (
	"crypto/subtle"
	"fmt"

	"github.com/zzci/mpc/internal/contract"
)

// ChainDecoder is the pluggable per-chain decoder. An override may replace a
// built-in decoder, but the "recomputed digest == digest32" assertion is
// enforced by the framework (Decoder.Decode), not by the decoder, so every
// override is automatically bound by the same invariant
// (docs/design/mcp/sdk.md §4 「可插拔覆盖…须满足同一断言」).
//
// Recompute parses unsignedTx into A-zone Facts and returns the chain signing
// digest derived from the parsed facts (EVM: keccak256 of the re-encoded
// preimage — a decode bug changes it) or from the raw_data bytes (TRON:
// sha256, chain-inherent — see package doc). It returns an error only when the
// input cannot be bound at all (then the framework hard-rejects); recognized-
// but-unsupported shapes degrade inside Facts with a caution, never an error.
type ChainDecoder interface {
	Recompute(unsignedTx []byte) (*Facts, [32]byte, error)
}

// Decoder dispatches an envelope to a chain decoder, enforces the
// ==digest32 binding, and runs the A/B declarative cross-check. The zero
// value is not usable; construct with New.
type Decoder struct {
	builtin   map[Chain]ChainDecoder
	overrides map[Chain]ChainDecoder
}

// New returns a Decoder with the built-in ETH/BSC/TRON decoders.
func New() *Decoder {
	return &Decoder{
		builtin: map[Chain]ChainDecoder{
			ChainETH:  &evmDecoder{chain: ChainETH, expectChainID: chainIDEth},
			ChainBSC:  &evmDecoder{chain: ChainBSC, expectChainID: chainIDBsc},
			ChainTRON: &tronDecoder{},
		},
		overrides: map[Chain]ChainDecoder{},
	}
}

// Register installs an override decoder for a chain, replacing the built-in.
// The framework still asserts the override's recomputed digest == digest32.
//
// Not safe for concurrent use with Decode: configure all overrides once at
// startup, before the Decoder is shared across signing calls.
func (d *Decoder) Register(chain Chain, cd ChainDecoder) {
	d.overrides[chain] = cd
}

// Decode is the security gate. It parses req.UnsignedTx, recomputes the chain
// signing digest, and asserts it equals req.Digest32. On any binding failure
// it returns an error and NO Result, so unverified data can never reach the
// UI as the authoritative A-zone — the caller MUST treat any error as 拒签
// and not enter MPC (docs/design/mcp/sdk.md §3/§5, docs/design/contract/protocol.md:25).
//
// A successful Result carries the digest-bound A-zone Facts plus the A/B
// declarative discrepancies (which are loud human-review warnings, not
// rejections; only digest mismatch rejects).
func (d *Decoder) Decode(req *contract.SigningRequest) (*Result, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: nil request", ErrInvalidEnvelope)
	}
	if len(req.Digest32) != 32 {
		return nil, fmt.Errorf("%w: digest32 is %d bytes, want 32", ErrInvalidEnvelope, len(req.Digest32))
	}

	chain, ok := normalizeChain(req.Chain)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedChain, req.Chain)
	}
	cd := d.overrides[chain]
	if cd == nil {
		cd = d.builtin[chain]
	}
	if cd == nil {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedChain, req.Chain)
	}

	facts, recomputed, err := cd.Recompute(req.UnsignedTx)
	if err != nil {
		// Cannot bind => hard reject, no facts (docs/design/mcp/sdk.md §5).
		return nil, fmt.Errorf("%w: %w", ErrDecodeRejected, err)
	}
	if facts == nil {
		return nil, fmt.Errorf("%w: decoder returned no facts", ErrDecodeRejected)
	}

	// Core double-binding invariant. Constant-time to avoid leaking how far a
	// crafted digest matched.
	if subtle.ConstantTimeCompare(recomputed[:], req.Digest32) != 1 {
		return nil, ErrDigestMismatch
	}

	ms := crossCheckAB(facts, req.BusinessInfo)
	return &Result{Facts: facts, Mismatches: ms, DigestVerified: true}, nil
}
