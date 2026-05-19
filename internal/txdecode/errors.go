package txdecode

import "errors"

// ErrInvalidEnvelope is returned when the SigningRequest itself cannot be used
// for decoding: nil request, or digest32 not exactly 32 bytes. Per
// docs/design/contract/protocol.md:25 the verifier rejects on any such failure.
var ErrInvalidEnvelope = errors.New("txdecode: invalid envelope")

// ErrUnsupportedChain is returned when req.Chain maps to no built-in decoder
// and no override is registered. The device must reject (cannot bind digest).
var ErrUnsupportedChain = errors.New("txdecode: unsupported chain")

// ErrDecodeRejected is returned when unsignedTx cannot be parsed into the
// structured facts required to recompute the chain digest (EVM: malformed
// RLP / unknown tx type). With no recomputable digest the envelope cannot be
// bound, so per docs/design/mcp/sdk.md §5 this is a hard rejection, never a
// best-effort display. Decode returns no facts in this case.
var ErrDecodeRejected = errors.New("txdecode: decode failed, signing rejected")

// ErrDigestMismatch is returned when the recomputed chain digest does not
// equal req.Digest32. This is the core double-binding outcome: a decode bug
// or a tampered envelope degrades to a hard rejection ("reject rather than mis-sign",
// docs/design/mcp/sdk.md §4). Decode returns no facts so callers cannot display
// unverified data as the authoritative A-zone.
var ErrDigestMismatch = errors.New("txdecode: recomputed digest != digest32, signing rejected")
