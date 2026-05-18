/**
 * E2E-002 container orchestration adapter (docs/design/testing.md §3.3).
 *
 * The Docker isolated-VM topology mirror of e2e/src/ring.ts: instead of local
 * 127.0.0.1 subprocesses it brings the SAME unmodified binaries up as
 * separate containers on a real Docker bridge network (no localhost
 * shortcut), then drives the §3.1 ring over Docker service DNS. Every
 * cryptographic/contract/MPC concern is delegated to the already finalized,
 * reused libs (coord-client, crypto, canonical, bytes, member-harness,
 * mock-extsvc, vectors) — this file only owns container lifecycle + wiring.
 * §3.1's local E2E-001 (ring.ts/node-process.ts/member-harness.ts) is
 * untouched and stays GREEN.
 *
 * Environment constraints handled here (the daemon is a sibling, not local):
 *   - NO host bind mounts (daemon fs namespace differs) → the node config and
 *     each member's PRIVATE secret config are delivered with `docker compose
 *     cp`; the shared rendezvous + public result live on a NAMED volume.
 *   - the harness cannot reach published ports → it attaches its own
 *     container to the compose network and speaks Docker service DNS, exactly
 *     like a wallet role would (still no 127.0.0.1 path for members).
 */
import type { MemberKey } from './coord-client.ts'
import type { DeviceResult } from './member-harness.ts'
import { Buffer } from 'node:buffer'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { hostname, tmpdir } from 'node:os'
import { basename, join } from 'node:path'
import process from 'node:process'
import { bytesToHex } from './bytes.ts'
import { ExternalClient } from './coord-client.ts'
import { ethAddress, pubCompressed, tronAddress } from './crypto.ts'
import { repoRoot } from './go-build.ts'
import { EIP155_DIGEST, EIP155_RLP, HARNESS_PSK_HEX } from './vectors.ts'

const N = 3
const TSS_THRESHOLD = 1 // t=1 -> 2-of-3
const SIGNERS = [0, 1]
const RELAY_GROUP_ID = 'wallet-e2e-docker'
const COORD_GROUP_ID = 'wallet-coord-e2e-docker'
/** Members reach the relay ONLY by Docker DNS — never a direct/loopback addr. */
const RELAY_ADDRS = ['/dns4/node/tcp/4001']
const COORD_URL = 'http://node:8080'
const MOCK_URL = 'http://mock-extsvc:4500'
const ALPINE = 'alpine:3.20'

function randPriv(): Uint8Array {
  return crypto.getRandomValues(new Uint8Array(32))
}

/** Mirrors e2e/src/lib/member-harness.ts DeviceConfig (cmd/cli member input). */
interface DeviceConfig {
  index: number
  n: number
  threshold: number
  groupId: string
  relayPeerId: string
  relayAddrs: string[]
  pskHex: string
  groupPubHex: string
  memberKeyHex: string
  groupKeyHex: string
  signers: number[]
  digestHex: string
  rendezvousDir: string
  resultPath: string
}

export interface DockerRingContext {
  ws: string
  project: string
  composeFile: string
  env: Record<string, string>
  coordBaseUrl: string
  mockBaseUrl: string
  apiKey: string
  relayPeerId: string
  network: string
  volume: string
  groupKeyPriv: Uint8Array
  groupKeyPubCompressed: Uint8Array
  coordMembers: MemberKey[]
  memberKeyHexByIndex: string[]
  stop: () => Promise<void>
}

export interface Phase1Result {
  masterPubUncompressed: Uint8Array
  members: DeviceResult[]
  signer: DeviceResult
}

interface RunResult {
  code: number
  stdout: string
  stderr: string
}

async function run(argv: string[], env: Record<string, string>, timeoutMs: number): Promise<RunResult> {
  const p = Bun.spawn(argv, {
    env: { ...process.env, ...env },
    stdout: 'pipe',
    stderr: 'pipe',
  })
  const timer = setTimeout(() => p.kill('SIGKILL'), timeoutMs)
  const code = await p.exited
  clearTimeout(timer)
  const stdout = await new Response(p.stdout as ReadableStream<Uint8Array>).text()
  const stderr = await new Response(p.stderr as ReadableStream<Uint8Array>).text()
  return { code, stdout, stderr }
}

interface Base { project: string, composeFile: string, env: Record<string, string> }

function composeArgs(b: Base, argv: string[]): string[] {
  return ['docker', 'compose', '-p', b.project, '-f', b.composeFile, ...argv]
}

async function compose(b: Base, argv: string[], timeoutMs: number): Promise<RunResult> {
  return run(composeArgs(b, argv), b.env, timeoutMs)
}

async function composeOK(b: Base, argv: string[], timeoutMs: number): Promise<void> {
  const r = await compose(b, argv, timeoutMs)
  if (r.code !== 0)
    throw new Error(`docker compose ${argv.join(' ')} failed (exit ${r.code}):\n${r.stdout}\n${r.stderr}`)
}

async function dockerOK(argv: string[], env: Record<string, string>, timeoutMs: number): Promise<string> {
  const r = await run(['docker', ...argv], env, timeoutMs)
  if (r.code !== 0)
    throw new Error(`docker ${argv.join(' ')} failed (exit ${r.code}): ${r.stderr}`)
  return r.stdout
}

const RELAY_STARTED = 'relay: started'

async function parseRelayPeer(b: Base, deadlineMs: number): Promise<string> {
  while (Date.now() < deadlineMs) {
    const r = await compose(b, ['logs', '--no-log-prefix', 'node'], 30_000)
    for (const line of `${r.stdout}\n${r.stderr}`.split('\n')) {
      const at = line.indexOf('{')
      if (at < 0 || !line.includes(RELAY_STARTED))
        continue
      try {
        const rec = JSON.parse(line.slice(at)) as { msg?: string, peer?: string }
        if (rec.msg === RELAY_STARTED && rec.peer != null && rec.peer !== '')
          return rec.peer
      }
      catch {
        // partial/non-JSON line; keep scanning
      }
    }
    await Bun.sleep(500)
  }
  throw new Error(`docker node: relay did not report "${RELAY_STARTED}" before deadline`)
}

async function waitHealthz(baseUrl: string, budgetMs: number): Promise<void> {
  const deadline = Date.now() + budgetMs
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${baseUrl}/healthz`)
      if (res.ok)
        return
    }
    catch {
      // not up / DNS not joined yet
    }
    await Bun.sleep(300)
  }
  throw new Error(`docker: ${baseUrl}/healthz not ready within ${budgetMs}ms`)
}

/** Resolve the compose-managed network / volume names by project label. */
async function byLabel(kind: 'network' | 'volume', project: string, env: Record<string, string>): Promise<string> {
  const out = await dockerOK(
    [kind, 'ls', '--filter', `label=com.docker.compose.project=${project}`, '--format', '{{.Name}}'],
    env,
    30_000,
  )
  const name = out.split('\n').map(s => s.trim()).filter(Boolean)[0]
  if (name === undefined)
    throw new Error(`docker: could not resolve compose ${kind} for project ${project}`)
  return name
}

/** Read the shared rendezvous volume (isolation assertion: secret-free). */
export async function inspectRz(ctx: DockerRingContext): Promise<{ files: string[], devEntries: Record<string, unknown>[] }> {
  const ls = await dockerOK(
    ['run', '--rm', '-v', `${ctx.volume}:/rz`, ALPINE, 'sh', '-c', 'ls -1 /rz'],
    ctx.env,
    60_000,
  )
  const files = ls.split('\n').map(s => s.trim()).filter(Boolean)
  const devEntries: Record<string, unknown>[] = []
  for (const f of files) {
    if (!f.startsWith('dev-') || !f.endsWith('.json'))
      continue
    const raw = await dockerOK(
      ['run', '--rm', '-v', `${ctx.volume}:/rz`, ALPINE, 'cat', `/rz/${f}`],
      ctx.env,
      60_000,
    )
    devEntries.push(JSON.parse(raw) as Record<string, unknown>)
  }
  return { files, devEntries }
}

/**
 * Build images, render the per-run node config, create the stack, deliver the
 * node config privately, start node, attach this harness to the compose
 * network, wait for coord/mock readiness and the relay peer id.
 */
export async function setupDockerRing(): Promise<DockerRingContext> {
  const root = repoRoot()
  const composeFile = join(root, 'tests', 'docker', 'docker-compose.yml')
  const ws = mkdtempSync(join(tmpdir(), 'e2e-docker-'))
  const project = `e2e002${basename(ws).toLowerCase().replace(/[^a-z0-9]/g, '')}`

  const groupKeyPriv = randPriv()
  const groupKeyPubCompressed = pubCompressed(groupKeyPriv)
  const groupPubB64 = Buffer.from(groupKeyPubCompressed).toString('base64')

  const coordMembers: MemberKey[] = []
  for (let i = 0; i < N; i++) {
    const priv = randPriv()
    coordMembers.push({ memberId: `m${i}`, priv, pub: pubCompressed(priv) })
  }
  const memberKeyHexByIndex: string[] = []
  for (let i = 0; i < N; i++)
    memberKeyHexByIndex.push(bytesToHex(randPriv()))

  const tmpl = readFileSync(join(root, 'tests', 'docker', 'node.yaml'), 'utf8')
  const rendered = tmpl
    .split('\n')
    .filter(l => !l.startsWith('#') && l.trim() !== '')
    .join('\n')
    .replace('__GROUP_PUB_B64__', groupPubB64)
  writeFileSync(join(ws, 'node.yaml'), `${rendered}\n`, { mode: 0o600 })

  const apiKey = 'ext-secret-e2e-docker'
  const env: Record<string, string> = {
    RELAY_PNET_PSK: HARNESS_PSK_HEX,
    COORD_API_KEY: apiKey,
    ADMIN_READ_TOKEN: 'e2e-docker-admin-read-0001',
    ADMIN_CONTROL_TOKEN: 'e2e-docker-admin-control-0002',
  }
  const base: Base = { project, composeFile, env }
  const self = hostname()

  let network = ''
  const stop = async (): Promise<void> => {
    try {
      if (network !== '')
        await run(['docker', 'network', 'disconnect', '-f', network, self], env, 30_000)
    }
    catch {
      // best effort
    }
    try {
      await composeOK(base, ['down', '-v', '--remove-orphans'], 180_000)
    }
    finally {
      rmSync(ws, { recursive: true, force: true })
    }
  }

  try {
    await composeOK(base, ['build'], 20 * 60_000)
    await composeOK(base, ['create'], 5 * 60_000)
    network = await byLabel('network', project, env)
    const volume = await byLabel('volume', project, env)

    await composeOK(base, ['cp', join(ws, 'node.yaml'), 'node:/node.yaml'], 60_000)
    await composeOK(base, ['start', 'node'], 60_000)

    // Attach the harness container to the compose network so service DNS
    // (node / mock-extsvc) resolves — the real cross-container path.
    await dockerOK(['network', 'connect', network, self], env, 30_000)

    await waitHealthz(COORD_URL, 120_000)
    const relayPeerId = await parseRelayPeer(base, Date.now() + 90_000)

    await composeOK(base, ['start', 'mock-extsvc'], 60_000)
    await waitHealthz(MOCK_URL, 90_000)

    return {
      ws,
      project,
      composeFile,
      env,
      coordBaseUrl: COORD_URL,
      mockBaseUrl: MOCK_URL,
      apiKey,
      relayPeerId,
      network,
      volume,
      groupKeyPriv,
      groupKeyPubCompressed,
      coordMembers,
      memberKeyHexByIndex,
      stop,
    }
  }
  catch (e) {
    await stop()
    throw e
  }
}

/**
 * Phase 1: deliver each member's PRIVATE secret config into its OWN container
 * (never on a shared medium), start the N member containers, wait for the
 * public results on the shared volume, and validate the relay-MPC outcome
 * (all-via-relay across containers, keygen agreement, reshare invariance).
 */
export async function runDockerPhase1(ctx: DockerRingContext): Promise<Phase1Result> {
  const base: Base = { project: ctx.project, composeFile: ctx.composeFile, env: ctx.env }
  for (let i = 0; i < N; i++) {
    const cfg: DeviceConfig = {
      index: i,
      n: N,
      threshold: TSS_THRESHOLD,
      groupId: RELAY_GROUP_ID,
      relayPeerId: ctx.relayPeerId,
      relayAddrs: RELAY_ADDRS,
      pskHex: HARNESS_PSK_HEX,
      groupPubHex: '',
      memberKeyHex: ctx.memberKeyHexByIndex[i] ?? '',
      groupKeyHex: bytesToHex(ctx.groupKeyPriv),
      signers: SIGNERS,
      digestHex: EIP155_DIGEST,
      rendezvousDir: '/rz',
      resultPath: `/rz/result-${i}.json`,
    }
    const local = join(ctx.ws, `m${i}.json`)
    writeFileSync(local, JSON.stringify(cfg), { mode: 0o600 })
    // PRIVATE delivery: cp targets ONLY member<i>'s /work — the secret
    // memberKeyHex/groupKeyHex never touches the shared volume.
    await composeOK(base, ['cp', local, `member${i}:/work/config.json`], 60_000)
  }
  await composeOK(base, ['start', 'member0', 'member1', 'member2'], 60_000)

  const deadline = Date.now() + 12 * 60_000
  const results: DeviceResult[] = []
  for (let i = 0; i < N; i++) {
    let parsed: DeviceResult | undefined
    while (Date.now() < deadline) {
      const r = await run(
        ['docker', 'run', '--rm', '-v', `${ctx.volume}:/rz`, ALPINE, 'cat', `/rz/result-${i}.json`],
        ctx.env,
        60_000,
      )
      if (r.code === 0) {
        try {
          parsed = JSON.parse(r.stdout) as DeviceResult
          break
        }
        catch {
          // partial write; retry
        }
      }
      await Bun.sleep(1000)
    }
    if (parsed === undefined)
      throw new Error(`docker member ${i}: result-${i}.json not produced within budget`)
    if (parsed.index !== i)
      throw new Error(`docker member ${i}: result has index ${parsed.index}`)
    writeFileSync(join(ctx.ws, `result-${i}.json`), JSON.stringify(parsed))
    results.push(parsed)
  }

  for (const m of results) {
    if (m.err !== '')
      throw new Error(`docker member ${m.index} failed: ${m.err}`)
    if (!m.allViaRelay)
      throw new Error(`docker member ${m.index} had a non-relay connection (zero-trust relay violated across containers)`)
  }
  const masterHex = results[0]!.groupPubHex
  for (const m of results) {
    if (m.groupPubHex !== masterHex)
      throw new Error(`docker member ${m.index} keygen pubkey mismatch`)
    if (m.resharedPubHex !== masterHex)
      throw new Error(`docker member ${m.index} reshare changed the master key`)
  }
  const signer = results.find(m => m.signed)
  if (signer === undefined)
    throw new Error('docker: no signer produced a signature')

  return { masterPubUncompressed: hexToBytesLocal(masterHex), members: results, signer }
}

function hexToBytesLocal(h: string): Uint8Array {
  const out = new Uint8Array(h.length / 2)
  for (let i = 0; i < out.length; i++)
    out[i] = Number.parseInt(h.slice(i * 2, i * 2 + 2), 16)
  return out
}

/** Provision the coord group with the Phase-1 master key (S-002 canonical). */
export async function provisionDocker(ctx: DockerRingContext, masterPubUncompressed: Uint8Array): Promise<void> {
  const ext = new ExternalClient(ctx.coordBaseUrl, ctx.apiKey)
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

export function expectedAddresses(masterPubUncompressed: Uint8Array): { evm: string, tron: string } {
  return { evm: ethAddress(masterPubUncompressed), tron: tronAddress(masterPubUncompressed) }
}

export const ANCHOR = { digestHex: EIP155_DIGEST, rlpHex: EIP155_RLP }
export const REQUEST_ID = '7265712d646b722d6532652d30303031'
export { COORD_GROUP_ID, N, RELAY_ADDRS, SIGNERS }
