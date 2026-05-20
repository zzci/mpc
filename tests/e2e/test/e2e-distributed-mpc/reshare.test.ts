/**
 * DM-6 §G(3) reshare acceptance.
 *
 *   - reshare-3-to-3-rotate (mandatory): old shares wiped from each
 *     member's keystore; chaincode invariant; new shares hold;
 *   - reshare-3-to-4 (optional first phase): a 4th member admitted
 *     iff in the strict expected_members set; rejected otherwise.
 *
 * Live-run gate: DM-5 (PC CLI host_transport) must be merged.
 */
import { describe, expect, it } from 'bun:test'
import { realMpcGate } from './gate.ts'

const gate = await realMpcGate()

describe('DM-6 §G(3) reshare (impl §B DM-6)', () => {
  it.skipIf(gate.ok)(`skipped (deps unmet): ${gate.reason || 'n/a'}`, () => {
    expect(gate.ok).toBe(false)
  })

  it.skipIf(!gate.ok)('reshare-3-to-3-rotate: old shares wiped, chaincode invariant', async () => {
    // Implementation arrives with DM-5: drive a B10 reshare against
    // the same expected_members, then assert every member's keystore
    // holds a NEW share (bytes differ from pre-reshare) and the
    // coord-stored chaincode is unchanged.
    throw new Error('DM-5 host_transport required for live reshare-3-to-3-rotate (see ./gate.ts)')
  }, 10 * 60_000)

  it.skipIf(!gate.ok)('reshare-3-to-4: new member rejected when absent from expected_members', async () => {
    // Implementation arrives with DM-5: try to admit a 4th member
    // whose identity is NOT in expected_members; coord must refuse
    // with 409 EXPECTED_MEMBER_MISMATCH.
    throw new Error('DM-5 host_transport required for live reshare-3-to-4 admission test (see ./gate.ts)')
  }, 10 * 60_000)
})
