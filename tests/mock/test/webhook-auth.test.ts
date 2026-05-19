import type { Config } from '../src/config.ts'
import { createHmac } from 'node:crypto'
import { describe, expect, it } from 'bun:test'
import { loadConfig } from '../src/config.ts'
import { verifyWebhookAuth } from '../src/webhook-auth.ts'

// Independent (cross-language) re-implementation of coord's signer, mirroring
// internal/server/coord/webhookauth.go, so a passing test proves the two
// sides agree and a forged/replayed POST is rejected (anti-forgery proof,
// user ruling 2026-05-19).
function coordSign(secret: string, ts: number, body: string): Headers {
  const v1 = createHmac('sha256', secret).update(`${ts}.${body}`).digest('hex')
  return new Headers({
    'X-MCP-Timestamp': String(ts),
    'X-MCP-Signature': `t=${ts},v1=${v1}`,
  })
}

const BASE = { COORD_BASE_URL: 'http://coord.local:8080' }

function sigCfg(extra: Record<string, string> = {}): Config {
  return loadConfig({ ...BASE, WEBHOOK_SECRET: 'topsecret', WEBHOOK_SKEW_S: '300', ...extra })
}

const NOW = 1_700_000_000
const BODY = JSON.stringify({ requestId: 'r1', status: 'RETURNED', rsv: 'AAAA' })

describe('webhook-auth signature mode', () => {
  it('accepts a correctly signed, in-window callback', () => {
    const r = verifyWebhookAuth(sigCfg(), coordSign('topsecret', NOW, BODY), BODY, NOW)
    expect(r).toEqual({ ok: true, mode: 'signature' })
  })

  it('accepts within the skew window (both directions)', () => {
    const cfg = sigCfg()
    expect(verifyWebhookAuth(cfg, coordSign('topsecret', NOW - 299, BODY), BODY, NOW).ok).toBe(true)
    expect(verifyWebhookAuth(cfg, coordSign('topsecret', NOW + 299, BODY), BODY, NOW).ok).toBe(true)
  })

  it('rejects a forged signature (wrong secret)', () => {
    const r = verifyWebhookAuth(sigCfg(), coordSign('attacker', NOW, BODY), BODY, NOW)
    expect(r.ok).toBe(false)
  })

  it('rejects a tampered body (signed for a different payload)', () => {
    const forged = JSON.stringify({ requestId: 'r1', status: 'RETURNED', rsv: 'EVIL' })
    const r = verifyWebhookAuth(sigCfg(), coordSign('topsecret', NOW, BODY), forged, NOW)
    expect(r).toMatchObject({ ok: false })
  })

  it('rejects a stale timestamp (replay) and a far-future timestamp', () => {
    const cfg = sigCfg()
    expect(verifyWebhookAuth(cfg, coordSign('topsecret', NOW - 301, BODY), BODY, NOW).ok).toBe(false)
    expect(verifyWebhookAuth(cfg, coordSign('topsecret', NOW + 301, BODY), BODY, NOW).ok).toBe(false)
  })

  it('rejects missing / malformed signature headers', () => {
    const cfg = sigCfg()
    expect(verifyWebhookAuth(cfg, new Headers(), BODY, NOW).ok).toBe(false)
    expect(verifyWebhookAuth(cfg, new Headers({ 'X-MCP-Timestamp': String(NOW), 'X-MCP-Signature': 'garbage' }), BODY, NOW).ok).toBe(false)
  })

  it('rejects when signature t disagrees with the timestamp header', () => {
    const h = coordSign('topsecret', NOW, BODY)
    h.set('X-MCP-Timestamp', String(NOW + 1))
    expect(verifyWebhookAuth(sigCfg(), h, BODY, NOW).ok).toBe(false)
  })
})

describe('webhook-auth token mode', () => {
  function tokCfg(): Config {
    return loadConfig({ ...BASE, WEBHOOK_API_KEY: 'tok-123' })
  }

  it('accepts the exact Bearer token', () => {
    const r = verifyWebhookAuth(tokCfg(), new Headers({ Authorization: 'Bearer tok-123' }), BODY, NOW)
    expect(r).toEqual({ ok: true, mode: 'token' })
  })

  it('rejects a wrong or missing Bearer token', () => {
    expect(verifyWebhookAuth(tokCfg(), new Headers({ Authorization: 'Bearer nope' }), BODY, NOW).ok).toBe(false)
    expect(verifyWebhookAuth(tokCfg(), new Headers(), BODY, NOW).ok).toBe(false)
  })

  it('signature mode wins when both secret and api_key are set', () => {
    const cfg = sigCfg({ WEBHOOK_API_KEY: 'tok-123' })
    // A valid Bearer alone must NOT pass — secret forces signature mode.
    expect(verifyWebhookAuth(cfg, new Headers({ Authorization: 'Bearer tok-123' }), BODY, NOW).ok).toBe(false)
    expect(verifyWebhookAuth(cfg, coordSign('topsecret', NOW, BODY), BODY, NOW).ok).toBe(true)
  })
})

describe('webhook-auth disabled', () => {
  it('accepts when neither secret nor api_key is configured', () => {
    const r = verifyWebhookAuth(loadConfig(BASE), new Headers(), BODY, NOW)
    expect(r).toEqual({ ok: true, mode: 'disabled' })
  })
})
