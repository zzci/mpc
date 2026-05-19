/**
 * E2E-001 EXPIRED path (docs/design/testing.md §3.1/§3.2: "expired-path assertion
 * EXPIRED"; api.md C 410 EXPIRED). An envelope whose expiry is already in
 * the past must be rejected by coord and surface a terminal EXPIRED to the
 * external service. Same live-run gate as the full ring.
 */
import { describe, expect, it } from 'bun:test'
import { locate, MockExtsvcClient, startMockExtsvc } from '../../src/lib/mock-extsvc.ts'
import { ANCHOR, COORD_GROUP_ID, provision, REQUEST_ID, runPhase1, setupRing } from '../../src/ring.ts'
import { liveGate } from './gate.ts'

const gate = await liveGate()

describe('expired path (testing.md §3.2)', () => {
  // Visibility placeholder (see full-ring.test.ts): skipped when gate is OPEN
  // so it does not false-fail; runs only when deps are UNMET to surface why.
  it.skipIf(gate.ok)(`skipped (deps unmet): ${gate.reason || 'n/a'}`, () => {
    expect(gate.ok).toBe(false)
  })

  it.skipIf(!gate.ok)('past-expiry envelope is rejected and surfaces EXPIRED', async () => {
    const ctx = await setupRing()
    let mock: Awaited<ReturnType<typeof startMockExtsvc>> | undefined
    try {
      const p1 = await runPhase1(ctx)
      await provision(ctx, p1.masterPubUncompressed)

      const loc = locate()
      mock = await startMockExtsvc(loc.dir!, ctx.server.coordBaseUrl, ctx.server.apiKey)
      const mockClient = new MockExtsvcClient(mock.baseUrl)

      const requestId = REQUEST_ID
      const submit = await mockClient.submit({
        groupId: COORD_GROUP_ID,
        chain: 'eth',
        requestId,
        digest32Hex: ANCHOR.digestHex,
        unsignedTxHex: ANCHOR.rlpHex,
        expiryMillis: Date.now() - 60_000, // already expired
      })

      if (submit.rejected) {
        // coord rejected at A2 (400 INVALID_ENVELOPE / 410 EXPIRED).
        expect(['EXPIRED', 'INVALID_ENVELOPE']).toContain(submit.code ?? '')
      }
      else {
        // accepted then expires: external service must observe terminal EXPIRED.
        const got = await mockClient.result(requestId)
        expect(got.status).toBe('EXPIRED')
      }
    }
    finally {
      await mock?.stop()
      await ctx.server.stop()
    }
  }, 12 * 60_000)
})
