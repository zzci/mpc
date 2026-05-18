import type { Config } from '../src/config.ts'
import { secp256k1 } from '@noble/curves/secp256k1.js'
import { describe, expect, it } from 'bun:test'
import { loadConfig } from '../src/config.ts'
import { recoverPubFromRSV } from '../src/contract/index.ts'
import { MockExtSvc } from '../src/extsvc.ts'
import { extractRSV, isTerminal } from '../src/result.ts'
import { fromB64, fromHex, toB64, toHex } from '../src/shared/bytes.ts'
import { verifyGroupRSV } from '../src/verify.ts'
import golden from '../testdata/golden.json'

// Simulate coord's signer: sign digest32 with the group key and emit the
// 65-byte [V+27 || R(32) || S(32)] compact form coord returns (api.md A4).
function coordRSV(privHex: string, digest32: Uint8Array): Uint8Array {
  const priv = fromHex(privHex)
  const compact = secp256k1.sign(digest32, priv, { prehash: false }) // 64 = r||s, low-S
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
    catch {
      // try next recid
    }
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

function cfg(over: Partial<Record<string, string>> = {}): Config {
  return loadConfig({
    COORD_BASE_URL: 'http://coord.local:8080',
    RESULT_MODE: 'longpoll',
    LONGPOLL_WAIT_S: '1',
    RESULT_DEADLINE_MS: '5000',
    ...over,
  })
}

describe('result helpers', () => {
  it('isTerminal recognizes the api.md A4 terminal set', () => {
    expect(isTerminal('returned')).toBe(true)
    expect(isTerminal('EXPIRED')).toBe(true)
    expect(isTerminal('PENDING')).toBe(false)
  })

  it('extractRSV accepts compact RSV and structured {R,S,V}', () => {
    const rsv = coordRSV(golden.group.privHex, fromHex(golden.group.rsv.digest32Hex))
    expect(extractRSV({ requestId: 'r', status: 'RETURNED', RSV: toB64(rsv) })?.length).toBe(65)
    const struct = extractRSV({
      requestId: 'r',
      status: 'RETURNED',
      result: { R: toB64(rsv.slice(1, 33)), S: toB64(rsv.slice(33, 65)), V: rsv[0]! - 27 },
    })
    expect(struct ? toHex(struct) : '').toBe(toHex(rsv))
  })

  it('extractRSV returns null on a signature-less terminal state', () => {
    expect(extractRSV({ requestId: 'r', status: 'EXPIRED' })).toBeNull()
  })
})

describe('verifyGroupRSV', () => {
  it('confirms a valid group signature across both chains and the pubkey', () => {
    const digest = fromHex(golden.group.rsv.digest32Hex)
    const rsv = fromB64(golden.group.rsv.rsvB64)
    const v = verifyGroupRSV(groupPublicBody, digest, rsv)
    expect(v.ok).toBe(true)
    expect(v.evmMatches && v.tronMatches && v.pubkeyMatches).toBe(true)
  })

  it('rejects an RSV that recovers a different key', () => {
    const digest = fromHex(golden.group.rsv.digest32Hex)
    const wrong = coordRSV(golden.proposer.privHex, digest) // proposer key, not group
    const v = verifyGroupRSV(groupPublicBody, digest, wrong)
    expect(v.ok).toBe(false)
  })

  it('matches a compressed ecdsa_pubkey form too', () => {
    const digest = fromHex(golden.group.rsv.digest32Hex)
    const rsv = fromB64(golden.group.rsv.rsvB64)
    const v = verifyGroupRSV(
      { ...groupPublicBody, ecdsa_pubkey: toB64(fromHex(golden.group.pubCompressedHex)) },
      digest,
      rsv,
    )
    expect(v.pubkeyMatches).toBe(true)
  })
})

describe('mockExtSvc.run end-to-end loop (mocked coord)', () => {
  function makeFetch(terminal: { status: string, withRSV: boolean }): typeof fetch {
    let submittedDigest: Uint8Array | undefined
    return (async (input: string | URL | Request, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/public'))
        return new Response(JSON.stringify(groupPublicBody), { status: 200 })
      if (url.endsWith('/v1/requests')) {
        const body = JSON.parse(String(init?.body)) as { digest32: string, requestId: string }
        submittedDigest = fromB64(body.digest32)
        return new Response(JSON.stringify({ requestId: body.requestId, status: 'PENDING' }), { status: 202 })
      }
      if (url.includes('/result')) {
        const payload: Record<string, unknown> = { requestId: 'x', status: terminal.status }
        if (terminal.withRSV && submittedDigest)
          payload.RSV = toB64(coordRSV(golden.group.privHex, submittedDigest))
        return new Response(JSON.stringify(payload), { status: 200 })
      }
      return new Response('not found', { status: 404 })
    }) as unknown as typeof fetch
  }

  const input = {
    groupId: 'grp-1',
    chain: 'ethereum',
    unsignedTx: new Uint8Array([1, 2, 3]),
    digest32: fromHex(golden.group.rsv.digest32Hex),
    proposer: 'finance-bot',
    expiry: Date.now() + 60_000,
    businessInfo: { title: 'Payout' },
  }
  const proposerPriv = fromHex(golden.proposer.privHex)

  it('RETURNED → independently verifies the {R,S,V} against A1 addresses', async () => {
    const svc = new MockExtSvc(cfg(), makeFetch({ status: 'RETURNED', withRSV: true }))
    const out = await svc.run(input, proposerPriv)
    svc.close()
    expect(out.status).toBe('RETURNED')
    expect(out.verification?.ok).toBe(true)
  })

  it('EXPIRED → outcome carries EXPIRED, no verification (testing.md §3.2)', async () => {
    const svc = new MockExtSvc(cfg(), makeFetch({ status: 'EXPIRED', withRSV: false }))
    const out = await svc.run(input, proposerPriv)
    svc.close()
    expect(out.status).toBe('EXPIRED')
    expect(out.verification).toBeUndefined()
  })
})

describe('webhook delivery mode', () => {
  it('resolves the waiter when coord posts the terminal callback', async () => {
    const svc = new MockExtSvc(
      cfg({ RESULT_MODE: 'webhook', WEBHOOK_HOST: '127.0.0.1', WEBHOOK_PORT: '0' }),
      (async () => new Response('unused', { status: 404 })) as unknown as typeof fetch,
    )
    const cbURL = svc.callbackURL
    expect(cbURL).toBeDefined()
    const rsv = coordRSV(golden.group.privHex, fromHex(golden.group.rsv.digest32Hex))
    const post = await fetch(cbURL!, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ requestId: 'wh-1', status: 'RETURNED', RSV: toB64(rsv) }),
    })
    expect(post.status).toBe(204)
    svc.close()
  })

  it('webhook waiter rejects on deadline', async () => {
    // group + submit succeed; coord never posts the callback → deadline.
    const noCallbackFetch = (async (input: string | URL | Request, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/public'))
        return new Response(JSON.stringify(groupPublicBody), { status: 200 })
      const body = JSON.parse(String(init?.body)) as { requestId: string }
      return new Response(JSON.stringify({ requestId: body.requestId, status: 'PENDING' }), { status: 202 })
    }) as unknown as typeof fetch
    const svc = new MockExtSvc(
      cfg({ RESULT_MODE: 'webhook', WEBHOOK_PORT: '0', RESULT_DEADLINE_MS: '50' }),
      noCallbackFetch,
    )
    await expect(
      svc.run(
        { groupId: 'grp-1', chain: 'ethereum', unsignedTx: new Uint8Array([1]), digest32: new Uint8Array(32), proposer: 'p', expiry: Date.now() + 60_000 },
        fromHex(golden.proposer.privHex),
      ),
    ).rejects.toThrow(/webhook timeout/)
    svc.close()
  })
})

describe('coordClient.getStatus (A3)', () => {
  it('parses the A3 status payload', async () => {
    const { CoordClient } = await import('../src/coord-client.ts')
    const c = new CoordClient(
      cfg(),
      (async () => new Response(JSON.stringify({ requestId: 'r9', status: 'PENDING' }), { status: 200 })) as unknown as typeof fetch,
    )
    expect((await c.getStatus('r9')).status).toBe('PENDING')
  })
})
