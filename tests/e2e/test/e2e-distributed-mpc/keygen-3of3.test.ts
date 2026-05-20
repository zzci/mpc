/**
 * DM-6 §G(1) real n-process keygen-3-of-3 acceptance.
 *
 * Asserts the design's hard "truly distributed" property: 3 independent processes
 * each end up holding exactly ONE share, the coord library and logs hold
 * NONE, and the relay sees only Noise ciphertext.
 *
 * Live-run gate: DM-5 (PC CLI host_transport) must be merged. Until
 * then the test skips with the precise unmet dependency (live-run
 * serial per L1).
 */
import { describe, expect, it } from 'bun:test'
import { realMpcGate } from './gate.ts'

const gate = await realMpcGate()

describe('DM-6 §G(1) keygen-3-of-3 (impl §B DM-6)', () => {
  it.skipIf(gate.ok)(`skipped (deps unmet): ${gate.reason || 'n/a'}`, () => {
    expect(gate.ok).toBe(false)
  })

  it.skipIf(!gate.ok)('3 processes, each keystore holds exactly 1 share; coord+relay hold none', async () => {
    // Implementation arrives with DM-5: spawn 3 cli member subprocesses
    // (each with its own keystore directory) + a DM-6-configured node
    // (relay+coord), drive a B9 keygen, drive the per-member B11
    // attestation, then assert:
    //   - each member keystore enumerates exactly 1 share field,
    //   - coord library + logs hold no share / PreParams pattern,
    //   - relay log shows only Noise-ciphertext frames.
    throw new Error('DM-5 host_transport required for live keygen-3-of-3 (see ./gate.ts)')
  }, 10 * 60_000)
})
