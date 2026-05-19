package mobileapi

import (
	"errors"

	"github.com/zzci/mpc/internal/contract"
	"github.com/zzci/mpc/internal/txdecode"
)

// The flat API reports every failure as a stable {code,msg} pair
// (docs/design/mcp/sdk.md §5). code is a machine-stable token the RN host branches
// on; msg is human context. Security-class codes (envelope/sig/metaHash/
// digest/expiry/version) are hard rejections: the flow stops, never retries,
// never degrades, and never enters MPC (docs/design/mcp/sdk.md §3/§5).
const (
	// CodeBadConfig is a malformed configJSON / startJSON or an out-of-range
	// parameter — a caller contract violation, not a security event.
	CodeBadConfig = "BAD_CONFIG"
	// CodeUnsupportedVersion is an envelope/MPC version this build does not
	// implement (security: reject, never downgrade-guess).
	CodeUnsupportedVersion = "UNSUPPORTED_VERSION"
	// CodeInvalidEnvelope is a structurally invalid envelope or a
	// metaHash≠H(businessInfo) mismatch (security hard reject).
	CodeInvalidEnvelope = "INVALID_ENVELOPE"
	// CodeBadProposerSig is a proposerSig that does not verify against the
	// canonical envelope preimage (security hard reject).
	CodeBadProposerSig = "BAD_PROPOSER_SIG"
	// CodeDigestMismatch is the core double-binding failure: the recomputed
	// chain digest ≠ digest32 (security hard reject — reject rather than mis-sign).
	CodeDigestMismatch = "DIGEST_MISMATCH"
	// CodeDecodeRejected means unsignedTx could not be parsed into the facts
	// needed to recompute the digest (security hard reject).
	CodeDecodeRejected = "DECODE_REJECTED"
	// CodeUnsupportedChain means req.Chain maps to no decoder (cannot bind
	// the digest → security hard reject).
	CodeUnsupportedChain = "UNSUPPORTED_CHAIN"
	// CodeExpired means the envelope/dispatch deadline elapsed at one of the
	// mandatory re-checks (security hard reject).
	CodeExpired = "EXPIRED"
	// CodeRejected means the human reviewer rejected the request (WYSIWYS
	// decision, not an error condition but reported via OnError per §5).
	CodeRejected = "REJECTED"
	// CodeNoShares means this process holds no key share for the group, so an
	// in-process signing/reshare cannot proceed.
	CodeNoShares = "NO_SHARES"
	// CodeSessionMismatch is an inbound wire message dropped by sessionId /
	// version isolation (cross-talk / replay defense).
	CodeSessionMismatch = "SESSION_MISMATCH"
	// CodeInternal is an unclassified internal failure (keygen/sign/reshare
	// engine error, keystore I/O, serialization).
	CodeInternal = "INTERNAL"
)

// codeFor maps an internal sentinel error onto a stable flat-API code. The
// mapping is intentionally explicit: a security-class sentinel must never be
// silently widened into a generic/internal code, because the host branches on
// it to enforce the hard-reject contract (docs/design/mcp/sdk.md §5).
func codeFor(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, contract.ErrUnsupportedVersion):
		return CodeUnsupportedVersion
	case errors.Is(err, contract.ErrBadSignature):
		return CodeBadProposerSig
	case errors.Is(err, txdecode.ErrDigestMismatch):
		return CodeDigestMismatch
	case errors.Is(err, txdecode.ErrDecodeRejected):
		return CodeDecodeRejected
	case errors.Is(err, txdecode.ErrUnsupportedChain):
		return CodeUnsupportedChain
	case errors.Is(err, contract.ErrInvalidEnvelope),
		errors.Is(err, txdecode.ErrInvalidEnvelope):
		return CodeInvalidEnvelope
	case errors.Is(err, contract.ErrSessionMismatch):
		return CodeSessionMismatch
	default:
		return CodeInternal
	}
}
