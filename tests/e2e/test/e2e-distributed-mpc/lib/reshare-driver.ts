/**
 * B10 reshare driver for the §G(3) `reshare-3-to-4` admission test.
 *
 * Re-implements the canonical reshare preimage from
 * `internal/server/coord/reshare.go:reshareRequestDigest` so the test can
 * POST a syntactically valid B10 envelope without depending on cli/device.
 * The on-the-wire signing layer (X-Member-* headers) reuses the parent
 * suite's memberAuth helpers — identical to AttestationClient's
 * `signedHeaders` modulo the method label "B10:reshare".
 */
import type { MemberKey } from '../../../src/lib/coord-client.ts'
import { sha256 } from '@noble/hashes/sha2.js'
import { bytesToB64, utf8Bytes } from '../../../src/lib/bytes.ts'
import { memberAuthDigest, memberAuthParams } from '../../../src/lib/canonical.ts'
import { signDigestDER } from '../../../src/lib/crypto.ts'

const RESHARE_DOMAIN: Uint8Array = (() => {
  const tag = utf8Bytes('TSS-COORD-RESHARE-REQUEST-CANONICAL-v1')
  const out = new Uint8Array(tag.length + 1)
  out.set(tag, 0)
  // domain suffix 0x00 matches `append([]byte("…"), 0x00)` in reshare.go.
  return out
})()

function concat(parts: Uint8Array[]): Uint8Array {
  let total = 0
  for (const p of parts) total += p.length
  const out = new Uint8Array(total)
  let off = 0
  for (const p of parts) {
    out.set(p, off)
    off += p.length
  }
  return out
}

function be32(n: number): Uint8Array {
  const b = new Uint8Array(4)
  new DataView(b.buffer).setUint32(0, n, false)
  return b
}

function be64(n: bigint): Uint8Array {
  const b = new Uint8Array(8)
  new DataView(b.buffer).setBigUint64(0, n, false)
  return b
}

function putLP(v: Uint8Array): Uint8Array {
  return concat([be32(v.length), v])
}

function putStr(s: string): Uint8Array {
  // reshare.go uses norm.NFC.Bytes; ASCII session IDs (the ones used in
  // tests) round-trip identically under NFC, so a plain UTF-8 encode is
  // sufficient here. The Go side will recompute via NFC over the same
  // ASCII bytes and arrive at the same preimage.
  return putLP(utf8Bytes(s))
}

/** Mirrors reshareRequestDigest (internal/server/coord/reshare.go:66). */
export function reshareDigest(
  sessionID: string,
  oldSet: Uint8Array[],
  newSet: Uint8Array[],
  deadlineMs: bigint,
): Uint8Array {
  const parts: Uint8Array[] = [RESHARE_DOMAIN, putStr(sessionID), be32(oldSet.length)]
  for (const m of oldSet) parts.push(putLP(m))
  parts.push(be32(newSet.length))
  for (const m of newSet) parts.push(putLP(m))
  parts.push(be64(deadlineMs))
  return sha256(concat(parts))
}

/** X-Member-* headers for a B10:reshare POST (matches AttestationClient.signedHeaders). */
export function reshareHeaders(key: MemberKey, groupId: string, bodyJson: string): Record<string, string> {
  const ts = BigInt(Date.now())
  const nonce = crypto.getRandomValues(new Uint8Array(16))
  const params = memberAuthParams('B10:reshare', groupId, utf8Bytes(bodyJson))
  const digest = memberAuthDigest(key.memberId, 'B10:reshare', params, ts, nonce)
  const sig = signDigestDER(key.priv, digest)
  return {
    'X-Member-Id': key.memberId,
    'X-Member-Ts': ts.toString(),
    'X-Member-Nonce': bytesToB64(nonce),
    'X-Member-Sig': bytesToB64(sig),
    'Content-Type': 'application/json',
  }
}
