import { secp256k1 } from '@noble/curves/secp256k1.js'
import { bytesToNumberBE } from '@noble/curves/utils.js'
import { BadSignatureError } from './errors.ts'

// Recover the signer's public key from the {R,S,V} wire form coord delivers
// (api.md A4 / internal/server/coord/callback.go verifyRSV / internal/mpc
// Signature.Compact): 65 bytes [V+27 || R(32) || S(32)], V the raw recovery
// id in {0..3}. This is the trust-free check the external service repeats
// against the group's A1 address (testing.md §3.1 "ecrecover/TRON 验签").

export const RSV_LEN = 65

/**
 * Recover the uncompressed secp256k1 public key (0x04‖X‖Y, 65 bytes) that
 * produced rsv over digest32. Throws BadSignatureError on any malformed input
 * or unrecoverable signature.
 */
export function recoverPubFromRSV(digest32: Uint8Array, rsv: Uint8Array): Uint8Array {
  if (digest32.length !== 32)
    throw new BadSignatureError('digest32 must be 32 bytes')
  if (rsv.length !== RSV_LEN)
    throw new BadSignatureError(`rsv must be ${RSV_LEN} bytes`)
  const header = rsv[0] as number
  const recid = header - 27
  if (recid < 0 || recid > 3)
    throw new BadSignatureError(`rsv recovery byte out of range: ${header}`)
  const r = bytesToNumberBE(rsv.slice(1, 33))
  const s = bytesToNumberBE(rsv.slice(33, 65))
  try {
    const sig = new secp256k1.Signature(r, s, recid)
    return sig.recoverPublicKey(digest32).toBytes(false)
  }
  catch (cause) {
    throw new BadSignatureError('rsv does not recover a public key', { cause })
  }
}
