import { readFileSync } from 'node:fs'
import { join } from 'node:path'
/**
 * E2E-002 — Docker isolated-VM E2E (docs/design/testing.md §3.3).
 *
 * The SAME §3.1 ring (provision -> A1 address -> A2 envelope -> quorum ->
 * START -> relay-MPC keygen/sign/reshare -> {R,S,V} -> coord group-pubkey
 * verify -> external RETURNED + 3-chain recover) but every simulated machine
 * is its own container on a real Docker bridge network — no 127.0.0.1
 * shortcut. Plus the EXPIRED path and the §3.3 isolation assertions:
 *   - real-network: members reach the relay only via `/dns4/server/...` (no
 *     loopback) and every device proves all-conns-via-relay across containers;
 *   - physical keystore isolation: each device's secret identity material is
 *     on its OWN private bind; the shared rendezvous volume carried only
 *     public peerIds/pubkeys + barrier markers — never a secret.
 *
 * §3.1's E2E-001 is untouched and stays GREEN; this is an additive harder
 * gate behind its own `E2E_DOCKER=1` switch (zero-regression red line).
 */
import { describe, expect, it } from 'bun:test'
import { b64ToBytes, bytesToHex, hexToBytes } from '../../src/lib/bytes.ts'
import { envelopeDigest } from '../../src/lib/canonical.ts'
import { ExternalClient, MemberClient } from '../../src/lib/coord-client.ts'
import { ecrecover, isLowS, keccak256, verifyDigestDER } from '../../src/lib/crypto.ts'
import {
  ANCHOR,
  COORD_GROUP_ID,
  expectedAddresses,
  inspectRz,
  N,
  provisionDocker,
  RELAY_ADDRS,
  REQUEST_ID,
  runDockerPhase1,
  setupDockerRing,
} from '../../src/lib/docker-ring.ts'
import { rsvCompact } from '../../src/lib/member-harness.ts'
import { MockExtsvcClient } from '../../src/lib/mock-extsvc.ts'
import { dockerLiveGate } from './docker-gate.ts'

const gate = await dockerLiveGate()

const RZ_FILE = /^(?:dev-\d+\.json|ready-(?:keygen|sign|reshare)-\d+|result-\d+\.json)$/
const DEV_KEYS = new Set(['index', 'peerId', 'memberPub'])

describe('docker isolated ring (testing.md §3.3)', () => {
  // Visibility placeholder (same pattern as test/e2e/full-ring.test.ts):
  // runs only when deps are UNMET to surface the precise skip reason; skipped
  // when the gate is OPEN so it never false-fails.
  it.skipIf(gate.ok)(`skipped (deps unmet): ${gate.reason || 'n/a'}`, () => {
    expect(gate.ok).toBe(false)
  })

  it.skipIf(!gate.ok)('cross-container full ring + EXPIRED + isolation', async () => {
    const ctx = await setupDockerRing()
    try {
      // --- Phase 1: relay-MPC across containers (real libp2p net) ---
      const p1 = await runDockerPhase1(ctx)
      const master = p1.masterPubUncompressed
      const digest = hexToBytes(ANCHOR.digestHex)

      expect(bytesToHex(ecrecover(digest, p1.signer.sigRHex, p1.signer.sigSHex, p1.signer.sigV)))
        .toBe(bytesToHex(master))
      expect(isLowS(p1.signer.sigSHex)).toBe(true)

      // --- Phase 2: coord envelope ring over the host-published ports ---
      await provisionDocker(ctx, master)
      const mockClient = new MockExtsvcClient(ctx.mockBaseUrl)

      const exp = expectedAddresses(master)
      const addr = await mockClient.requestAddress(COORD_GROUP_ID)
      expect(addr.evmAddress).toBe(exp.evm)
      expect(addr.tronAddress).toBe(exp.tron)
      expect(bytesToHex(b64ToBytes(addr.ecdsaPubkeyB64))).toBe(bytesToHex(master))

      const submit = await mockClient.submit({
        groupId: COORD_GROUP_ID,
        chain: 'eth',
        requestId: REQUEST_ID,
        digest32Hex: ANCHOR.digestHex,
        unsignedTxHex: ANCHOR.rlpHex,
        expiryMillis: Date.now() + 3_600_000,
      })
      expect(submit.rejected ?? false).toBe(false)

      const members = new MemberClient(ctx.coordBaseUrl, COORD_GROUP_ID)
      for (const k of ctx.coordMembers.slice(0, 2)) {
        await members.heartbeat(k, `peer-${k.memberId}`)
        await members.decide(k, REQUEST_ID, 'approved')
      }

      const start = await members.waitForStart(ctx.coordMembers[0]!, REQUEST_ID, Date.now() + 30_000)
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
      expect(verifyDigestDER(hexToBytes(env.proposer), envDigest, b64ToBytes(env.proposerSig))).toBe(true)
      expect(bytesToHex(keccak256(b64ToBytes(env.unsignedTx)))).toBe(ANCHOR.digestHex)
      expect(bytesToHex(b64ToBytes(env.digest32))).toBe(ANCHOR.digestHex)

      await members.postResult(ctx.coordMembers[0]!, REQUEST_ID, rsvCompact(p1.signer))

      const ext = new ExternalClient(ctx.coordBaseUrl, ctx.apiKey)
      const lp = await ext.resultLongpoll(REQUEST_ID, 15)
      expect(lp.status).toBe('RETURNED')
      expect(b64ToBytes(lp.rsvB64 ?? '').length).toBeGreaterThan(0)

      const got = await mockClient.result(REQUEST_ID)
      expect(got.status).toBe('RETURNED')
      expect(got.recovered?.ok).toBe(true)
      expect(got.recovered?.pubkeyMatches).toBe(true)
      expect(got.recovered?.evmMatches).toBe(true)
      expect(got.recovered?.tronMatches).toBe(true)

      // --- EXPIRED path (same isolated coord, fresh requestId) ---
      const expiredId = '7265712d646b722d6578702d30303032'
      const ex = await mockClient.submit({
        groupId: COORD_GROUP_ID,
        chain: 'eth',
        requestId: expiredId,
        digest32Hex: ANCHOR.digestHex,
        unsignedTxHex: ANCHOR.rlpHex,
        expiryMillis: Date.now() - 60_000,
      })
      if (ex.rejected)
        expect(['EXPIRED', 'INVALID_ENVELOPE']).toContain(ex.code ?? '')
      else
        expect((await mockClient.result(expiredId)).status).toBe('EXPIRED')

      // --- §3.3 isolation assertions ---
      // (1) real-network: relay reachable only via Docker DNS, never loopback.
      for (const a of RELAY_ADDRS) {
        expect(a.startsWith('/dns4/server/')).toBe(true)
        expect(a.includes('127.0.0.1') || a.includes('localhost')).toBe(false)
      }
      // every device proved all-MPC-conns-via-relay ACROSS containers.
      for (const m of p1.members)
        expect(m.allViaRelay).toBe(true)

      // (2) physical keystore isolation: each device's secret identity key
      // was delivered ONLY into its own container (`docker compose cp` ->
      // member<i>:/work, never any shared medium) and they are all distinct.
      const secrets = new Set<string>()
      for (let i = 0; i < N; i++) {
        const cfg = JSON.parse(readFileSync(join(ctx.ws, `m${i}.json`), 'utf8')) as { memberKeyHex: string }
        expect(cfg.memberKeyHex.length).toBe(64)
        secrets.add(cfg.memberKeyHex)
      }
      expect(secrets.size).toBe(N)
      expect(new Set(p1.members.map(m => m.index)).size).toBe(N)

      // (3) the SHARED rendezvous volume carried only public discovery data
      // (peerId + public memberPub), barrier markers and the public result —
      // never a secret (no config / memberKeyHex / groupKeyHex).
      const rz = await inspectRz(ctx)
      for (const f of rz.files)
        expect(RZ_FILE.test(f)).toBe(true)
      for (const e of rz.devEntries) {
        for (const k of Object.keys(e))
          expect(DEV_KEYS.has(k)).toBe(true)
      }
    }
    finally {
      await ctx.stop()
    }
  }, 30 * 60_000)
})
