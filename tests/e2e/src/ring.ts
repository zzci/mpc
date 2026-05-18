import type { MemberKey } from './lib/coord-client.ts'
import type { DeviceResult } from './lib/member-harness.ts'
/**
 * Full-ring orchestration (docs/design/testing.md §3.1). Composes the libs into
 * the two paired test replicas bound by the pinned real ETH digest:
 *   Phase 1 — real relay-transported member MPC (cli member subprocesses).
 *   Phase 2 — coord envelope ring driven against the live node, with
 *             mock-extsvc as the external business service.
 * Assertions live in the tests; this module returns structured results.
 */
import { Buffer } from 'node:buffer'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { bytesToHex, hexToBytes } from './lib/bytes.ts'
import { ExternalClient, MemberClient } from './lib/coord-client.ts'
import { ethAddress, pubCompressed, tronAddress } from './lib/crypto.ts'
import { buildBinary, repoRoot } from './lib/go-build.ts'
import { runMembers } from './lib/member-harness.ts'
import { startNode } from './lib/node-process.ts'
import { EIP155_DIGEST, EIP155_RLP, HARNESS_PSK_HEX } from './lib/vectors.ts'

const N = 3
const TSS_THRESHOLD = 1 // t=1 → 2-of-3
const SIGNERS = [0, 1]
const RELAY_GROUP_ID = 'wallet-e2e'
const COORD_GROUP_ID = 'wallet-coord-e2e'

function randPriv(): Uint8Array {
  // 32 random bytes is in [1, n-1] with overwhelming probability;
  // getPublicKey() validates the scalar and throws otherwise.
  return crypto.getRandomValues(new Uint8Array(32))
}

export interface Phase1Result {
  masterPubUncompressed: Uint8Array
  members: DeviceResult[]
  /** the participating signer's DeviceResult (carries R/S/V). */
  signer: DeviceResult
}

export interface RingContext {
  workdir: string
  nodeBin: string
  cliBin: string
  groupKeyPriv: Uint8Array
  groupKeyPubCompressed: Uint8Array
  /** coord-side B1 member identities (m0..m2). */
  coordMembers: MemberKey[]
  node: Awaited<ReturnType<typeof startNode>>
}

/** Build binaries + start node (relay+coord+admin, unlocked). */
export async function setupRing(): Promise<RingContext> {
  const root = repoRoot()
  const workdir = mkdtempSync(join(tmpdir(), 'e2e-ring-'))
  const nodeBin = join(workdir, 'node')
  const cliBin = join(workdir, 'cli')
  await buildBinary(root, './cmd/node', nodeBin)
  await buildBinary(root, './cmd/cli', cliBin)

  const groupKeyPriv = randPriv()
  const groupKeyPubCompressed = pubCompressed(groupKeyPriv)

  const coordMembers: MemberKey[] = []
  for (let i = 0; i < N; i++) {
    const priv = randPriv()
    coordMembers.push({ memberId: `m${i}`, priv, pub: pubCompressed(priv) })
  }

  const node = await startNode({
    nodeBin,
    groupPubB64: Buffer.from(groupKeyPubCompressed).toString('base64'),
    pskHex: HARNESS_PSK_HEX,
  })

  return { workdir, nodeBin, cliBin, groupKeyPriv, groupKeyPubCompressed, coordMembers, node }
}

/** Phase 1: relay-transported 2-of-3 keygen/sign(anchor)/reshare. */
export async function runPhase1(ctx: RingContext): Promise<Phase1Result> {
  const memberKeyHexByIndex: string[] = []
  for (let i = 0; i < N; i++)
    memberKeyHexByIndex.push(bytesToHex(randPriv()))

  const members = await runMembers({
    cliBin: ctx.cliBin,
    workdir: ctx.workdir,
    relayPeerId: ctx.node.relayPeerId,
    relayAddrs: ctx.node.relayAddrs,
    pskHex: HARNESS_PSK_HEX,
    groupKeyHex: bytesToHex(ctx.groupKeyPriv),
    groupId: RELAY_GROUP_ID,
    n: N,
    threshold: TSS_THRESHOLD,
    signers: SIGNERS,
    digestHex: EIP155_DIGEST,
    memberKeyHexByIndex,
    timeoutMs: 8 * 60_000,
  })

  for (const m of members) {
    if (m.err !== '')
      throw new Error(`member ${m.index} failed: ${m.err}`)
    if (!m.allViaRelay)
      throw new Error(`member ${m.index} had a non-relay connection (zero-trust relay violated)`)
  }
  const masterHex = members[0]!.groupPubHex
  for (const m of members) {
    if (m.groupPubHex !== masterHex)
      throw new Error(`member ${m.index} keygen pubkey mismatch`)
    if (m.resharedPubHex !== masterHex)
      throw new Error(`member ${m.index} reshare changed the master key`)
  }
  const signer = members.find(m => m.signed)
  if (!signer)
    throw new Error('no signer produced a signature')

  return { masterPubUncompressed: hexToBytes(masterHex), members, signer }
}

/** Provision the coord group with the Phase-1 master key (S-002 canonical). */
export async function provision(ctx: RingContext, masterPubUncompressed: Uint8Array): Promise<void> {
  const ext = new ExternalClient(ctx.node.coordBaseUrl, ctx.node.apiKey)
  await ext.provisionGroup(
    {
      version: 1,
      groupId: COORD_GROUP_ID,
      ecdsaPubkey: masterPubUncompressed,
      groupPubkey: ctx.groupKeyPubCompressed,
      thresholdT: 2,
      partiesN: N,
      members: ctx.coordMembers.map(m => ({ memberId: m.memberId, identityPubkey: m.pub })),
      createdAt: new Date().toISOString(),
    },
    ctx.groupKeyPriv,
    ctx.coordMembers.slice(0, 2),
  )
}

/** Expected derived chain addresses for the master key (assertion helper). */
export function expectedAddresses(masterPubUncompressed: Uint8Array): { evm: string, tron: string } {
  return {
    evm: ethAddress(masterPubUncompressed),
    tron: tronAddress(masterPubUncompressed),
  }
}

export const ANCHOR = { digestHex: EIP155_DIGEST, rlpHex: EIP155_RLP }
/** 16-byte requestId as 32-hex (contract.go uuidBytes accepts 32-hex). */
export const REQUEST_ID = '7265712d636c692d6532652d30303031'
export { COORD_GROUP_ID, MemberClient, N, SIGNERS }
