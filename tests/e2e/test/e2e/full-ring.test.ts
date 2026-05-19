/**
 * E2E-001 full ring (docs/design/testing.md §3.1): node(coord+relay) +
 * mock-extsvc + multi-process CLI members. Provision -> address -> envelope
 * -> quorum -> START -> relay MPC {R,S,V} -> coord group-pubkey verify ->
 * external RETURNED + verify. Gated on FIX-001+XA-001+MEXT-001 (live-run
 * serial per L1); skips with the precise unmet dependency otherwise.
 */
import { describe, expect, it } from 'bun:test'
import { b64ToBytes, bytesToHex, hexToBytes } from '../../src/lib/bytes.ts'
import { envelopeDigest } from '../../src/lib/canonical.ts'
import { ExternalClient } from '../../src/lib/coord-client.ts'
import { ecrecover, isLowS, keccak256, verifyDigestDER } from '../../src/lib/crypto.ts'
import { rsvCompact } from '../../src/lib/member-harness.ts'
import { locate, MockExtsvcClient, startMockExtsvc } from '../../src/lib/mock-extsvc.ts'
import {
  ANCHOR,
  COORD_GROUP_ID,
  expectedAddresses,
  MemberClient,
  provision,
  REQUEST_ID,
  runPhase1,
  setupRing,
} from '../../src/ring.ts'
import { liveGate } from './gate.ts'

const gate = await liveGate()

describe('full ring (testing.md §3.1)', () => {
  // Visibility placeholder: meaningful only when deps are UNMET (gate closed) —
  // it then runs and its name surfaces the skip reason. When the gate is OPEN
  // the real live test below runs, so this placeholder is skipped (no false fail).
  it.skipIf(gate.ok)(`skipped (deps unmet): ${gate.reason || 'n/a'}`, () => {
    expect(gate.ok).toBe(false)
  })

  it.skipIf(!gate.ok)('provision -> address -> envelope -> quorum -> MPC -> RETURNED', async () => {
    const ctx = await setupRing()
    let mock: Awaited<ReturnType<typeof startMockExtsvc>> | undefined
    try {
      // Phase 1 — real relay-transported 2-of-3 keygen/sign/reshare.
      const p1 = await runPhase1(ctx)
      const master = p1.masterPubUncompressed
      const digest = hexToBytes(ANCHOR.digestHex)

      // {R,S,V} over the real ETH digest must ecrecover to the master key.
      expect(bytesToHex(ecrecover(digest, p1.signer.sigRHex, p1.signer.sigSHex, p1.signer.sigV)))
        .toBe(bytesToHex(master))
      expect(isLowS(p1.signer.sigSHex)).toBe(true)

      // Phase 2 — provision the coord group with that master key.
      await provision(ctx, master)

      const loc = locate()
      mock = await startMockExtsvc(loc.dir!, ctx.server.coordBaseUrl, ctx.server.apiKey)
      const mockClient = new MockExtsvcClient(mock.baseUrl)

      // External service requests the group address (api.md A1 / XA-001).
      const exp = expectedAddresses(master)
      const addr = await mockClient.requestAddress(COORD_GROUP_ID)
      expect(addr.evmAddress).toBe(exp.evm)
      expect(addr.tronAddress).toBe(exp.tron)
      expect(bytesToHex(b64ToBytes(addr.ecdsaPubkeyB64))).toBe(bytesToHex(master))

      // External service submits the signing envelope (A2).
      const requestId = REQUEST_ID
      const submit = await mockClient.submit({
        groupId: COORD_GROUP_ID,
        chain: 'eth',
        requestId,
        digest32Hex: ANCHOR.digestHex,
        unsignedTxHex: ANCHOR.rlpHex,
        expiryMillis: Date.now() + 3_600_000,
      })
      if (submit.rejected)
        console.error('[E2E-DIAG] full-ring coord A2 rejected submit:', JSON.stringify(submit))
      expect(submit.rejected ?? false).toBe(false)

      // Members heartbeat + approve -> quorum.
      const members = new MemberClient(ctx.server.coordBaseUrl, COORD_GROUP_ID)
      for (const k of ctx.coordMembers.slice(0, 2)) {
        await members.heartbeat(k, `peer-${k.memberId}`)
        await members.decide(k, requestId, 'approved')
      }

      // B6 START: re-verify proposerSig + tx-decode binds digest32.
      const start = await members.waitForStart(
        ctx.coordMembers[0]!,
        requestId,
        Date.now() + 30_000,
      )
      const env = start.envelope
      const envDigest = envelopeDigest({
        version: env.version,
        requestId: env.requestId,
        groupId: env.groupId,
        chain: env.chain,
        unsignedTx: b64ToBytes(env.unsignedTx),
        digest32: b64ToBytes(env.digest32),
        proposer: env.proposer,
        createdAt: env.createdAt,
        expiry: env.expiry,
        metaHash: b64ToBytes(env.metaHash),
      })
      expect(verifyDigestDER(hexToBytes(env.proposer), envDigest, b64ToBytes(env.proposerSig)))
        .toBe(true)
      // tx-decode: keccak256(unsignedTx) must equal digest32 == the anchor.
      expect(bytesToHex(keccak256(b64ToBytes(env.unsignedTx)))).toBe(ANCHOR.digestHex)
      expect(bytesToHex(b64ToBytes(env.digest32))).toBe(ANCHOR.digestHex)

      // B7: post the real relay-MPC {R,S,V}; coord verifies recover==master.
      await members.postResult(ctx.coordMembers[0]!, requestId, rsvCompact(p1.signer))

      // External service longpolls and gets RETURNED + verifies.
      const ext = new ExternalClient(ctx.server.coordBaseUrl, ctx.server.apiKey)
      const lp = await ext.resultLongpoll(requestId, 15)
      expect(lp.status).toBe('RETURNED')
      expect(lp.rsvB64).toBeDefined()
      expect(b64ToBytes(lp.rsvB64 ?? '').length).toBeGreaterThan(0)

      const got = await mockClient.result(requestId)
      expect(got.status).toBe('RETURNED')
      // recovered is the {R,S,V} 3-chain verification object (control contract):
      // the external service recovers the signer and confirms it matches the
      // group's master key + derived evm/tron addresses.
      expect(got.recovered?.ok).toBe(true)
      expect(got.recovered?.pubkeyMatches).toBe(true)
      expect(got.recovered?.evmMatches).toBe(true)
      expect(got.recovered?.tronMatches).toBe(true)
    }
    finally {
      await mock?.stop()
      await ctx.server.stop()
    }
  }, 15 * 60_000)
})
