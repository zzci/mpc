/**
 * Byte-exact ports of the Go canonical-encoding primitives
 * (internal/server/coord/provision_canonical.go, auth.go,
 * internal/contract/canonical.go). Every helper here must match its Go
 * counterpart bit-for-bit — the canonical preimages are signed and
 * cross-verified by Go, so any divergence silently breaks the ring.
 */

import { Buffer } from 'node:buffer'

/** Big-endian uint64 (Go `binary.BigEndian.PutUint64`). */
export function putU64(v: bigint | number): Uint8Array {
  const b = new Uint8Array(8)
  let x = BigInt(v) & 0xFFFFFFFFFFFFFFFFn
  for (let i = 7; i >= 0; i--) {
    b[i] = Number(x & 0xFFn)
    x >>= 8n
  }
  return b
}

/** Big-endian int64 as two's-complement (Go `putI64` = `putU64(uint64(v))`). */
export function putI64(v: bigint | number): Uint8Array {
  let x = BigInt(v)
  if (x < 0n)
    x += 1n << 64n
  return putU64(x)
}

/** Big-endian uint32 (Go `binary.BigEndian.PutUint32` / AppendUint32). */
export function putU32(v: number): Uint8Array {
  const b = new Uint8Array(4)
  const x = v >>> 0
  b[0] = (x >>> 24) & 0xFF
  b[1] = (x >>> 16) & 0xFF
  b[2] = (x >>> 8) & 0xFF
  b[3] = x & 0xFF
  return b
}

/** Length-prefixed: 4-byte BE uint32 length, then raw bytes (Go `putLP`/`appendLP`). */
export function putLP(v: Uint8Array): Uint8Array {
  return concat(putU32(v.length), v)
}

const utf8 = new TextEncoder()

/** UTF-8 + Unicode NFC, then length-prefixed (Go `putStr` = `putLP(NFC(s))`). */
export function putStr(s: string): Uint8Array {
  return putLP(utf8.encode(s.normalize('NFC')))
}

/** UTF-8 length-prefixed WITHOUT NFC (Go member-auth `appendLP([]byte(s))`). */
export function putRawStr(s: string): Uint8Array {
  return putLP(utf8.encode(s))
}

export function concat(...parts: Uint8Array[]): Uint8Array {
  let n = 0
  for (const p of parts)
    n += p.length
  const out = new Uint8Array(n)
  let o = 0
  for (const p of parts) {
    out.set(p, o)
    o += p.length
  }
  return out
}

export function utf8Bytes(s: string): Uint8Array {
  return utf8.encode(s)
}

export function hexToBytes(hex: string): Uint8Array {
  const h = hex.startsWith('0x') ? hex.slice(2) : hex
  if (h.length % 2 !== 0)
    throw new Error(`hexToBytes: odd-length hex (${h.length})`)
  const out = new Uint8Array(h.length / 2)
  for (let i = 0; i < out.length; i++) {
    const byte = Number.parseInt(h.slice(i * 2, i * 2 + 2), 16)
    if (Number.isNaN(byte))
      throw new Error(`hexToBytes: bad hex at ${i * 2}`)
    out[i] = byte
  }
  return out
}

const HEX = '0123456789abcdef'
export function bytesToHex(b: Uint8Array): string {
  let s = ''
  for (const x of b)
    s += HEX[x >> 4]! + HEX[x & 0x0F]!
  return s
}

/** Go `encoding/json` marshals `[]byte` as base64 std; mirror that on the wire. */
export function bytesToB64(b: Uint8Array): string {
  return Buffer.from(b).toString('base64')
}

export function b64ToBytes(s: string): Uint8Array {
  return new Uint8Array(Buffer.from(s, 'base64'))
}

/**
 * RFC3339Nano string -> int64 unix milliseconds, matching Go
 * `time.Parse(time.RFC3339Nano, s).UTC().UnixMilli()`. `Date.parse` handles
 * RFC3339 (incl. fractional seconds and `Z`/offset) and yields ms directly.
 */
export function rfc3339ToMillis(s: string): bigint {
  const ms = Date.parse(s)
  if (Number.isNaN(ms))
    throw new Error(`rfc3339ToMillis: unparseable time ${JSON.stringify(s)}`)
  return BigInt(ms)
}
