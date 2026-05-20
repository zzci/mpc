/**
 * DM-6 §G(2) real n-process sign-2-of-3 acceptance.
 *
 * Asserts the design's "any t+1 signs / any ≤t fails (no degradation)"
 * property: 2 cooperating members produce a valid signature; a single
 * member alone times out without producing one.
 *
 * Live-run gate: DM-5 (PC CLI host_transport) must be merged.
 */
import { describe, expect, it } from 'bun:test'
import { realMpcGate } from './gate.ts'

const gate = await realMpcGate()

describe('DM-6 §G(2) sign-2-of-3 (impl §B DM-6)', () => {
  it.skipIf(gate.ok)(`skipped (deps unmet): ${gate.reason || 'n/a'}`, () => {
    expect(gate.ok).toBe(false)
  })

  it.skipIf(!gate.ok)('any t+1 cooperating members produce a valid {R,S,V}', async () => {
    // Implementation arrives with DM-5: spawn 2 cli member subprocesses
    // + DM-6-configured node, drive a sign request, assert the
    // resulting {R,S,V} ecrecovers to the group master key (low-S).
    throw new Error('DM-5 host_transport required for live sign-2-of-3 (see ./gate.ts)')
  }, 10 * 60_000)

  it.skipIf(!gate.ok)('a single member alone times out (no degradation)', async () => {
    // Implementation arrives with DM-5: spawn 1 cli member subprocess
    // + DM-6-configured node, drive a sign request, assert it times
    // out without producing a signature.
    throw new Error('DM-5 host_transport required for live degraded-signer test (see ./gate.ts)')
  }, 10 * 60_000)
})
