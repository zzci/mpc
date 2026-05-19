/**
 * Spawns the real `node` binary with the relay AND coord roles enabled in one
 * process (cmd/server supports double-role), then unlocks the LOCKED coord
 * store at runtime through the admin-api (A-001) — the only path to unlock
 * (server.md C9b: passphrase never in config/env/KMS). The relay config
 * mirrors the proven internal/cli `startRelay` harness.
 */
import type { Subprocess } from 'bun'
import { mkdtempSync, writeFileSync } from 'node:fs'
import { createServer } from 'node:net'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'

export interface ServerHandle {
  proc: Subprocess
  relayPeerId: string
  relayAddrs: string[]
  coordBaseUrl: string
  adminBaseUrl: string
  apiKey: string
  passphrase: string
  workdir: string
  stop: () => Promise<void>
}

export interface ServerOptions {
  serverBin: string
  /** base64-std of the compressed wallet-group cap-token issuer pubkey. */
  groupPubB64: string
  /** hex pnet PSK shared with the member subprocesses. */
  pskHex: string
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

/** Start node (relay+coord+admin), wait for relay + coord readiness, unlock. */
export async function startServer(opts: ServerOptions): Promise<ServerHandle> {
  const workdir = mkdtempSync(join(tmpdir(), 'e2e-node-'))
  const coordPort = await freePort()
  const adminPort = await freePort()
  const apiKey = 'ext-secret-e2e'
  const passphrase = 'e2e-coord-passphrase'
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
  # Config framework v2 (user ruling 2026-05-19): external auth is fixed
  # api_key, result delivery fixed webhook, notification a single fixed
  # webhook (all required). This E2E fetches the result via the A4
  # long-poll endpoint, so the webhook targets are unused localhost
  # discard URLs and the background POST failing is harmless here.
  external: { api_key: "env:COORD_API_KEY", result_webhook: "http://127.0.0.1:9/result" }
  notify: { webhook: "http://127.0.0.1:9/notify" }
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
      // FIX-003 §7.1: dev/test whole-DB encryption disabled so coord starts
      // UNLOCKED-equivalent (no LOCKED 503 on data ops). The production
      // iron-law guardrail requires this explicit non-production confirmation;
      // node fail-closes without it. E2E is non-production by definition.
      ALLOW_INSECURE_DB: '1',
      MPC_ADMIN_LISTEN: `127.0.0.1:${adminPort}`,
      MPC_ADMIN_READ_TOKEN: 'e2e-admin-read-token-0001',
      MPC_ADMIN_CONTROL_TOKEN: 'e2e-admin-control-token-0002',
    },
    stdout: 'inherit',
    stderr: 'pipe',
  })

  const handle: ServerHandle = {
    proc,
    relayPeerId: '',
    relayAddrs: [],
    coordBaseUrl: `http://127.0.0.1:${coordPort}`,
    adminBaseUrl: `http://127.0.0.1:${adminPort}`,
    apiKey,
    passphrase,
    workdir,
    stop: async () => {
      proc.kill('SIGKILL')
      await proc.exited
    },
  }

  await parseRelayStarted(proc, handle)
  await waitHealthz(handle.coordBaseUrl, 20_000)
  await waitHealthz(handle.adminBaseUrl, 20_000)
  // FIX-003 §7.1: with whole-DB encryption disabled the store opens
  // UNLOCKED-equivalent (OpenInsecure) — no admin-api unlock step, and a
  // plaintext store rejects Unlock by design. The admin-unlock path remains
  // covered by the coord/admin Go suites (encrypted mode).
  return handle
}

/** Scan the structured JSON stderr for the relay's ephemeral peer + addrs. */
async function parseRelayStarted(proc: Subprocess, handle: ServerHandle): Promise<void> {
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
