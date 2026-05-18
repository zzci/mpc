import type { Config } from '../src/config.ts'
import { secp256k1 } from '@noble/curves/secp256k1.js'
import { describe, expect, it } from 'bun:test'
import { loadConfig } from '../src/config.ts'
import { recoverPubFromRSV } from '../src/contract/index.ts'
import { ControlServer } from '../src/server.ts'
import { fromB64, fromHex, toB64, toHex } from '../src/shared/bytes.ts'
import golden from '../testdata/golden.json'

// Coord signer simulation (same layout coord.verifyRSV consumes).
function coordRSV(privHex: string, digest32: Uint8Array): Uint8Array {
  const priv = fromHex(privHex)
  const compact = secp256k1.sign(digest32, priv, { prehash: false })
  const r = compact.slice(0, 32)
  const s = compact.slice(32, 64)
  const want = toHex(secp256k1.getPublicKey(priv, false))
  for (let recid = 0; recid < 4; recid++) {
    const rsv = new Uint8Array(65)
    rsv[0] = recid + 27
    rsv.set(r, 1)
    rsv.set(s, 33)
    try {
      if (toHex(recoverPubFromRSV(digest32, rsv)) === want)
        return rsv
    }
    catch {}
  }
  throw new Error('no recovery id matched')
}

const groupPublicBody = {
  groupId: 'grp-1',
  ecdsa_pubkey: toB64(fromHex(golden.group.pubUncompressedHex)),
  evm_address: golden.group.evmAddress,
  tron_address: golden.group.tronAddress,
  threshold_t: 1,
  parties_n: 3,
}

function cfg(): Config {
  return loadConfig({
    COORD_BASE_URL: 'http://coord.local:8080',
    RESULT_DEADLINE_MS: '4000',
    LONGPOLL_WAIT_S: '1',
  })
}

// Stateful coord mock: serves A1, A2, then A4 longpoll with the terminal state.
function coordMock(terminal: { status: string, withRSV: boolean }): typeof fetch {
  const digests = new Map<string, Uint8Array>()
  return (async (input: string | URL | Request, init?: RequestInit) => {
    const u = String(input)
    if (u.endsWith('/public'))
      return new Response(JSON.stringify(groupPublicBody), { status: 200 })
    if (u.endsWith('/v1/requests')) {
      const body = JSON.parse(String(init?.body)) as { requestId: string, digest32: string }
      digests.set(body.requestId, fromB64(body.digest32))
      return new Response(JSON.stringify({ requestId: body.requestId, status: 'PENDING' }), { status: 202 })
    }
    if (u.includes('/result')) {
      const id = u.split('/v1/requests/')[1]!.split('/result')[0]!
      const payload: Record<string, unknown> = { requestId: id, status: terminal.status }
      if (terminal.withRSV)
        payload.RSV = toB64(coordRSV(golden.group.privHex, digests.get(id)!))
      return new Response(JSON.stringify(payload), { status: 200 })
    }
    return new Response('not found', { status: 404 })
  }) as unknown as typeof fetch
}

const submitBody = {
  groupId: 'grp-1',
  chain: 'ethereum',
  digest32Hex: golden.group.rsv.digest32Hex,
  unsignedTxHex: '0102030a',
  requestId: '123e4567-e89b-12d3-a456-426614174000',
  expiryMillis: Date.now() + 60_000,
}

async function pollResult(srv: ControlServer, id: string, tries = 40): Promise<{ status: string, recovered?: { ok: boolean } }> {
  for (let i = 0; i < tries; i++) {
    const r = await srv.handle(new Request(`http://x/control/result/${id}`))
    const body = await r.json() as { status: string, recovered?: { ok: boolean } }
    if (body.status !== 'PENDING')
      return body
    await new Promise(res => setTimeout(res, 25))
  }
  throw new Error('result stayed PENDING')
}

describe('controlServer', () => {
  it('GET /healthz → 200 {status:ok}', async () => {
    const s = new ControlServer(cfg(), coordMock({ status: 'RETURNED', withRSV: true }))
    const r = await s.handle(new Request('http://x/healthz'))
    expect(r.status).toBe(200)
    expect(await r.json()).toEqual({ status: 'ok' })
  })

  it('POST /control/request-address → {ecdsaPubkeyB64,evmAddress,tronAddress}', async () => {
    const s = new ControlServer(cfg(), coordMock({ status: 'RETURNED', withRSV: true }))
    const r = await s.handle(new Request('http://x/control/request-address', {
      method: 'POST',
      body: JSON.stringify({ groupId: 'grp-1' }),
    }))
    expect(r.status).toBe(200)
    expect(await r.json()).toEqual({
      ecdsaPubkeyB64: groupPublicBody.ecdsa_pubkey,
      evmAddress: golden.group.evmAddress,
      tronAddress: golden.group.tronAddress,
    })
  })

  it('POST /control/submit → 202 {requestId}; result resolves RETURNED + verified', async () => {
    const s = new ControlServer(cfg(), coordMock({ status: 'RETURNED', withRSV: true }))
    const r = await s.handle(new Request('http://x/control/submit', {
      method: 'POST',
      body: JSON.stringify(submitBody),
    }))
    expect(r.status).toBe(202)
    expect(await r.json()).toEqual({ requestId: submitBody.requestId })
    const final = await pollResult(s, submitBody.requestId)
    expect(final.status).toBe('RETURNED')
    expect(final.recovered?.ok).toBe(true)
  })

  it('EXPIRED terminal propagates with no recovered block', async () => {
    const s = new ControlServer(cfg(), coordMock({ status: 'EXPIRED', withRSV: false }))
    await s.handle(new Request('http://x/control/submit', {
      method: 'POST',
      body: JSON.stringify({ ...submitBody, requestId: '00000000-0000-0000-0000-000000000009' }),
    }))
    const final = await pollResult(s, '00000000-0000-0000-0000-000000000009')
    expect(final.status).toBe('EXPIRED')
    expect(final.recovered).toBeUndefined()
  })

  it('unknown requestId → 404', async () => {
    const s = new ControlServer(cfg(), coordMock({ status: 'RETURNED', withRSV: true }))
    const r = await s.handle(new Request('http://x/control/result/nope'))
    expect(r.status).toBe(404)
  })

  it('bad submit body → 400', async () => {
    const s = new ControlServer(cfg(), coordMock({ status: 'RETURNED', withRSV: true }))
    const r = await s.handle(new Request('http://x/control/submit', {
      method: 'POST',
      body: JSON.stringify({ groupId: 'g' }),
    }))
    expect(r.status).toBe(400)
  })

  it('unrouted path → 404', async () => {
    const s = new ControlServer(cfg(), coordMock({ status: 'RETURNED', withRSV: true }))
    const r = await s.handle(new Request('http://x/nope'))
    expect(r.status).toBe(404)
  })

  it('listen() binds and serves /healthz over HTTP, stop() releases', async () => {
    const s = new ControlServer(
      loadConfig({ COORD_BASE_URL: 'http://coord.local:8080', PORT: '0' }),
      coordMock({ status: 'RETURNED', withRSV: true }),
    )
    const { port } = s.listen()
    expect(port).toBeGreaterThan(0)
    const r = await fetch(`http://127.0.0.1:${port}/healthz`)
    expect(r.status).toBe(200)
    s.stop()
  })
})
