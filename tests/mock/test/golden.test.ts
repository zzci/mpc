import type { BusinessInfo, SigningRequest } from '../src/contract/index.ts'
import { describe, expect, it } from 'bun:test'
import {
  CANONICAL_DOMAIN,
  canonicalBytes,
  EMPTY_META_HASH,
  envelopeDigest,
  evmAddress,
  metaHash,
  recoverPubFromRSV,
  signDigest,
  signEnvelope,
  tronAddress,
  verifyDigest,
  verifyProposerSig,
} from '../src/contract/index.ts'
import { bytesEqual, fromB64, fromHex, toHex } from '../src/shared/bytes.ts'
import golden from '../testdata/golden.json'

// Cross-language byte-exactness anchor (MEXT-001 "逐字节一致"). golden.json is
// produced by the real Go internal/contract + internal/addr packages
// (testdata/gen/main.go). Every assertion here proves the TypeScript port
// reproduces Go's bytes exactly; a drift on either side fails CI.

interface GoldenCase {
  envelope: {
    version: number
    requestId: string
    groupId: string
    chain: string
    unsignedTxB64: string
    digest32Hex: string
    proposer: string
    createdAt: number
    expiry: number
    businessInfo: BusinessInfo | null
  }
  metaHashHex: string
  canonicalHex: string
  envelopeDigestHex: string
  proposerSigHex: string
}

function buildReq(c: GoldenCase): SigningRequest {
  return {
    version: c.envelope.version,
    requestId: c.envelope.requestId,
    groupId: c.envelope.groupId,
    chain: c.envelope.chain,
    unsignedTx: fromB64(c.envelope.unsignedTxB64),
    digest32: fromHex(c.envelope.digest32Hex),
    proposer: c.envelope.proposer,
    createdAt: c.envelope.createdAt,
    expiry: c.envelope.expiry,
    businessInfo: c.envelope.businessInfo,
    metaHash: fromHex(c.metaHashHex),
    proposerSig: new Uint8Array(0),
  }
}

it('domain prefix matches Go canonicalDomain', () => {
  expect(toHex(CANONICAL_DOMAIN)).toBe(golden.domainHex)
})

it('EMPTY_META_HASH matches Go contract.EmptyMetaHash', () => {
  expect(toHex(EMPTY_META_HASH)).toBe(golden.emptyMetaHashHex)
})

for (const key of ['withBusiness', 'noBusiness'] as const) {
  describe(`golden ${key}`, () => {
    const c = golden[key] as GoldenCase
    const req = buildReq(c)

    it('metaHash byte-exact', () => {
      expect(toHex(metaHash(c.envelope.businessInfo))).toBe(c.metaHashHex)
    })

    it('canonical preimage byte-exact', () => {
      expect(toHex(canonicalBytes(req))).toBe(c.canonicalHex)
    })

    it('envelope digest byte-exact', () => {
      expect(toHex(envelopeDigest(req))).toBe(c.envelopeDigestHex)
    })

    it('verifies the Go-produced proposerSig (cross-impl signature interop)', () => {
      const signed = { ...req, proposerSig: fromHex(c.proposerSigHex) }
      const pub = fromHex(golden.proposer.pubUncompressedHex)
      expect(() => verifyProposerSig(signed, pub)).not.toThrow()
      // also against the compressed key form
      expect(() => verifyProposerSig(signed, fromHex(golden.proposer.pubCompressedHex))).not.toThrow()
    })

    it('TS-signed envelope verifies under the same key (round trip)', () => {
      const priv = fromHex(golden.proposer.privHex)
      const signed = signEnvelope(priv, req)
      expect(() => verifyProposerSig(signed, fromHex(golden.proposer.pubUncompressedHex))).not.toThrow()
    })

    it('TS proposerSig is byte-identical to Go (RFC6979 deterministic)', () => {
      const priv = fromHex(golden.proposer.privHex)
      const sig = signDigest(priv, envelopeDigest(req))
      expect(toHex(sig)).toBe(c.proposerSigHex)
    })
  })
}

describe('golden RSV / group address', () => {
  it('derives the group EVM address from the group pubkey (addr.ETHAddress)', () => {
    expect(evmAddress(fromHex(golden.group.pubUncompressedHex))).toBe(golden.group.evmAddress)
  })

  it('derives the group TRON address (addr.TronAddress Base58Check)', () => {
    expect(tronAddress(fromHex(golden.group.pubUncompressedHex))).toBe(golden.group.tronAddress)
  })

  it('recovers the group pubkey from the {R,S,V} compact form (coord.verifyRSV)', () => {
    const digest = fromHex(golden.group.rsv.digest32Hex)
    const rsv = fromB64(golden.group.rsv.rsvB64)
    const rec = recoverPubFromRSV(digest, rsv)
    expect(toHex(rec)).toBe(golden.group.pubUncompressedHex)
    expect(bytesEqual(rec, fromHex(golden.group.pubUncompressedHex))).toBe(true)
  })

  it('recovered key derives back the A1 evm/tron addresses', () => {
    const digest = fromHex(golden.group.rsv.digest32Hex)
    const rec = recoverPubFromRSV(digest, fromB64(golden.group.rsv.rsvB64))
    expect(evmAddress(rec)).toBe(golden.group.evmAddress)
    expect(tronAddress(rec)).toBe(golden.group.tronAddress)
  })

  it('rejects a tampered RSV', () => {
    const digest = fromHex(golden.group.rsv.digest32Hex)
    const rsv = fromB64(golden.group.rsv.rsvB64)
    rsv[10] = (rsv[10] ?? 0) ^ 0xFF
    let ok = true
    try {
      const rec = recoverPubFromRSV(digest, rsv)
      ok = toHex(rec) === golden.group.pubUncompressedHex
    }
    catch {
      ok = false
    }
    expect(ok).toBe(false)
  })

  it('verifyDigest accepts/rejects via signDigest round trip', () => {
    const priv = fromHex(golden.group.privHex)
    const digest = fromHex(golden.group.rsv.digest32Hex)
    const sig = signDigest(priv, digest)
    expect(() => verifyDigest(fromHex(golden.group.pubUncompressedHex), digest, sig)).not.toThrow()
    const bad = new Uint8Array(digest)
    bad[0] = (bad[0] ?? 0) ^ 0x01
    expect(() => verifyDigest(fromHex(golden.group.pubUncompressedHex), bad, sig)).toThrow()
  })
})
