import type { BusinessInfo } from './types.ts'
import { sha256 } from '@noble/hashes/sha2.js'
import canonicalize from 'canonicalize'
import { InvalidEnvelopeError } from './errors.ts'

// Byte-exact port of internal/contract/metahash.go
// (docs/spec/envelope-canonical.md §4). metaHash = SHA-256 over the RFC 8785
// JCS bytes of businessInfo, or EmptyMetaHash when absent. The Go side does
// json.Marshal(bi) (with `omitempty`) then jcs.Transform; we reproduce the
// `omitempty` field set, then JCS-canonicalize, so the hash input is identical.

/**
 * SHA-256 of the zero-length byte string — metaHash when businessInfo is
 * absent (§4.2). The well-known constant e3b0c442…b855; NOT H("{}") nor H('""').
 */
export const EMPTY_META_HASH: Uint8Array = sha256(new Uint8Array(0))

// Reproduce Go encoding/json `omitempty`: omit empty string, empty slice,
// empty map, and nil. A present-but-all-empty BusinessInfo marshals to "{}"
// (distinct from absent → EMPTY_META_HASH), matching contract.MetaHash.
function toMarshaledObject(bi: BusinessInfo): Record<string, unknown> {
  const o: Record<string, unknown> = {}
  if (bi.title)
    o.title = bi.title
  if (bi.summary)
    o.summary = bi.summary
  if (bi.items && bi.items.length > 0)
    o.items = bi.items
  if (bi.refs && Object.keys(bi.refs).length > 0)
    o.refs = bi.refs
  if (bi.requester)
    o.requester = bi.requester
  if (bi.memo)
    o.memo = bi.memo
  if (bi.displayHints && Object.keys(bi.displayHints).length > 0)
    o.displayHints = bi.displayHints
  return o
}

/**
 * metaHash for a structured businessInfo (§4.2): SHA-256 over RFC 8785 JCS
 * bytes, or EMPTY_META_HASH when bi is null/undefined. JCS re-sorts keys, so
 * field order is irrelevant — only the present-key set and values matter.
 */
export function metaHash(bi: BusinessInfo | null | undefined): Uint8Array {
  if (bi === null || bi === undefined)
    return EMPTY_META_HASH
  const canon = canonicalize(toMarshaledObject(bi))
  if (canon === undefined)
    throw new InvalidEnvelopeError('businessInfo not canonicalizable JSON')
  return sha256(new TextEncoder().encode(canon))
}

/**
 * Check req.metaHash equals metaHash(businessInfo) — the device pre-MPC check
 * of protocol.md:25 ("metaHash==H(businessInfo)").
 */
export function verifyMetaHash(metaHashBytes: Uint8Array, bi: BusinessInfo | null | undefined): void {
  const want = metaHash(bi)
  if (want.length !== metaHashBytes.length || !want.every((b, i) => b === metaHashBytes[i]))
    throw new InvalidEnvelopeError('metaHash does not match businessInfo')
}
