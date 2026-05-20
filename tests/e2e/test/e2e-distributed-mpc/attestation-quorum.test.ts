/**
 * DM-6 §G(4) attestation-consistency live test.
 *
 * Drives the B11 endpoint with synthesized attestations against a real
 * coord and asserts the DM-6 same-tx commit fires (idempotent path
 * when stored pubkey matches the attestation's pubkey, R7-refused
 * when they disagree).
 *
 * Live-run gate: this test depends only on Go toolchain + the DM-6
 * primitive (both in main); it does NOT require DM-5. The full
 * keygen/sign/reshare §G suite is gated separately on DM-5.
 */
import { describe, expect, it } from 'bun:test'
import { bytesToHex } from '../../src/lib/bytes.ts'
import { attestationOnlyGate } from './gate.ts'
import { buildAttestation } from './lib/attestation.ts'
import { AttestationClient, DMPC_GROUP_ID, setupDmpcRing } from './lib/dmpc-ring.ts'

const gate = await attestationOnlyGate()

describe('DM-6 §G(4) attestation quorum (impl §B DM-6)', () => {
  // Visibility placeholder: meaningful only when the gate is closed —
  // it surfaces the precise unmet dependency. Skipped when live.
  it.skipIf(gate.ok)(`skipped (deps unmet): ${gate.reason || 'n/a'}`, () => {
    expect(gate.ok).toBe(false)
  })

  it.skipIf(!gate.ok)('all n consistent + matching pubkey → idempotent commit (REGISTERED)', async () => {
    const ctx = await setupDmpcRing()
    try {
      const client = new AttestationClient(ctx.server.coordBaseUrl, DMPC_GROUP_ID)
      const chaincode = new Uint8Array(32).fill(0xCC)
      let lastBody: unknown
      for (let i = 0; i < ctx.members.length; i++) {
        const m = ctx.members[i]!
        const body = buildAttestation({
          groupId: DMPC_GROUP_ID,
          identityPubkey: m.pub,
          holdsShare: true,
          groupPubkey: ctx.masterPubUncompressed,
          chaincode,
          ts: Date.now() + i,
        }, m.priv)
        const json = JSON.stringify(body)
        const r = await client.put(m, body, AttestationClient.signedHeaders(m, DMPC_GROUP_ID, json))
        expect(r.status).toBe(200)
        lastBody = r.body
      }
      // After the n-th attestation, the orchestrator hit
      // CommitAttestationQuorum. Because S-002 already stored
      // ctx.masterPubUncompressed, the primitive returned
      // alreadyCommitted=true (no R7) and groupState computed to
      // REGISTERED.
      const finalBody = lastBody as { groupState?: string }
      expect(finalBody.groupState).toBe('REGISTERED')
    }
    finally {
      await ctx.server.stop()
    }
  }, 5 * 60_000)

  it.skipIf(!gate.ok)('all n consistent + different pubkey → 409 STATE_CONFLICT (R7) on the quorum-completing call', async () => {
    const ctx = await setupDmpcRing()
    try {
      const client = new AttestationClient(ctx.server.coordBaseUrl, DMPC_GROUP_ID)
      const chaincode = new Uint8Array(32).fill(0xDD)
      // Choose a pubkey that is NOT the S-002-stored one (random 65
      // bytes starting with 0x04 to look like an uncompressed point;
      // attestationDigest doesn't validate curve, so this is fine for
      // the canonical preimage). The DM-6 primitive sees a
      // pubkey-mismatch on the existing groups row and refuses with
      // ErrR7Violation → 409 STATE_CONFLICT.
      const conflicting = new Uint8Array(65)
      conflicting[0] = 0x04
      for (let i = 1; i < 65; i++) conflicting[i] = (i * 7) & 0xFF

      // First n-1 attestations populate the cache without triggering
      // a commit (quorum not yet reached).
      for (let i = 0; i < ctx.members.length - 1; i++) {
        const m = ctx.members[i]!
        const body = buildAttestation({
          groupId: DMPC_GROUP_ID,
          identityPubkey: m.pub,
          holdsShare: true,
          groupPubkey: conflicting,
          chaincode,
          ts: Date.now() + i,
        }, m.priv)
        const json = JSON.stringify(body)
        const r = await client.put(m, body, AttestationClient.signedHeaders(m, DMPC_GROUP_ID, json))
        expect(r.status).toBe(200)
      }
      // Final attestation triggers the commit attempt → R7 surfaces.
      const last = ctx.members.at(-1)!
      const lastBody = buildAttestation({
        groupId: DMPC_GROUP_ID,
        identityPubkey: last.pub,
        holdsShare: true,
        groupPubkey: conflicting,
        chaincode,
        ts: Date.now() + ctx.members.length,
      }, last.priv)
      const lastJson = JSON.stringify(lastBody)
      const r = await client.put(last, lastBody, AttestationClient.signedHeaders(last, DMPC_GROUP_ID, lastJson))
      expect(r.status).toBe(409)
      expect(JSON.stringify(r.body)).toContain('STATE_CONFLICT')
    }
    finally {
      await ctx.server.stop()
    }
  }, 5 * 60_000)

  it.skipIf(!gate.ok)('partial quorum + 1 disagreeing → no commit, groupState=INCONSISTENT', async () => {
    const ctx = await setupDmpcRing()
    try {
      const client = new AttestationClient(ctx.server.coordBaseUrl, DMPC_GROUP_ID)
      const cc = new Uint8Array(32).fill(0xAA)
      const altPub = new Uint8Array(65)
      altPub[0] = 0x04
      for (let i = 1; i < 65; i++) altPub[i] = (i * 11) & 0xFF

      // m0, m1 attest with the matching master pubkey.
      for (let i = 0; i < 2; i++) {
        const m = ctx.members[i]!
        const body = buildAttestation({
          groupId: DMPC_GROUP_ID,
          identityPubkey: m.pub,
          holdsShare: true,
          groupPubkey: ctx.masterPubUncompressed,
          chaincode: cc,
          ts: Date.now() + i,
        }, m.priv)
        const json = JSON.stringify(body)
        const r = await client.put(m, body, AttestationClient.signedHeaders(m, DMPC_GROUP_ID, json))
        expect(r.status).toBe(200)
      }
      // m2 disagrees. The orchestrator finds inconsistent attestations
      // → no commit attempt → no R7. groupState surfaces as INCONSISTENT.
      const m2 = ctx.members[2]!
      const body = buildAttestation({
        groupId: DMPC_GROUP_ID,
        identityPubkey: m2.pub,
        holdsShare: true,
        groupPubkey: altPub,
        chaincode: cc,
        ts: Date.now() + 10,
      }, m2.priv)
      const json = JSON.stringify(body)
      const r = await client.put(m2, body, AttestationClient.signedHeaders(m2, DMPC_GROUP_ID, json))
      expect(r.status).toBe(200)
      const respBody = r.body as { groupState?: string }
      expect(respBody.groupState).toBe('INCONSISTENT')
    }
    finally {
      await ctx.server.stop()
    }
  }, 5 * 60_000)
})

// silence unused-import warnings on bytesToHex (kept for future helpers)
void bytesToHex
