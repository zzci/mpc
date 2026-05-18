package contract

import "errors"

// ErrInvalidEnvelope is returned when a SigningRequest cannot be reduced to a
// canonical preimage: a field violates the fixed-length contract (digest32 or
// metaHash not exactly 32 bytes), an integer is out of range, or a required
// field is malformed. Per docs/design/contract/protocol.md:25 the verifier rejects
// ("任一不过即拒签") on any such failure; callers map this to the API's
// 400 INVALID_ENVELOPE (docs/design/contract/api.md).
var ErrInvalidEnvelope = errors.New("contract: invalid envelope")

// ErrSessionMismatch is returned when an inbound MpcMessage carries a
// sessionId different from the expected session. Per
// docs/design/contract/protocol.md:53 such messages are dropped unconditionally
// to defend against replay / cross-session ("串话") injection.
var ErrSessionMismatch = errors.New("contract: session id mismatch")

// ErrUnsupportedVersion is returned when an envelope or MPC message declares a
// version this build does not implement. Per docs/design/contract/protocol.md:86
// the receiver rejects rather than guessing a downgrade ("不识别即拒签").
var ErrUnsupportedVersion = errors.New("contract: unsupported version")

// ErrNoCommonProtocol is returned when libp2p multi-protocol negotiation finds
// no shared /tss/mpc/x.y.z version between the two peers
// (docs/design/contract/protocol.md:86).
var ErrNoCommonProtocol = errors.New("contract: no common protocol version")

// ErrBadSignature is returned when a proposerSig or senderAuth signature does
// not verify against its canonical digest and the claimed public key.
var ErrBadSignature = errors.New("contract: signature verification failed")
