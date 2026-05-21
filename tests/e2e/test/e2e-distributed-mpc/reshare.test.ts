/**
 * DM-6 §G(3) reshare acceptance.
 *
 *   - reshare-3-to-3-rotate: runPhase1 drives keygen → sign → reshare via
 *     the CLI-001 member harness; this test asserts the post-reshare
 *     group key is unchanged (master invariant) and every party emitted
 *     a fresh reshared pubkey field that matches the original master.
 *
 *   - reshare-3-to-4 admission (B10): direct coord driver — POST
 *     /v1/groups/{groupId}/reshare with a newMemberSet that contains an
 *     identity NOT in coord.external.expected_members; coord must
 *     refuse with 409 EXPECTED_MEMBER_MISMATCH (impl §B DM-4 errors.go).
 */
import { describe, expect, it } from 'bun:test'
import { bytesToB64, bytesToHex } from '../../src/lib/bytes.ts'
import { pubCompressed, signDigestDER } from '../../src/lib/crypto.ts'
import { runPhase1, setupRing } from '../../src/ring.ts'
import { realMpcGate } from './gate.ts'
import { DMPC_GROUP_ID, setupDmpcRing } from './lib/dmpc-ring.ts'
import { reshareDigest, reshareHeaders } from './lib/reshare-driver.ts'

const gate = await realMpcGate()

describe('DM-6 §G(3) reshare (impl §B DM-6)', () => {
  it.skipIf(gate.ok)(`skipped (deps unmet): ${gate.reason || 'n/a'}`, () => {
    expect(gate.ok).toBe(false)
  })

  it.skipIf(!gate.ok)('reshare-3-to-3-rotate: master pubkey invariant across rotation', async () => {
    const ctx = await setupRing()
    try {
      const p1 = await runPhase1(ctx)
      const master = bytesToHex(p1.masterPubUncompressed)

      // R1+R7 — every member emitted a resharedPubHex that matches the
      // original group master key (master invariant: reshare rotates the
      // SHARES, not the GROUP KEY). cli member binary populates
      // resharedPubHex only after a successful per-party reshare; mismatch
      // would mean the n-party rotation produced a different group key,
      // which is forbidden.
      for (const m of p1.members) {
        expect(m.resharedPubHex.length).toBe(130)
        expect(m.resharedPubHex).toBe(master)
      }

      // R5 — every member's connections to peers were established through
      // the relay (zero-trust transport for the reshare round too).
      for (const m of p1.members)
        expect(m.allViaRelay).toBe(true)
    }
    finally {
      await ctx.server.stop()
    }
  }, 10 * 60_000)

  it.skipIf(!gate.ok)('reshare-3-to-4: new member rejected when absent from expected_members (B10 → 409 EXPECTED_MEMBER_MISMATCH)', async () => {
    const ctx = await setupDmpcRing()
    try {
      // m0..m2 are in expected_members (set by setupDmpcRing). The
      // "alien" identity is brand new — never declared anywhere — so
      // adding it to newMemberSet must trip the expectedSubset check
      // in reshare.go before any signature is even verified.
      const alienPriv = crypto.getRandomValues(new Uint8Array(32))
      const alienPub = pubCompressed(alienPriv)

      const oldSet = ctx.members.map(m => m.pub) // [m0.pub, m1.pub, m2.pub]
      const newSet = [...oldSet, alienPub] // 4-of-4 candidate including alien

      const sessionID = `dmpc-§G3-3to4-${Date.now()}`
      const deadlineMs = BigInt(Date.now() + 60_000)
      const digest = reshareDigest(sessionID, oldSet, newSet, deadlineMs)

      // Proposer = m0 (in expected_members + active in group); m0 signs
      // oldMemberSig. Every member of newSet co-signs the same digest
      // (including the alien — coord rejects on the set membership
      // check BEFORE signature verification, so the alien signature is
      // valid as long as it's well-formed).
      const proposer = ctx.members[0]!
      const oldMemberSig = signDigestDER(proposer.priv, digest)
      const newMemberSigs: Uint8Array[] = []
      for (const k of ctx.members)
        newMemberSigs.push(signDigestDER(k.priv, digest))
      newMemberSigs.push(signDigestDER(alienPriv, digest))

      const body = {
        sessionID,
        oldMemberSig: bytesToB64(oldMemberSig),
        newMemberSet: newSet.map(p => bytesToHex(p)),
        newMemberSig: newMemberSigs.map(s => bytesToB64(s)),
        deadline: Number(deadlineMs),
      }
      const json = JSON.stringify(body)
      const r = await fetch(`${ctx.server.coordBaseUrl}/v1/groups/${DMPC_GROUP_ID}/reshare`, {
        method: 'POST',
        headers: reshareHeaders(proposer, DMPC_GROUP_ID, json),
        body: json,
      })
      const respText = await r.text()
      expect(r.status).toBe(409)
      expect(respText).toContain('EXPECTED_MEMBER_MISMATCH')
    }
    finally {
      await ctx.server.stop()
    }
  }, 5 * 60_000)
})
