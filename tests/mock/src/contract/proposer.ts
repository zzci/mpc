import type { SigningRequest } from './types.ts'
import { secp256k1 } from '@noble/curves/secp256k1.js'
import { envelopeDigest } from './canonical.ts'
import { BadSignatureError } from './errors.ts'

// Port of internal/contract/proposer.go signing primitive
// (docs/spec/envelope-canonical.md §2.4): secp256k1 ECDSA over the 32-byte
// canonical digest, DER-serialized, low-S — identical to the project's
// btcec/v2 stack. @noble/curves v2 treats the input as already-hashed
// (prehash) by default and low-S normalizes by default, matching btcec.

// prehash:false — the input is already the 32-byte canonical digest; @noble v2
// would otherwise sha256 it again. lowS defaults true, matching btcec.
const DER = { format: 'der' as const, prehash: false }

/** DER secp256k1 ECDSA signature over a 32-byte digest (shared primitive). */
export function signDigest(priv: Uint8Array, digest: Uint8Array): Uint8Array {
  return secp256k1.sign(digest, priv, DER)
}

/**
 * Verify a DER secp256k1 ECDSA signature over a 32-byte digest against a
 * serialized public key (compressed or uncompressed). Throws BadSignatureError
 * on any parse or verification failure (contract.VerifyDigest semantics).
 */
export function verifyDigest(pub: Uint8Array, digest: Uint8Array, sig: Uint8Array): void {
  let ok: boolean
  try {
    ok = secp256k1.verify(sig, digest, pub, DER)
  }
  catch (cause) {
    throw new BadSignatureError('signature verification failed', { cause })
  }
  if (!ok)
    throw new BadSignatureError('signature verification failed')
}

/**
 * Set req.proposerSig to the proposer's signature over the canonical envelope
 * digest (contract.SignEnvelope). Computed before assignment so proposerSig
 * never enters the preimage. Returns a new request (immutable update).
 */
export function signEnvelope(priv: Uint8Array, req: SigningRequest): SigningRequest {
  const digest = envelopeDigest(req)
  return { ...req, proposerSig: signDigest(priv, digest) }
}

/** Re-derive the canonical digest and verify req.proposerSig (device pre-MPC check). */
export function verifyProposerSig(req: SigningRequest, proposerPub: Uint8Array): void {
  verifyDigest(proposerPub, envelopeDigest(req), req.proposerSig)
}
