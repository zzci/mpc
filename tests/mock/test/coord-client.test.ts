import { describe, expect, it } from 'bun:test'
import { loadConfig } from '../src/config.ts'
import { CoordClient, CoordError, envelopeToA2JSON } from '../src/coord-client.ts'
import { fromB64, fromHex } from '../src/shared/bytes.ts'
import golden from '../testdata/golden.json'

const baseEnv = {
  COORD_BASE_URL: 'http://coord.local:8080',
  COORD_API_KEY: 'secret-key',
  COORD_API_KEY_HEADER: 'X-Api-Key',
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}

describe('envelopeToA2JSON', () => {
  it('serializes contract.SigningRequest JSON shape (b64 bytes, int64 ms times)', () => {
    const c = golden.withBusiness
    const body = envelopeToA2JSON({
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
      proposerSig: fromHex(c.proposerSigHex),
    })
    expect(body.createdAt).toBe(c.envelope.createdAt)
    expect(typeof body.unsignedTx).toBe('string')
    expect(body.businessInfo).toEqual(c.envelope.businessInfo)
    expect(body).not.toHaveProperty('proposerSig', undefined)
  })

  it('omits businessInfo when absent', () => {
    const c = golden.noBusiness
    const body = envelopeToA2JSON({
      version: 1,
      requestId: c.envelope.requestId,
      groupId: 'g',
      chain: 'ethereum',
      unsignedTx: new Uint8Array([1]),
      digest32: new Uint8Array(32),
      proposer: 'p',
      createdAt: 1,
      expiry: 2,
      businessInfo: null,
      metaHash: new Uint8Array(32),
      proposerSig: new Uint8Array(1),
    })
    expect(body).not.toHaveProperty('businessInfo')
  })
})

describe('coordClient A1/A2', () => {
  it('GET /v1/groups/{id}/public sends api key and parses GroupPublic', async () => {
    const cfg = loadConfig({ ...baseEnv })
    let seenUrl = ''
    let seenKey = ''
    const fetchImpl = (async (input: string | URL | Request, init?: RequestInit) => {
      seenUrl = String(input)
      seenKey = new Headers(init?.headers).get('X-Api-Key') ?? ''
      return jsonResponse({
        groupId: 'grp-1',
        ecdsa_pubkey: 'AA==',
        evm_address: golden.group.evmAddress,
        tron_address: golden.group.tronAddress,
        threshold_t: 1,
        parties_n: 3,
      })
    }) as unknown as typeof fetch
    const c = new CoordClient(cfg, fetchImpl)
    const g = await c.getGroupPublic('grp-1')
    expect(seenUrl).toBe('http://coord.local:8080/v1/groups/grp-1/public')
    expect(seenKey).toBe('secret-key')
    expect(g.evm_address).toBe(golden.group.evmAddress)
  })

  it('POST /v1/requests returns the accepted envelope', async () => {
    const cfg = loadConfig({ ...baseEnv })
    const fetchImpl = (async () =>
      jsonResponse({ requestId: 'r1', status: 'PENDING' }, 202)) as unknown as typeof fetch
    const c = new CoordClient(cfg, fetchImpl)
    const res = await c.submitRequest({
      version: 1,
      requestId: 'r1',
      groupId: 'g',
      chain: 'ethereum',
      unsignedTx: new Uint8Array([1]),
      digest32: new Uint8Array(32),
      proposer: 'p',
      createdAt: 1,
      expiry: 2,
      businessInfo: null,
      metaHash: new Uint8Array(32),
      proposerSig: new Uint8Array(1),
    })
    expect(res).toEqual({ requestId: 'r1', status: 'PENDING' })
  })

  it('maps api.md C error body to CoordError(status, code)', async () => {
    const cfg = loadConfig({ ...baseEnv })
    const fetchImpl = (async () =>
      jsonResponse({ error: { code: 'INVALID_ENVELOPE', message: 'bad sig' } }, 400)) as unknown as typeof fetch
    const c = new CoordClient(cfg, fetchImpl)
    try {
      await c.getGroupPublic('x')
      throw new Error('expected CoordError')
    }
    catch (e) {
      expect(e).toBeInstanceOf(CoordError)
      const ce = e as CoordError
      expect(ce.status).toBe(400)
      expect(ce.code).toBe('INVALID_ENVELOPE')
    }
  })
})
