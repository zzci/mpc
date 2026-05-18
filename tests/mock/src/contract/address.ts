import { sha256 } from '@noble/hashes/sha2.js'
import { keccak_256 } from '@noble/hashes/sha3.js'
import { InvalidEnvelopeError } from './errors.ts'

// Byte-exact port of internal/addr/addr.go: secp256k1 → ETH/BSC (keccak256 +
// EIP-55) and TRON (Base58Check, 0x41 || keccak256[12:]). Used to confirm a
// recovered RSV public key derives the group address coord returned at A1.

const UNCOMPRESSED_PUBKEY_LEN = 65
const TRON_PREFIX = 0x41
const BASE58_ALPHABET = '123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz'

/** Validate an uncompressed secp256k1 pubkey and return its 64-byte body (X‖Y). */
function pubKeyBody(pub: Uint8Array): Uint8Array {
  if (pub.length !== UNCOMPRESSED_PUBKEY_LEN)
    throw new InvalidEnvelopeError(`invalid pubkey: got ${pub.length} bytes, want ${UNCOMPRESSED_PUBKEY_LEN}`)
  if (pub[0] !== 0x04)
    throw new InvalidEnvelopeError(`invalid pubkey: prefix 0x${pub[0]?.toString(16)}, want 0x04`)
  return pub.slice(1)
}

/** EIP-55 mixed-case hex with 0x prefix (addr.toChecksumHex). */
function toChecksumHex(addr20: Uint8Array): string {
  const lower = Array.from(addr20, b => b.toString(16).padStart(2, '0')).join('')
  const hash = keccak_256(new TextEncoder().encode(lower))
  let out = ''
  for (let i = 0; i < lower.length; i++) {
    const c = lower[i] as string
    const hashByte = hash[i >> 1] as number
    const nibble = i % 2 === 0 ? hashByte >> 4 : hashByte & 0x0F
    out += (c > '9' && nibble > 7) ? c.toUpperCase() : c
  }
  return `0x${out}`
}

/** ETH / BSC address (identical scheme): last 20 bytes of keccak256(body), EIP-55. */
export function evmAddress(pub: Uint8Array): string {
  return toChecksumHex(keccak_256(pubKeyBody(pub)).slice(12))
}

function base58Encode(input: Uint8Array): string {
  let n = 0n
  for (const b of input) n = n * 256n + BigInt(b)
  let out = ''
  while (n > 0n) {
    const r = Number(n % 58n)
    n /= 58n
    out = (BASE58_ALPHABET[r] as string) + out
  }
  for (const b of input) {
    if (b === 0)
      out = (BASE58_ALPHABET[0] as string) + out
    else break
  }
  return out
}

/**
 * TRON Base58Check address (btcutil base58.CheckEncode semantics):
 * base58( ver ‖ payload ‖ sha256(sha256(ver‖payload))[:4] ), ver = 0x41,
 * payload = keccak256(body)[12:].
 */
export function tronAddress(pub: Uint8Array): string {
  const payload = keccak_256(pubKeyBody(pub)).slice(12)
  const versioned = new Uint8Array(1 + payload.length)
  versioned[0] = TRON_PREFIX
  versioned.set(payload, 1)
  const checksum = sha256(sha256(versioned)).slice(0, 4)
  const full = new Uint8Array(versioned.length + 4)
  full.set(versioned, 0)
  full.set(checksum, versioned.length)
  return base58Encode(full)
}
