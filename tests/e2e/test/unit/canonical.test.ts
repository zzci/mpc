import type { GroupProvisioning } from '../../src/lib/canonical.ts'
/**
 * Canonical-preimage invariants vs the Go encoders
 * (provision_canonical.go, auth.go, canonical.go, metahash.go). Determinism,
 * field-sensitivity and the NFC-vs-raw distinction must hold or the signed
 * preimages diverge from Go and the ring silently breaks.
 */
import { describe, expect, it } from 'bun:test'
import { bytesToHex, putLP, putRawStr, putStr, putU64 } from '../../src/lib/bytes.ts'
import {
  EMPTY_META_HASH,
  envelopeDigest,
  groupProvisionDigest,

  memberAuthDigest,
} from '../../src/lib/canonical.ts'

const baseProv: GroupProvisioning = {
  version: 1,
  groupId: 'wallet-coord-e2e',
  ecdsaPubkey: new Uint8Array([0x04, 1, 2, 3]),
  groupPubkey: new Uint8Array([0x02, 9, 9]),
  thresholdT: 2,
  partiesN: 3,
  members: [
    { memberId: 'm0', identityPubkey: new Uint8Array([0xAA]) },
    { memberId: 'm1', identityPubkey: new Uint8Array([0xBB]) },
  ],
  createdAt: '2026-05-18T12:00:00Z',
}

describe('byte primitives match Go layout', () => {
  it('putU64 is 8-byte big-endian', () => {
    expect(bytesToHex(putU64(1))).toBe('0000000000000001')
    expect(bytesToHex(putU64(0x0102030405060708n))).toBe('0102030405060708')
  })
  it('putLP is 4-byte BE length prefix', () => {
    expect(bytesToHex(putLP(new Uint8Array([0xFF, 0xEE])))).toBe('00000002ffee')
  })
  it('putStr applies NFC; putRawStr does not', () => {
    // U+00E9 (é, NFC) vs U+0065 U+0301 (e + combining acute, NFD).
    const nfc = putStr('é')
    const nfd = putStr('é')
    expect(bytesToHex(nfc)).toBe(bytesToHex(nfd))
    const rawNfc = putRawStr('é')
    const rawNfd = putRawStr('é')
    expect(bytesToHex(rawNfc)).not.toBe(bytesToHex(rawNfd))
  })
})

describe('group provisioning digest', () => {
  it('is deterministic', () => {
    expect(bytesToHex(groupProvisionDigest(baseProv)))
      .toBe(bytesToHex(groupProvisionDigest(structuredClone(baseProv))))
  })
  it('is 32 bytes', () => {
    expect(groupProvisionDigest(baseProv).length).toBe(32)
  })
  it('is sensitive to every field', () => {
    const base = bytesToHex(groupProvisionDigest(baseProv))
    const mutators: Array<(p: GroupProvisioning) => GroupProvisioning> = [
      p => ({ ...p, groupId: `${p.groupId}x` }),
      p => ({ ...p, thresholdT: 1 }),
      p => ({ ...p, partiesN: 4 }),
      p => ({ ...p, ecdsaPubkey: new Uint8Array([0x04, 1, 2, 4]) }),
      p => ({ ...p, createdAt: '2026-05-18T12:00:01Z' }),
      p => ({ ...p, members: [...p.members, { memberId: 'm2', identityPubkey: new Uint8Array([0xCC]) }] }),
    ]
    for (const m of mutators)
      expect(bytesToHex(groupProvisionDigest(m(baseProv)))).not.toBe(base)
  })
  it('no length-prefix concatenation ambiguity (member split)', () => {
    const a = groupProvisionDigest({
      ...baseProv,
      members: [{ memberId: 'ab', identityPubkey: new Uint8Array([1]) }],
    })
    const b = groupProvisionDigest({
      ...baseProv,
      members: [{ memberId: 'a', identityPubkey: new Uint8Array([0x62, 1]) }],
    })
    expect(bytesToHex(a)).not.toBe(bytesToHex(b))
  })
})

describe('member-auth digest', () => {
  it('is deterministic and 32 bytes', () => {
    const params = new Uint8Array([1, 2, 3])
    const nonce = new Uint8Array([9, 9, 9, 9])
    const a = memberAuthDigest('m0', 'B5:heartbeat', params, 1747569600000n, nonce)
    const b = memberAuthDigest('m0', 'B5:heartbeat', params, 1747569600000n, nonce)
    expect(a.length).toBe(32)
    expect(bytesToHex(a)).toBe(bytesToHex(b))
  })
  it('separates method and memberId (no concatenation ambiguity)', () => {
    const n = new Uint8Array([1])
    const p = new Uint8Array([2])
    const a = memberAuthDigest('m0', 'B5', p, 1n, n)
    const b = memberAuthDigest('m', '0B5', p, 1n, n)
    expect(bytesToHex(a)).not.toBe(bytesToHex(b))
  })
})

describe('envelope digest + metaHash', () => {
  it('eMPTY_META_HASH == sha256(nil) = e3b0c4...', () => {
    expect(bytesToHex(EMPTY_META_HASH))
      .toBe('e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855')
  })
  it('envelope digest is 32 bytes and field-sensitive', () => {
    const env = {
      version: 1,
      requestId: '0123456789abcdef0123456789abcdef',
      groupId: 'wallet-coord-e2e',
      chain: 'eth',
      unsignedTx: new Uint8Array([1, 2, 3]),
      digest32: new Uint8Array(32).fill(7),
      proposer: 'deadbeef',
      createdAt: 1747569600000,
      expiry: 1747573200000,
      metaHash: EMPTY_META_HASH,
    }
    const d = envelopeDigest(env)
    expect(d.length).toBe(32)
    expect(bytesToHex(envelopeDigest({ ...env, chain: 'bsc' }))).not.toBe(bytesToHex(d))
    expect(bytesToHex(envelopeDigest({ ...env, expiry: env.expiry + 1 }))).not.toBe(bytesToHex(d))
  })
})
