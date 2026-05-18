import type { SigningRequest } from './types.ts'
import { sha256 } from '@noble/hashes/sha2.js'
import { InvalidEnvelopeError } from './errors.ts'

// Byte-exact TypeScript port of internal/contract/canonical.go
// (docs/spec/envelope-canonical.md §2). The committed testdata/golden.json,
// produced by the real Go package, is the authoritative cross-language anchor
// asserted in test/golden.test.ts — this file MUST reproduce those bytes.

/**
 * DOMAIN = ASCII("TSS-ENVELOPE-CANONICAL-v1") ‖ 0x00 (§2.2). The trailing NUL
 * terminates the constant and versions the scheme (orthogonal to
 * SigningRequest.version).
 */
export const CANONICAL_DOMAIN: Uint8Array = (() => {
  const tag = new TextEncoder().encode('TSS-ENVELOPE-CANONICAL-v1')
  const out = new Uint8Array(tag.length + 1)
  out.set(tag, 0)
  out[tag.length] = 0x00
  return out
})()

const FIXED_HASH_LEN = 32
const MAX_LEN_PREFIX = 0xFFFFFFFF

function nfcBytes(s: string): Uint8Array {
  // §2.3 rule 2: Unicode NFC then UTF-8, so visually identical strings from
  // different input methods canonicalize identically across parties.
  return new TextEncoder().encode(s.normalize('NFC'))
}

/**
 * Parse an RFC 4122 UUID string into its 16 raw bytes (§2.3). Accepts the
 * hyphenated 8-4-4-4-12 form and the 32-hex unhyphenated form, hex
 * case-insensitive; anything else is an invalid envelope.
 */
export function uuidBytes(s: string): Uint8Array {
  let hexStr: string
  if (s.length === 36) {
    if (s[8] !== '-' || s[13] !== '-' || s[18] !== '-' || s[23] !== '-')
      throw new InvalidEnvelopeError('malformed requestId UUID')
    hexStr = s.slice(0, 8) + s.slice(9, 13) + s.slice(14, 18) + s.slice(19, 23) + s.slice(24)
  }
  else if (s.length === 32) {
    hexStr = s
  }
  else {
    throw new InvalidEnvelopeError(`requestId is not a UUID (len ${s.length})`)
  }
  if (!/^[0-9a-f]{32}$/i.test(hexStr))
    throw new InvalidEnvelopeError('requestId not hex')
  const out = new Uint8Array(16)
  for (let i = 0; i < 16; i++)
    out[i] = Number.parseInt(hexStr.slice(i * 2, i * 2 + 2), 16)
  return out
}

class ByteBuf {
  private chunks: Uint8Array[] = []

  write(b: Uint8Array): void {
    this.chunks.push(b)
  }

  writeUint64(v: number | bigint): void {
    const buf = new Uint8Array(8)
    new DataView(buf.buffer).setBigUint64(0, BigInt(v), false)
    this.chunks.push(buf)
  }

  // writeInt64: Go writes uint64(v) two's-complement big-endian; setBigInt64
  // produces the identical bytes for the int64 range.
  writeInt64(v: number | bigint): void {
    const buf = new Uint8Array(8)
    new DataView(buf.buffer).setBigInt64(0, BigInt(v), false)
    this.chunks.push(buf)
  }

  writeLenPrefixed(field: string, b: Uint8Array): void {
    if (b.length > MAX_LEN_PREFIX)
      throw new InvalidEnvelopeError(`${field} exceeds uint32 length`)
    const lp = new Uint8Array(4)
    new DataView(lp.buffer).setUint32(0, b.length, false)
    this.chunks.push(lp, b)
  }

  bytes(): Uint8Array {
    let n = 0
    for (const c of this.chunks) n += c.length
    const out = new Uint8Array(n)
    let off = 0
    for (const c of this.chunks) {
      out.set(c, off)
      off += c.length
    }
    return out
  }
}

/**
 * Build the canonical preimage P of an envelope (§2.1):
 *
 *   P = DOMAIN ‖ F(version) ‖ F(requestId) ‖ F(groupId) ‖ F(chain)
 *         ‖ F(unsignedTx) ‖ F(digest32) ‖ F(proposer)
 *         ‖ F(createdAt) ‖ F(expiry) ‖ F(metaHash)
 *
 * businessInfo never enters P (carried via metaHash); proposerSig never enters
 * P (it signs P). Bytes are derived from logical values only.
 */
export function canonicalBytes(req: SigningRequest): Uint8Array {
  const buf = new ByteBuf()
  buf.write(CANONICAL_DOMAIN)
  buf.writeUint64(req.version)
  buf.write(uuidBytes(req.requestId))
  buf.writeLenPrefixed('groupId', nfcBytes(req.groupId))
  buf.writeLenPrefixed('chain', nfcBytes(req.chain))
  buf.writeLenPrefixed('unsignedTx', req.unsignedTx)

  if (req.digest32.length !== FIXED_HASH_LEN)
    throw new InvalidEnvelopeError(`digest32 is ${req.digest32.length} bytes, want ${FIXED_HASH_LEN}`)
  buf.write(req.digest32)

  buf.writeLenPrefixed('proposer', nfcBytes(req.proposer))
  buf.writeInt64(req.createdAt)
  buf.writeInt64(req.expiry)

  if (req.metaHash.length !== FIXED_HASH_LEN)
    throw new InvalidEnvelopeError(`metaHash is ${req.metaHash.length} bytes, want ${FIXED_HASH_LEN}`)
  buf.write(req.metaHash)

  return buf.bytes()
}

/** SHA-256(canonicalBytes(req)) — the 32-byte value the proposer signs (§2.4). */
export function envelopeDigest(req: SigningRequest): Uint8Array {
  return sha256(canonicalBytes(req))
}
