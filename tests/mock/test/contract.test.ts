import type { SigningRequest } from '../src/contract/index.ts'
import { describe, expect, it } from 'bun:test'
import {
  canonicalBytes,
  envelopeDigest,
  InvalidEnvelopeError,
  metaHash,
  uuidBytes,
  verifyMetaHash,
} from '../src/contract/index.ts'
import { toHex } from '../src/shared/bytes.ts'
import golden from '../testdata/golden.json'

// Property/edge tests beyond the golden anchor: the invariants the Go
// canonical_test.go / metahash_test.go pin, re-asserted in TypeScript.

function sampleReq(): SigningRequest {
  return {
    version: 1,
    requestId: '123e4567-e89b-12d3-a456-426614174000',
    groupId: 'grp-1',
    chain: 'ethereum',
    unsignedTx: new Uint8Array([1, 2, 3]),
    digest32: new Uint8Array(32).fill(0xAB),
    proposer: 'proposer-A',
    createdAt: 1_700_000_000_000,
    expiry: 1_700_000_900_000,
    businessInfo: null,
    metaHash: new Uint8Array(32).fill(0xCD),
    proposerSig: new Uint8Array(0),
  }
}

describe('uuidBytes', () => {
  it('hyphenated, unhyphenated and upper-case decode identically', () => {
    const a = uuidBytes('123e4567-e89b-12d3-a456-426614174000')
    const b = uuidBytes('123e4567e89b12d3a456426614174000')
    const c = uuidBytes('123E4567-E89B-12D3-A456-426614174000')
    expect(toHex(a)).toBe(toHex(b))
    expect(toHex(a)).toBe(toHex(c))
    expect(a.length).toBe(16)
  })

  it('rejects malformed uuid', () => {
    expect(() => uuidBytes('not-a-uuid')).toThrow(InvalidEnvelopeError)
    expect(() => uuidBytes('zz3e4567-e89b-12d3-a456-426614174000')).toThrow(InvalidEnvelopeError)
    expect(() => uuidBytes('123e4567xe89b-12d3-a456-426614174000')).toThrow(InvalidEnvelopeError)
  })
})

describe('canonicalBytes invariants', () => {
  it('is deterministic and domain-prefixed', () => {
    const r = sampleReq()
    expect(toHex(canonicalBytes(r))).toBe(toHex(canonicalBytes(r)))
    expect(toHex(canonicalBytes(r)).startsWith(golden.domainHex)).toBe(true)
  })

  it('every covered field is sensitive; proposerSig is not', () => {
    const base = toHex(canonicalBytes(sampleReq()))
    const muts: Array<(r: SigningRequest) => void> = [
      r => (r.version = 2),
      r => (r.requestId = '00000000-0000-0000-0000-000000000001'),
      r => (r.groupId = 'grp-2'),
      r => (r.chain = 'tron'),
      r => (r.unsignedTx = new Uint8Array([9])),
      r => (r.digest32 = new Uint8Array(32).fill(1)),
      r => (r.proposer = 'proposer-B'),
      r => (r.createdAt = 1),
      r => (r.expiry = 1),
      r => (r.metaHash = new Uint8Array(32).fill(2)),
    ]
    for (const m of muts) {
      const r = sampleReq()
      m(r)
      expect(toHex(canonicalBytes(r))).not.toBe(base)
    }
    const r = sampleReq()
    r.proposerSig = new Uint8Array([1, 2, 3])
    expect(toHex(canonicalBytes(r))).toBe(base)
  })

  it('no concatenation ambiguity across adjacent variable fields', () => {
    const a = sampleReq()
    a.groupId = 'ab'
    a.chain = 'cd'
    const b = sampleReq()
    b.groupId = 'a'
    b.chain = 'bcd'
    expect(toHex(canonicalBytes(a))).not.toBe(toHex(canonicalBytes(b)))
  })

  it('NFC-normalizes string fields (precomposed == decomposed)', () => {
    const pre = sampleReq()
    pre.proposer = 'é' // é
    const dec = sampleReq()
    dec.proposer = 'é' // e + combining acute
    expect(toHex(canonicalBytes(pre))).toBe(toHex(canonicalBytes(dec)))
  })

  it('rejects wrong fixed-length fields', () => {
    const shortD = sampleReq()
    shortD.digest32 = new Uint8Array([1])
    expect(() => canonicalBytes(shortD)).toThrow(InvalidEnvelopeError)
    const shortM = sampleReq()
    shortM.metaHash = new Uint8Array([2])
    expect(() => canonicalBytes(shortM)).toThrow(InvalidEnvelopeError)
  })

  it('envelopeDigest is sha256 of the preimage', () => {
    const r = sampleReq()
    expect(envelopeDigest(r).length).toBe(32)
  })
})

describe('metaHash omitempty + JCS', () => {
  it('absent businessInfo → EMPTY_META_HASH (not H("{}"))', () => {
    expect(toHex(metaHash(null))).toBe(golden.emptyMetaHashHex)
    expect(toHex(metaHash(undefined))).toBe(golden.emptyMetaHashHex)
  })

  it('present-but-all-empty businessInfo hashes to H(JCS("{}")), distinct from absent', () => {
    const empty = metaHash({ title: '', items: [], refs: {} })
    expect(toHex(empty)).not.toBe(golden.emptyMetaHashHex)
  })

  it('JCS re-sorts keys: field order is irrelevant', () => {
    const a = metaHash({ title: 'x', memo: 'y' })
    const b = metaHash({ memo: 'y', title: 'x' })
    expect(toHex(a)).toBe(toHex(b))
  })

  it('verifyMetaHash accepts the matching hash and rejects a mismatch', () => {
    const bi = { title: 'x' }
    expect(() => verifyMetaHash(metaHash(bi), bi)).not.toThrow()
    expect(() => verifyMetaHash(metaHash(bi), { title: 'y' })).toThrow(InvalidEnvelopeError)
  })
})
