// Typed error hierarchy (pma-bun: never throw raw strings). These mirror the
// rejection semantics of internal/contract/errors.go so the test double fails
// for the same reasons coord/device would.

export class ContractError extends Error {
  override readonly name: string = 'ContractError'
}

/**
 * Raised when a SigningRequest cannot be reduced to a canonical preimage:
 * a fixed-length field is wrong, an integer is out of range, or requestId is
 * not a UUID. Mirrors contract.ErrInvalidEnvelope (protocol.md:25 "任一不过即拒签").
 */
export class InvalidEnvelopeError extends ContractError {
  override readonly name = 'InvalidEnvelopeError'
}

/** Raised when a proposerSig / RSV signature does not verify. */
export class BadSignatureError extends ContractError {
  override readonly name = 'BadSignatureError'
}
