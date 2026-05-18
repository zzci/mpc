import type { GroupPublic } from './contract/index.ts'
import { evmAddress, recoverPubFromRSV, tronAddress } from './contract/index.ts'
import { bytesEqual, fromB64, toHex } from './shared/bytes.ts'

// The external-service side of testing.md §3.1: independently re-verify the
// returned {R,S,V} with the three-chain real digest by recovering the public
// key and checking it derives the group's A1 evm/tron addresses. This repeats
// coord.verifyRSV's group-pubkey gate from a separate implementation, which is
// the whole point of the cross-language E2E.

export interface RSVVerification {
  ok: boolean
  recoveredPubHex: string
  evmAddress: string
  tronAddress: string
  evmMatches: boolean
  tronMatches: boolean
  pubkeyMatches: boolean
}

/**
 * Recover the signer pubkey from (digest32, rsv) and assert it matches the
 * group's A1 ecdsa_pubkey and both derived chain addresses. Any mismatch →
 * ok:false (a forged/mismatched RSV — coord's C8 defense, re-checked here).
 */
export function verifyGroupRSV(group: GroupPublic, digest32: Uint8Array, rsv: Uint8Array): RSVVerification {
  const rec = recoverPubFromRSV(digest32, rsv)
  const evm = evmAddress(rec)
  const tron = tronAddress(rec)
  const evmMatches = evm === group.evm_address
  const tronMatches = tron === group.tron_address
  const pubkeyMatches = matchesGroupPubkey(rec, group.ecdsa_pubkey)
  return {
    ok: evmMatches && tronMatches && pubkeyMatches,
    recoveredPubHex: toHex(rec),
    evmAddress: evm,
    tronAddress: tron,
    evmMatches,
    tronMatches,
    pubkeyMatches,
  }
}

// coord may publish the group key compressed (33) or uncompressed (65). The
// recovered key is uncompressed; compare on the shared 64-byte X‖Y body so
// the encoding form does not cause a false mismatch.
function matchesGroupPubkey(recoveredUncompressed: Uint8Array, ecdsaPubkeyB64: string): boolean {
  const pub = fromB64(ecdsaPubkeyB64)
  const recBody = recoveredUncompressed.slice(1) // strip 0x04
  if (pub.length === 65 && pub[0] === 0x04)
    return bytesEqual(pub.slice(1), recBody)
  if (pub.length === 33 && (pub[0] === 0x02 || pub[0] === 0x03)) {
    // X must match; parity of Y must match the prefix.
    const xMatches = bytesEqual(pub.slice(1), recBody.slice(0, 32))
    const yIsOdd = (recBody[63] as number) & 1
    const prefixOdd = pub[0] === 0x03 ? 1 : 0
    return xMatches && yIsOdd === prefixOdd
  }
  return false
}
