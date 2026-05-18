import type { ResultPayload } from './contract/index.ts'
import { RSV_LEN } from './contract/index.ts'
import { fromB64 } from './shared/bytes.ts'

// Terminal states of api.md A4 / the coord state machine.
export const TERMINAL = new Set(['RETURNED', 'EXPIRED', 'REJECTED', 'FAILED'])

export function isTerminal(status: string): boolean {
  return TERMINAL.has(status.toUpperCase())
}

/**
 * Normalize the A3/A4 result shapes to the canonical 65-byte
 * [V+27 || R(32) || S(32)] compact form (coord.verifyRSV layout). Accepts
 * either a single base64 `RSV`/`rsv` field (already compact) or a structured
 * `result {R,S,V}` (R,S base64 32-byte, V raw recovery id {0..3}). Returns
 * null when the terminal state carries no signature (EXPIRED/REJECTED/FAILED).
 */
export function extractRSV(p: ResultPayload): Uint8Array | null {
  const compactB64 = p.RSV ?? p.rsv
  if (compactB64) {
    const b = fromB64(compactB64)
    if (b.length !== RSV_LEN)
      throw new Error(`coord RSV must be ${RSV_LEN} bytes, got ${b.length}`)
    return b
  }
  if (p.result) {
    const r = fromB64(p.result.R)
    const s = fromB64(p.result.S)
    if (r.length !== 32 || s.length !== 32)
      throw new Error('coord result R/S must be 32 bytes each')
    if (p.result.V < 0 || p.result.V > 3)
      throw new Error(`coord result V out of range: ${p.result.V}`)
    const out = new Uint8Array(RSV_LEN)
    out[0] = p.result.V + 27
    out.set(r, 1)
    out.set(s, 33)
    return out
  }
  return null
}
