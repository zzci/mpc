import type { MemberKey } from '../../../src/lib/coord-client.ts'
import type { DmpcServerHandle } from './server-process-dmpc.ts'
/**
 * Self-contained ring setup for the DM-6 §G attestation-quorum live
 * test. Builds the `node` server binary, spawns a node with
 * `coord.external.expected_members` populated for the test group,
 * provisions the group via S-002, and exposes a typed handle so the
 * tests can drive B11 attestations directly. Built atop the parent
 * suite's coord-client and crypto helpers — only the bits the parent
 * harness doesn't already cover live here.
 */
import { Buffer } from 'node:buffer'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { bytesToB64, bytesToHex, utf8Bytes } from '../../../src/lib/bytes.ts'
import { memberAuthDigest, memberAuthParams } from '../../../src/lib/canonical.ts'
import { ExternalClient } from '../../../src/lib/coord-client.ts'
import { pubCompressed, pubUncompressed, signDigestDER } from '../../../src/lib/crypto.ts'

// --- canonical preimage helpers (delegated to parent suite) -------------

import { buildBinary, repoRoot } from '../../../src/lib/go-build.ts'
import { HARNESS_PSK_HEX } from '../../../src/lib/vectors.ts'
import { startDmpcServer } from './server-process-dmpc.ts'

export const DMPC_GROUP_ID = 'dmpc-attest-e2e'
export const DMPC_N = 3
export const DMPC_THRESHOLD = 2

export interface DmpcRingContext {
  workdir: string
  serverBin: string
  /**
   * master/ECDSA key used as the S-002 ecdsaPubkey (also the value
   *  members "claim" in their attestation when matching the stored
   *  pubkey).
   */
  masterPriv: Uint8Array
  masterPubUncompressed: Uint8Array
  /** cap-token (group_pubkey) key. */
  groupKeyPriv: Uint8Array
  groupKeyPubCompressed: Uint8Array
  /**
   * member identities (m0..mN-1); both X-Member-* signing key and B11
   *  identityPubkey use these.
   */
  members: MemberKey[]
  server: DmpcServerHandle
}

function randPriv(): Uint8Array {
  return crypto.getRandomValues(new Uint8Array(32))
}

/**
 * Build node binary, spawn a DMPC node with expected_members
 *  configured for the test group, and S-002-provision the group with
 *  the master ECDSA pubkey + the n member identities.
 */
export async function setupDmpcRing(): Promise<DmpcRingContext> {
  const root = repoRoot()
  const workdir = mkdtempSync(join(tmpdir(), 'e2e-dmpc-'))
  const serverBin = join(workdir, 'server')
  await buildBinary(root, './cmd/server', serverBin)

  const masterPriv = randPriv()
  const masterPubUncompressed = pubUncompressed(masterPriv)
  const groupKeyPriv = randPriv()
  const groupKeyPubCompressed = pubCompressed(groupKeyPriv)

  const members: MemberKey[] = []
  for (let i = 0; i < DMPC_N; i++) {
    const priv = randPriv()
    members.push({ memberId: `m${i}`, priv, pub: pubCompressed(priv) })
  }

  const server = await startDmpcServer({
    serverBin,
    groupPubB64: Buffer.from(groupKeyPubCompressed).toString('base64'),
    pskHex: HARNESS_PSK_HEX,
    expectedMembers: {
      [DMPC_GROUP_ID]: members.map(m => bytesToHex(m.pub)),
    },
  })

  // S-002 provision: writes groups + group_members in one tx; groups
  // ends up with the master pubkey, so subsequent DM-6 quorum commits
  // are exercised on the IDEMPOTENT branch (alreadyCommitted=true on
  // pubkey match) or on the R7 branch (ErrR7Violation on pubkey
  // mismatch).
  const ext = new ExternalClient(server.coordBaseUrl, server.apiKey)
  await ext.provisionGroup(
    {
      version: 1,
      groupId: DMPC_GROUP_ID,
      ecdsaPubkey: masterPubUncompressed,
      groupPubkey: groupKeyPubCompressed,
      thresholdT: DMPC_THRESHOLD,
      partiesN: DMPC_N,
      members: members.map(m => ({ memberId: m.memberId, identityPubkey: m.pub })),
      createdAt: new Date().toISOString(),
    },
    groupKeyPriv,
    members.slice(0, DMPC_THRESHOLD),
  )

  return { workdir, serverBin, masterPriv, masterPubUncompressed, groupKeyPriv, groupKeyPubCompressed, members, server }
}

/**
 * A tiny B11 client — the parent suite's MemberClient covers
 *  heartbeat/decide/dispatch/postResult only; attestation lives here so
 *  DM-6 owns its surface end-to-end. Signs the standard B-side
 *  X-Member-* headers (preimage = memberAuthDigest of the body).
 */
export class AttestationClient {
  constructor(private readonly baseUrl: string, private readonly groupId: string) {}

  async put(key: MemberKey, body: object, headers: Record<string, string>): Promise<{ status: number, body: unknown }> {
    const json = JSON.stringify(body)
    const r = await fetch(`${this.baseUrl}/v1/groups/${this.groupId}/attestation`, {
      method: 'PUT',
      headers,
      body: json,
    })
    let parsed: unknown
    try {
      parsed = await r.json()
    }
    catch {
      parsed = await r.text()
    }
    return { status: r.status, body: parsed }
  }

  static signedHeaders(key: MemberKey, groupId: string, bodyJson: string): Record<string, string> {
    const ts = BigInt(Date.now())
    const nonce = crypto.getRandomValues(new Uint8Array(16))
    const params = canonicalParams('B11:attestation', groupId, bodyJson)
    const digest = memberAuthDigestLocal(key.memberId, 'B11:attestation', params, ts, nonce)
    const sig = signDigestDERLocal(key.priv, digest)
    return {
      'X-Member-Id': key.memberId,
      'X-Member-Ts': ts.toString(),
      'X-Member-Nonce': bytesToB64(nonce),
      'X-Member-Sig': bytesToB64(sig),
      'Content-Type': 'application/json',
    }
  }
}

function canonicalParams(method: string, groupId: string, bodyJson: string): Uint8Array {
  return memberAuthParams(method, groupId, utf8Bytes(bodyJson))
}

function memberAuthDigestLocal(
  memberId: string,
  method: string,
  params: Uint8Array,
  ts: bigint,
  nonce: Uint8Array,
): Uint8Array {
  return memberAuthDigest(memberId, method, params, ts, nonce)
}

function signDigestDERLocal(priv: Uint8Array, digest: Uint8Array): Uint8Array {
  return signDigestDER(priv, digest)
}
