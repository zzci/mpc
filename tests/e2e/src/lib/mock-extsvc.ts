/**
 * Integration boundary with MEXT-001 (`mock-extsvc`, separate Bun module /
 * separate worktree, zero file overlap). `mock-extsvc` is the external
 * business service replica: it owns byte-exact `proposerSig`/`metaHash`
 * construction and drives coord A1/A2/A4. This suite only orchestrates it
 * over the small control surface defined below and asserts the ring.
 *
 * Until MEXT-001 is merged, `mock-extsvc` is not resolvable from this
 * worktree; `locate()` then returns a skip reason — the L1-ruled
 * "live-run serial" gate, not a failure.
 */
import { existsSync } from 'node:fs'
import { join, resolve } from 'node:path'
import process from 'node:process'
import { repoRoot } from './go-build.ts'

export interface MockExtsvcLocation {
  available: boolean
  dir?: string
  reason?: string
}

/** env MOCK_EXTSVC_DIR, else `<repoRoot>/tests/mock`. */
export function locate(): MockExtsvcLocation {
  const envDir = process.env.MOCK_EXTSVC_DIR
  const dir = envDir !== undefined && envDir !== ''
    ? resolve(envDir)
    : join(repoRoot(), 'tests', 'mock')
  if (!existsSync(join(dir, 'package.json'))) {
    return {
      available: false,
      reason: `mock-extsvc (MEXT-001) not resolvable at ${dir} — set MOCK_EXTSVC_DIR or merge MEXT-001 (live-run serial per L1)`,
    }
  }
  return { available: true, dir }
}

export interface MockExtsvcHandle {
  baseUrl: string
  stop: () => Promise<void>
}

/**
 * Start `mock-extsvc` pointed at the live coord. Control surface (the
 * documented MEXT-001 ↔ E2E-001 contract):
 *   POST /control/request-address { groupId }
 *        -> 200 { ecdsaPubkeyB64, evmAddress, tronAddress }   (drives coord A1)
 *   POST /control/submit { groupId, chain, digest32Hex, unsignedTxHex,
 *                          requestId, expiryMillis }
 *        -> 202 { requestId }                                  (drives coord A2,
 *                 mock-extsvc builds proposerSig/metaHash byte-exact)
 *   GET  /control/result/{requestId}
 *        -> 200 { status, rsvB64?, recovered? }                (coord A4 + verify)
 *   GET  /healthz -> 200
 */
export async function startMockExtsvc(
  dir: string,
  coordBaseUrl: string,
  apiKey: string,
): Promise<MockExtsvcHandle> {
  const port = 4399 + Math.floor(Math.random() * 600)
  const cmd = (process.env.MOCK_EXTSVC_CMD ?? 'bun run start').split(' ')
  const proc = Bun.spawn(cmd, {
    cwd: dir,
    env: {
      ...process.env,
      PORT: String(port),
      COORD_BASE_URL: coordBaseUrl,
      COORD_API_KEY: apiKey,
    },
    stdout: 'inherit',
    stderr: 'inherit',
  })
  const baseUrl = `http://127.0.0.1:${port}`
  const deadline = Date.now() + 20_000
  while (Date.now() < deadline) {
    try {
      const r = await fetch(`${baseUrl}/healthz`)
      if (r.ok) {
        return {
          baseUrl,
          stop: async () => {
            proc.kill('SIGKILL')
            await proc.exited
          },
        }
      }
    }
    catch {
      // not up yet
    }
    await Bun.sleep(150)
  }
  proc.kill('SIGKILL')
  await proc.exited
  throw new Error(`mock-extsvc did not become healthy at ${baseUrl} within 20s`)
}

export interface AddressResponse {
  ecdsaPubkeyB64: string
  evmAddress: string
  tronAddress: string
}

export class MockExtsvcClient {
  constructor(private readonly baseUrl: string) {}

  async requestAddress(groupId: string): Promise<AddressResponse> {
    const r = await fetch(`${this.baseUrl}/control/request-address`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ groupId }),
    })
    if (!r.ok)
      throw new Error(`mock-extsvc request-address: ${r.status} ${await r.text()}`)
    return (await r.json()) as AddressResponse
  }

  async submit(req: {
    groupId: string
    chain: string
    requestId: string
    digest32Hex: string
    unsignedTxHex: string
    expiryMillis: number
  }): Promise<{ requestId: string, rejected?: boolean, code?: string }> {
    const r = await fetch(`${this.baseUrl}/control/submit`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(req),
    })
    const j = (await r.json()) as { requestId?: string, code?: string }
    if (r.status === 202)
      return { requestId: j.requestId ?? req.requestId }
    return { requestId: req.requestId, rejected: true, code: j.code }
  }

  async result(requestId: string): Promise<{ status: string, rsvB64?: string, recovered?: { ok: boolean, evmAddress: string, tronAddress: string, evmMatches: boolean, tronMatches: boolean, pubkeyMatches: boolean } }> {
    const r = await fetch(`${this.baseUrl}/control/result/${requestId}`)
    if (!r.ok)
      throw new Error(`mock-extsvc result: ${r.status} ${await r.text()}`)
    return (await r.json()) as { status: string, rsvB64?: string, recovered?: { ok: boolean, evmAddress: string, tronAddress: string, evmMatches: boolean, tronMatches: boolean, pubkeyMatches: boolean } }
  }
}
