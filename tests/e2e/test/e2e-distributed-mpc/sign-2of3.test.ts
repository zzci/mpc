/**
 * DM-6 §G(2) real n-process sign-2-of-3 acceptance.
 *
 * Asserts the design's "any t+1 cooperating members sign / fewer than t+1
 * cannot" property. The positive path drives runPhase1 (which signs the
 * pinned ETH digest with SIGNERS=[0,1]) and ecrecovers {R,S,V} against the
 * group master key. The degraded path runs only one member (signers=[0],
 * threshold still t=1 → 2-of-3) and asserts the sole nominal signer's
 * subprocess emits a non-empty error and signed=false.
 *
 * Both tests share one ring (built + started once per file in beforeAll)
 * so they cooperate on a single node and a single pair of compiled
 * binaries — keeping parallel pressure on the healthz probe minimal and
 * the whole §G suite runtime bounded.
 */
import { mkdirSync, mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterAll, beforeAll, describe, expect, it } from 'bun:test'
import { bytesToHex, hexToBytes } from '../../src/lib/bytes.ts'
import { ecrecover, isLowS } from '../../src/lib/crypto.ts'
import { runMembers } from '../../src/lib/member-harness.ts'
import { EIP155_DIGEST, HARNESS_PSK_HEX } from '../../src/lib/vectors.ts'
import { ANCHOR, runPhase1, setupRing } from '../../src/ring.ts'
import { realMpcGate } from './gate.ts'

const gate = await realMpcGate()

describe('DM-6 §G(2) sign-2-of-3 (impl §B DM-6)', () => {
  it.skipIf(gate.ok)(`skipped (deps unmet): ${gate.reason || 'n/a'}`, () => {
    expect(gate.ok).toBe(false)
  })

  // Single ring per file: the positive test runs runPhase1 (signers=[0,1]),
  // the degraded test reuses cliBin + relay/coord against a fresh runMembers
  // call with signers=[0]. Both share one set of compiled binaries.
  let ctx: Awaited<ReturnType<typeof setupRing>> | undefined
  beforeAll(async () => {
    if (gate.ok)
      ctx = await setupRing()
  }, 5 * 60_000)
  afterAll(async () => {
    if (ctx)
      await ctx.server.stop()
  })

  it.skipIf(!gate.ok)('any t+1 cooperating members produce a valid {R,S,V}', async () => {
    const p1 = await runPhase1(ctx!)
    const digest = hexToBytes(ANCHOR.digestHex)

    // Exactly one of the two designated signers carries the post-sign
    // {R,S,V}; the third member is a non-signer and must report
    // signed=false. Per cli/device.go, every DeviceResult records the
    // common group key but only signers actually attempt to sign.
    const signers = p1.members.filter(m => m.signed)
    const nonSigners = p1.members.filter(m => !m.signed)
    expect(signers.length).toBeGreaterThanOrEqual(1)
    expect(signers.length).toBeLessThanOrEqual(2)
    expect(nonSigners.length).toBeGreaterThanOrEqual(1)

    // The signer's {R,S,V} must ecrecover to the group master key and
    // be low-S (TSS canonicalisation, security.md).
    const s = p1.signer
    expect(s.sigRHex.length).toBe(64)
    expect(s.sigSHex.length).toBe(64)
    expect(isLowS(s.sigSHex)).toBe(true)
    const recovered = ecrecover(digest, s.sigRHex, s.sigSHex, s.sigV)
    expect(bytesToHex(recovered)).toBe(bytesToHex(p1.masterPubUncompressed))
  }, 10 * 60_000)

  it.skipIf(!gate.ok)('a single member alone cannot produce a signature (no degradation)', async () => {
    // Re-drive runMembers against the SAME node/cli binaries set up in
    // beforeAll, but tell the harness only m0 is a designated signer.
    // With n=3, t=1 (2-of-3) a sole signer cannot produce a valid
    // signature; cli/device.go aborts the sign phase with a TSS error
    // and the DeviceResult reports signed=false plus a non-empty err.
    const workdir = mkdtempSync(join(tmpdir(), 'dmpc-lone-'))
    mkdirSync(workdir, { recursive: true })

    const memberKeyHexByIndex: string[] = []
    for (let i = 0; i < 3; i++) {
      const priv = new Uint8Array(32)
      crypto.getRandomValues(priv)
      memberKeyHexByIndex.push(bytesToHex(priv))
    }

    const members = await runMembers({
      cliBin: ctx!.cliBin,
      workdir,
      relayPeerId: ctx!.server.relayPeerId,
      relayAddrs: ctx!.server.relayAddrs,
      pskHex: HARNESS_PSK_HEX,
      groupKeyHex: bytesToHex(ctx!.groupKeyPriv),
      groupId: 'wallet-e2e-lone',
      n: 3,
      threshold: 1,
      signers: [0], // under threshold — need 2 signers for 2-of-3
      digestHex: EIP155_DIGEST,
      memberKeyHexByIndex,
      timeoutMs: 8 * 60_000,
    })

    // The lone signer's sign phase must fail loudly (TSS aborts when the
    // committed key count < t+1) and emit signed=false. Non-signers
    // report signed=false too because they were never on the signer list.
    const m0 = members[0]!
    expect(m0.signed).toBe(false)
    expect(m0.err).not.toBe('')
    for (let i = 1; i < members.length; i++)
      expect(members[i]!.signed).toBe(false)
  }, 10 * 60_000)
})
