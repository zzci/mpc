/**
 * Crypto primitives, byte-exact with the Go stack
 * (btcec/v2 ECDSA + sha3 legacy Keccak + btcutil base58 + internal/addr).
 * Audited libraries only (`@noble/curves`, `@noble/hashes`) — no
 * hand-rolled primitive (docs/design/testing.md §6).
 */
import { secp256k1 } from '@noble/curves/secp256k1.js'
import { sha256 as nobleSha256 } from '@noble/hashes/sha2.js'
import { keccak_256 } from '@noble/hashes/sha3.js'
import { bytesToHex, hexToBytes } from './bytes.ts'

export function sha256(data: Uint8Array): Uint8Array {
  return nobleSha256(data)
}

export function sha256d(data: Uint8Array): Uint8Array {
  return nobleSha256(nobleSha256(data))
}

/** Legacy Keccak-256 (NOT NIST SHA3) — matches Go `sha3.NewLegacyKeccak256`. */
export function keccak256(data: Uint8Array): Uint8Array {
  return keccak_256(data)
}

/**
 * DER secp256k1 ECDSA over a 32-byte digest — the project's single signing
 * primitive (internal/contract/proposer.go `SignDigest`: btcec
 * `ecdsa.Sign(priv, digest).Serialize()`). btcec is RFC6979-deterministic +
 * low-S + DER; noble with `prehash:false, lowS:true, format:'der'` is the
 * same construction, so signatures are byte-identical.
 */
export function signDigestDER(priv: Uint8Array, digest32: Uint8Array): Uint8Array {
  if (digest32.length !== 32)
    throw new Error('signDigestDER: digest must be 32 bytes')
  return secp256k1.sign(digest32, priv, { prehash: false, lowS: true, format: 'der' })
}

/** Verify a DER signature over a 32-byte digest (internal/contract `VerifyDigest`). */
export function verifyDigestDER(pub: Uint8Array, digest32: Uint8Array, der: Uint8Array): boolean {
  try {
    return secp256k1.verify(der, digest32, pub, { prehash: false, format: 'der', lowS: false })
  }
  catch {
    return false
  }
}

/** Compressed (33-byte) secp256k1 public key for a private scalar. */
export function pubCompressed(priv: Uint8Array): Uint8Array {
  return secp256k1.getPublicKey(priv, true)
}

/** Uncompressed (65-byte, 0x04‖X‖Y) secp256k1 public key for a private scalar. */
export function pubUncompressed(priv: Uint8Array): Uint8Array {
  return secp256k1.getPublicKey(priv, false)
}

export function toUncompressed(pub: Uint8Array): Uint8Array {
  return secp256k1.Point.fromBytes(pub).toBytes(false)
}

/**
 * ECDSA public-key recovery from the chain `{R,S,V}` compact form
 * `[V+27 ‖ R ‖ S]` (how an ETH/BSC consumer verifies; mirrors
 * internal/cli `recoverPub` -> btcec `RecoverCompact`). `v` is the 0/1
 * recovery id stored in `DeviceResult.SigV`. Returns the uncompressed key.
 */
export function ecrecover(digest32: Uint8Array, rHex: string, sHex: string, v: number): Uint8Array {
  const r = hexToBytes(rHex)
  const s = hexToBytes(sHex)
  if (r.length !== 32 || s.length !== 32)
    throw new Error('ecrecover: R and S must be 32 bytes each')
  // noble 'recovered' format = [recovery(0..3) ‖ r(32) ‖ s(32)].
  const recovered = new Uint8Array(65)
  recovered[0] = v & 0xFF
  recovered.set(r, 1)
  recovered.set(s, 33)
  // recoverPublicKey operates on the message hash directly (no prehash);
  // returns the recovered Point — emit it uncompressed for address use.
  const sig = secp256k1.Signature.fromBytes(recovered, 'recovered')
  return sig.recoverPublicKey(digest32).toBytes(false)
}

/** N/2 — the low-S boundary tss-lib's finalize enforces (rejects malleability). */
const HALF_N = secp256k1.Point.Fn.ORDER >> 1n

export function isLowS(sHex: string): boolean {
  return BigInt(`0x${sHex}`) <= HALF_N
}

/** 64-byte public-key body (X‖Y) — Go `internal/addr.pubKeyBody`. */
function pubKeyBody(pub: Uint8Array): Uint8Array {
  const u = pub.length === 65 ? pub : toUncompressed(pub)
  if (u.length !== 65 || u[0] !== 0x04)
    throw new Error('pubKeyBody: expected uncompressed 0x04 key')
  return u.slice(1)
}

/** EIP-55 mixed-case checksum hex — verbatim port of Go `toChecksumHex`. */
function toChecksumHex(addr20: Uint8Array): string {
  const lower = bytesToHex(addr20) // 40 lowercase hex chars, no 0x
  const hash = keccak256(new TextEncoder().encode(lower))
  const out = [...lower]
  for (let i = 0; i < out.length; i++) {
    const hb = hash[i >> 1]!
    const nibble = i % 2 === 0 ? hb >> 4 : hb & 0x0F
    const c = out[i]!
    if (c > '9' && nibble > 7)
      out[i] = c.toUpperCase()
  }
  return `0x${out.join('')}`
}

/** ETH (and BSC — identical scheme) address from a public key. */
export function ethAddress(pub: Uint8Array): string {
  return toChecksumHex(keccak256(pubKeyBody(pub)).slice(12))
}

export const bscAddress = ethAddress

const B58 = '123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz'

function base58Encode(b: Uint8Array): string {
  let zeros = 0
  while (zeros < b.length && b[zeros] === 0)
    zeros++
  let x = 0n
  for (const byte of b)
    x = x * 256n + BigInt(byte)
  let out = ''
  while (x > 0n) {
    const r = Number(x % 58n)
    x /= 58n
    out = B58[r]! + out
  }
  return '1'.repeat(zeros) + out
}

/** btcutil `base58.CheckEncode`: base58(version ‖ payload ‖ sha256d(...)[:4]). */
export function base58Check(payload: Uint8Array, version: number): string {
  const body = new Uint8Array(1 + payload.length)
  body[0] = version & 0xFF
  body.set(payload, 1)
  const checksum = sha256d(body).slice(0, 4)
  const full = new Uint8Array(body.length + 4)
  full.set(body, 0)
  full.set(checksum, body.length)
  return base58Encode(full)
}

const TRON_PREFIX = 0x41

/** TRON Base58Check address (0x41 ‖ keccak256(body)[12:]) — Go `TronAddress`. */
export function tronAddress(pub: Uint8Array): string {
  return base58Check(keccak256(pubKeyBody(pub)).slice(12), TRON_PREFIX)
}
