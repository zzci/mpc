/**
 * Byte-exact crypto vectors vs the Go stack (internal/addr/addr_test.go,
 * internal/cli eip155 anchor, btcec ecdsa). These run unconditionally and
 * gate every commit — they prove the TS port matches Go before any live ring.
 */
import { secp256k1 } from '@noble/curves/secp256k1.js'
import { describe, expect, it } from 'bun:test'
import { bytesToHex, hexToBytes } from '../../src/lib/bytes.ts'
import {
  base58Check,
  ecrecover,
  ethAddress,
  isLowS,
  keccak256,
  pubUncompressed,
  signDigestDER,
  tronAddress,
  verifyDigestDER,
} from '../../src/lib/crypto.ts'
import { EIP155_DIGEST, EIP155_RLP } from '../../src/lib/vectors.ts'

/** scalar n -> 32-byte BE private key (priv[31]=n), as addr_test.go uses. */
function scalarPriv(n: number): Uint8Array {
  const b = new Uint8Array(32)
  b[31] = n
  return b
}

describe('eIP-155 anchor', () => {
  it('keccak256(EIP155_RLP) == EIP155_DIGEST', () => {
    expect(bytesToHex(keccak256(hexToBytes(EIP155_RLP)))).toBe(EIP155_DIGEST)
  })
})

describe('eTH/BSC EIP-55 address goldens (addr_test.go)', () => {
  const vectors: Array<[number, string]> = [
    [1, '0x7E5F4552091A69125d5DfCb7b8C2659029395Bdf'],
    [2, '0x2B5AD5c4795c026514f8317c7a215E218DcCD6cF'],
    [3, '0x6813Eb9362372EEF6200f3b1dbC3f819671cBA69'],
  ]
  for (const [n, addr] of vectors) {
    it(`scalar ${n} -> ${addr}`, () => {
      expect(ethAddress(pubUncompressed(scalarPriv(n)))).toBe(addr)
    })
  }
})

describe('tRON Base58Check (addr_test.go)', () => {
  it('scalar 1 -> TMVQGm1qAQYVdetCeGRRkTWYYrLXuHK2HC', () => {
    expect(tronAddress(pubUncompressed(scalarPriv(1)))).toBe('TMVQGm1qAQYVdetCeGRRkTWYYrLXuHK2HC')
  })
  it('checkEncode doc vector', () => {
    const payload = hexToBytes('E552F6487585C2B58BC2C9BB4492BC1F17132CD0')
    expect(base58Check(payload, 0x41)).toBe('TWsm8HtU2A5eEzoT8ev8yaoFjHsXLLrckb')
  })
})

describe('dER sign/verify (btcec parity: RFC6979 + low-S + DER)', () => {
  it('round-trips and rejects tampering', () => {
    const priv = scalarPriv(7)
    const pub = pubUncompressed(priv)
    const digest = keccak256(hexToBytes(EIP155_RLP))
    const sig = signDigestDER(priv, digest)
    expect(verifyDigestDER(pub, digest, sig)).toBe(true)
    const bad = new Uint8Array(digest)
    bad[0] = (bad[0] ?? 0) ^ 0x01
    expect(verifyDigestDER(pub, bad, sig)).toBe(false)
  })
  it('is deterministic (RFC6979)', () => {
    const priv = scalarPriv(9)
    const d = keccak256(hexToBytes(EIP155_RLP))
    expect(bytesToHex(signDigestDER(priv, d))).toBe(bytesToHex(signDigestDER(priv, d)))
  })
})

describe('ecrecover over chain {R,S,V} == signer key, low-S', () => {
  it('recovers the uncompressed master key', () => {
    const priv = scalarPriv(11)
    const digest = hexToBytes(EIP155_DIGEST)
    const rec = secp256k1.sign(digest, priv, { prehash: false, lowS: true, format: 'recovered' })
    const v = rec[0]!
    const rHex = bytesToHex(rec.slice(1, 33))
    const sHex = bytesToHex(rec.slice(33, 65))
    expect(bytesToHex(ecrecover(digest, rHex, sHex, v))).toBe(bytesToHex(pubUncompressed(priv)))
    expect(isLowS(sHex)).toBe(true)
  })
})
