/**
 * DM-6 acceptance-suite server bootstrap: spawns the real `node` binary
 * (relay+coord+admin in one process, FIX-003 §7.1 plaintext-db mode) with
 * a YAML that includes coord.external.expected_members for the test
 * group. The parent suite's server-process.ts does not configure
 * expected_members (DM-4 is a separate batch), so this fork-helper
 * injects it without modifying the parent harness.
 *
 * Reused verbatim from server-process.ts: relay+coord double-role
 * topology, FIX-003 §7.1 plaintext mode, freePort/healthz/parseRelay
 * helpers — copied so DM-6 owns its harness end-to-end (impl §C strict
 * file ownership).
 */
import type { Subprocess } from 'bun'
import { mkdtempSync, writeFileSync } from 'node:fs'
import { createServer } from 'node:net'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'

export interface DmpcServerHandle {
  proc: Subprocess
  relayPeerId: string
  relayAddrs: string[]
  coordBaseUrl: string
  adminBaseUrl: string
  apiKey: string
  workdir: string
  stop: () => Promise<void>
}

export interface DmpcServerOptions {
  serverBin: string
  /** base64-std of the compressed wallet-group cap-token issuer pubkey. */
  groupPubB64: string
  /** hex pnet PSK shared with the member subprocesses. */
  pskHex: string
  /** map of groupId → hex-encoded (compressed or uncompressed) identity pubkeys. */
  expectedMembers: Record<string, string[]>
}

async function freePort(): Promise<number> {
  return new Promise((res, rej) => {
    const srv = createServer()
    srv.once('error', rej)
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address()
      if (addr !== null && typeof addr === 'object') {
        const { port } = addr
        srv.close(() => res(port))
      }
      else {
        srv.close(() => rej(new Error('freePort: no address')))
      }
    })
  })
}

const RELAY_STARTED = 'relay: started'

/** Format expected_members as a YAML block. */
function expectedMembersYaml(m: Record<string, string[]>): string {
  const lines: string[] = ['    expected_members:']
  for (const [gid, keys] of Object.entries(m)) {
    lines.push(`      "${gid}":`)
    for (const k of keys)
      lines.push(`        - "${k}"`)
  }
  return lines.join('\n')
}

export async function startDmpcServer(opts: DmpcServerOptions): Promise<DmpcServerHandle> {
  const workdir = mkdtempSync(join(tmpdir(), 'e2e-dmpc-node-'))
  const coordPort = await freePort()
  const adminPort = await freePort()
  const apiKey = 'ext-secret-dmpc'
  const dbPath = join(workdir, 'coord.db')

  const cfg = `log: { level: info, format: json }
relay:
  enable: true
  listen: ["/ip4/127.0.0.1/tcp/0"]
  pnet_psk: "env:RELAY_PNET_PSK"
  token_verify: { source: config, group_pubkeys: ["${opts.groupPubB64}"] }
  rendezvous: { enable: false }
  limits: { reservation_per_token: 8, reservation_per_group: 16, bandwidth_per_conn: "4MiB/s" }
coord:
  enable: true
  http: { listen: "127.0.0.1:${coordPort}" }
  db: { dsn: "env:COORD_DB_DSN", encryption: { enable: false } }
  external:
    api_key: "env:COORD_API_KEY"
    result: { url: "http://127.0.0.1:9/result", api_key: "dmpc-result-discard" }
${expectedMembersYaml(opts.expectedMembers)}
  notify: { url: "http://127.0.0.1:9/notify", api_key: "dmpc-notify-discard" }
  ttl: { skew_tolerance: "2m" }
  quorum: { signer_select: stable }
  dispatch: { timeout: "2m" }
`
  const cfgPath = join(workdir, 'server.yaml')
  writeFileSync(cfgPath, cfg, { mode: 0o600 })

  const proc = Bun.spawn([opts.serverBin], {
    cwd: workdir,
    env: {
      ...process.env,
      SERVER_CONFIG: cfgPath,
      RELAY_PNET_PSK: opts.pskHex,
      COORD_DB_DSN: dbPath,
      COORD_API_KEY: apiKey,
      ALLOW_INSECURE_DB: '1',
      MPC_ADMIN_LISTEN: `127.0.0.1:${adminPort}`,
      MPC_ADMIN_READ_TOKEN: 'dmpc-admin-read-0001',
      MPC_ADMIN_CONTROL_TOKEN: 'dmpc-admin-control-0002',
    },
    stdout: 'inherit',
    stderr: 'pipe',
  })

  const handle: DmpcServerHandle = {
    proc,
    relayPeerId: '',
    relayAddrs: [],
    coordBaseUrl: `http://127.0.0.1:${coordPort}`,
    adminBaseUrl: `http://127.0.0.1:${adminPort}`,
    apiKey,
    workdir,
    stop: async () => {
      proc.kill('SIGKILL')
      await proc.exited
    },
  }

  await parseRelayStarted(proc, handle)
  await waitHealthz(handle.coordBaseUrl, 20_000)
  await waitHealthz(handle.adminBaseUrl, 20_000)
  return handle
}

async function parseRelayStarted(proc: Subprocess, handle: DmpcServerHandle): Promise<void> {
  if (proc.stderr == null || typeof proc.stderr === 'number')
    throw new Error('node: no stderr pipe')
  const deadline = Date.now() + 30_000
  const decoder = new TextDecoder()
  let buf = ''
  const reader = (proc.stderr as ReadableStream<Uint8Array>).getReader()
  while (Date.now() < deadline) {
    const { value, done } = await reader.read()
    if (done)
      break
    buf += decoder.decode(value, { stream: true })
    const lines = buf.split('\n')
    buf = lines.pop() ?? ''
    for (const line of lines) {
      if (!line.includes(RELAY_STARTED))
        continue
      try {
        const rec = JSON.parse(line) as { msg?: string, peer?: string, addrs?: string[] }
        if (rec.msg === RELAY_STARTED && rec.peer != null && rec.peer !== '' && (rec.addrs?.length ?? 0) > 0) {
          handle.relayPeerId = rec.peer
          handle.relayAddrs = rec.addrs ?? []
          reader.releaseLock()
          return
        }
      }
      catch {
        // non-JSON log line; keep scanning
      }
    }
  }
  reader.releaseLock()
  throw new Error('node: relay did not report "relay: started" within 30s')
}

async function waitHealthz(baseUrl: string, budgetMs: number): Promise<void> {
  const deadline = Date.now() + budgetMs
  while (Date.now() < deadline) {
    try {
      const r = await fetch(`${baseUrl}/healthz`)
      if (r.ok)
        return
    }
    catch {
      // not up yet
    }
    await Bun.sleep(150)
  }
  throw new Error(`node: ${baseUrl}/healthz not ready within ${budgetMs}ms`)
}
