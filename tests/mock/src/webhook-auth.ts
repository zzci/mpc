import type { Config } from './config.ts'
import { Buffer } from 'node:buffer'
import { createHmac, timingSafeEqual } from 'node:crypto'

// Verifier side of coord's dual-mode anti-forgery callback auth (user
// ruling 2026-05-19; docs/design/server/server.md change-summary item 4,
// docs/design/contract/api.md A4). This re-implements the wire format
// coord's internal/server/coord/webhookauth.go produces, independently and
// in another language — the cross-language E2E proof that a forged
// {requestId,status,RSV} POST is rejected.

export const TIMESTAMP_HEADER = 'x-mcp-timestamp'
export const SIGNATURE_HEADER = 'x-mcp-signature'

export type AuthOutcome
  = | { ok: true, mode: 'signature' | 'token' | 'disabled' }
    | { ok: false, reason: string }

/**
 * Constant-time string compare (length-safe — timingSafeEqual throws on
 * unequal lengths, so the length check both guards that and is itself not
 * a useful timing oracle for a hex digest / fixed token).
 */
function constantTimeEqual(a: string, b: string): boolean {
  const ab = Buffer.from(a)
  const bb = Buffer.from(b)
  if (ab.length !== bb.length)
    return false
  return timingSafeEqual(ab, bb)
}

/** Parse "t=<unix>,v1=<hex>" → { t, v1 } or null if malformed. */
function parseSignature(h: string): { t: string, v1: string } | null {
  const parts = new Map<string, string>()
  for (const seg of h.split(',')) {
    const i = seg.indexOf('=')
    if (i <= 0)
      return null
    parts.set(seg.slice(0, i).trim(), seg.slice(i + 1).trim())
  }
  const t = parts.get('t')
  const v1 = parts.get('v1')
  if (t === undefined || v1 === undefined || t === '' || v1 === '')
    return null
  return { t, v1 }
}

/**
 * Verify one inbound coord callback.
 *
 *   - WEBHOOK_SECRET set → signature mode: X-MCP-Timestamp + X-MCP-Signature
 *     required; the signature's `t` must match the timestamp header; the
 *     timestamp must be within ±WEBHOOK_SKEW_S of nowSec (reject replay /
 *     stale / future); HMAC-SHA256(secret, "<ts>.<rawBody>") must
 *     constant-time-equal the v1 hex (reject forgery / body tampering).
 *   - else WEBHOOK_API_KEY set → token mode: Authorization must
 *     constant-time-equal "Bearer <api_key>".
 *   - neither set → disabled (accept; the longpoll E2E never POSTs here).
 *
 * `headers` is a case-insensitive getter (Request.headers.get / Headers).
 */
export function verifyWebhookAuth(
  cfg: Config,
  headers: { get: (name: string) => string | null },
  rawBody: string,
  nowSec: number,
): AuthOutcome {
  if (cfg.WEBHOOK_SECRET !== undefined) {
    const tsHeader = headers.get(TIMESTAMP_HEADER)
    const sigHeader = headers.get(SIGNATURE_HEADER)
    if (tsHeader === null || tsHeader === '' || sigHeader === null || sigHeader === '')
      return { ok: false, reason: 'missing X-MCP-Timestamp / X-MCP-Signature' }

    const sig = parseSignature(sigHeader)
    if (sig === null)
      return { ok: false, reason: 'malformed X-MCP-Signature' }
    if (sig.t !== tsHeader)
      return { ok: false, reason: 'signature t does not match X-MCP-Timestamp' }

    const ts = Number(tsHeader)
    if (!Number.isInteger(ts))
      return { ok: false, reason: 'non-integer timestamp' }
    if (Math.abs(nowSec - ts) > cfg.WEBHOOK_SKEW_S)
      return { ok: false, reason: `timestamp outside ±${cfg.WEBHOOK_SKEW_S}s skew (replay/stale)` }

    const want = createHmac('sha256', cfg.WEBHOOK_SECRET)
      .update(`${tsHeader}.${rawBody}`)
      .digest('hex')
    if (!constantTimeEqual(sig.v1, want))
      return { ok: false, reason: 'HMAC mismatch (forged or tampered body)' }
    return { ok: true, mode: 'signature' }
  }

  if (cfg.WEBHOOK_API_KEY !== undefined) {
    const auth = headers.get('authorization')
    if (auth === null || auth === '')
      return { ok: false, reason: 'missing Authorization' }
    if (!constantTimeEqual(auth, `Bearer ${cfg.WEBHOOK_API_KEY}`))
      return { ok: false, reason: 'Bearer token mismatch' }
    return { ok: true, mode: 'token' }
  }

  return { ok: true, mode: 'disabled' }
}
