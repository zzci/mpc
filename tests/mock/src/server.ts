import type { Config } from './config.ts'
import type { SigningRequest } from './contract/index.ts'
import process from 'node:process'
import { secp256k1 } from '@noble/curves/secp256k1.js'
import { z } from 'zod'
import { loadConfig } from './config.ts'
import { ENVELOPE_VERSION_V1, metaHash, signEnvelope } from './contract/index.ts'
import { CoordClient, CoordError } from './coord-client.ts'
import { extractRSV, isTerminal } from './result.ts'
import { fromHex, toB64, toHex } from './shared/bytes.ts'
import { verifyGroupRSV } from './verify.ts'

// Thin HTTP control-plane wrapping the already-verified MockExtSvc/CoordClient
// library API, so the E2E-001 harness can launch this double as a subprocess
// (testing.md §3.1 subprocess topology). It adds NO crypto/contract logic —
// proposerSig/metaHash/canonical/verify come unchanged from src/contract.
//
// Documented contract (e2e/src/lib/mock-extsvc.ts:42-55):
//   GET  /healthz                                  → 200
//   POST /control/request-address {groupId}        → 200 {ecdsaPubkeyB64,evmAddress,tronAddress}
//   POST /control/submit {...}                     → 202 {requestId}
//   GET  /control/result/{requestId}               → 200 {status,rsvB64?,recovered?}

const RequestAddressBody = z.object({ groupId: z.string().min(1) })

const SubmitBody = z.object({
  groupId: z.string().min(1),
  chain: z.string().min(1),
  digest32Hex: z.string().regex(/^[0-9a-f]{64}$/i, 'digest32Hex must be 32 bytes hex'),
  unsignedTxHex: z.string().regex(/^([0-9a-f]{2})*$/i, 'unsignedTxHex must be hex'),
  requestId: z.string().min(1),
  expiryMillis: z.coerce.number().int().positive(),
})

interface ResultState {
  status: string
  rsvB64?: string
  recovered?: {
    ok: boolean
    evmAddress: string
    tronAddress: string
    evmMatches: boolean
    tronMatches: boolean
    pubkeyMatches: boolean
  }
  reason?: string
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}

function errBody(code: string, message: string, status: number): Response {
  return json({ error: { code, message } }, status)
}

/**
 * The control plane. `handle` is a pure request handler (unit-testable without
 * binding a socket, per the pma-bun HTTP-handler testing pattern); `listen`
 * binds Bun.serve for the harness subprocess.
 */
export class ControlServer {
  private readonly client: CoordClient
  private readonly proposerPriv: Uint8Array
  // coord's default proposerKeyResolver (auth.go defaultProposerKey) treats the
  // envelope `proposer` field as a hex-encoded secp256k1 public key (33/65 B);
  // a name literal is a clean INVALID_ENVELOPE. So the proposer identity is the
  // compressed pubkey of MOCKEXT_PROPOSER_PRIVKEY_HEX, hex-encoded.
  private readonly proposerHex: string
  private readonly results = new Map<string, ResultState>()
  private server: ReturnType<typeof Bun.serve> | undefined

  constructor(private readonly cfg: Config, fetchImpl: typeof fetch = fetch) {
    this.client = new CoordClient(cfg, fetchImpl)
    this.proposerPriv = fromHex(cfg.MOCKEXT_PROPOSER_PRIVKEY_HEX)
    this.proposerHex = toHex(secp256k1.getPublicKey(this.proposerPriv, true))
  }

  async handle(req: Request): Promise<Response> {
    const url = new URL(req.url)
    const { pathname } = url
    try {
      if (req.method === 'GET' && pathname === '/healthz')
        return json({ status: 'ok' })

      if (req.method === 'POST' && pathname === '/control/request-address')
        return await this.requestAddress(req)

      if (req.method === 'POST' && pathname === '/control/submit')
        return await this.submit(req)

      if (req.method === 'GET' && pathname.startsWith('/control/result/')) {
        const id = decodeURIComponent(pathname.slice('/control/result/'.length))
        return this.result(id)
      }

      return errBody('NOT_FOUND', `no route ${req.method} ${pathname}`, 404)
    }
    catch (e) {
      // Faithfully surface coord's real api.md-C rejection: top-level `code`
      // (the E2E control contract / harness reads `j.code` at top level) +
      // coord's own HTTP status — never mask it as a generic INTERNAL/502.
      if (e instanceof CoordError)
        return json({ code: e.code ?? 'COORD_ERROR', message: e.message }, e.status)
      const message = e instanceof Error ? e.message : 'internal error'
      return errBody('INTERNAL', message, 502)
    }
  }

  private async requestAddress(req: Request): Promise<Response> {
    const parsed = RequestAddressBody.safeParse(await req.json().catch(() => null))
    if (!parsed.success)
      return errBody('BAD_REQUEST', parsed.error.message, 400)
    const g = await this.client.getGroupPublic(parsed.data.groupId)
    return json({
      ecdsaPubkeyB64: g.ecdsa_pubkey,
      evmAddress: g.evm_address,
      tronAddress: g.tron_address,
    })
  }

  private async submit(req: Request): Promise<Response> {
    const parsed = SubmitBody.safeParse(await req.json().catch(() => null))
    if (!parsed.success)
      return errBody('BAD_REQUEST', parsed.error.message, 400)
    const b = parsed.data

    // Build + sign with the verified contract primitives (no rewrite).
    const base: SigningRequest = {
      version: ENVELOPE_VERSION_V1,
      requestId: b.requestId,
      groupId: b.groupId,
      chain: b.chain,
      unsignedTx: fromHex(b.unsignedTxHex),
      digest32: fromHex(b.digest32Hex),
      proposer: this.proposerHex,
      createdAt: Date.now(),
      expiry: b.expiryMillis,
      businessInfo: null,
      metaHash: metaHash(null),
      proposerSig: new Uint8Array(0),
    }
    const signed = signEnvelope(this.proposerPriv, base)

    const group = await this.client.getGroupPublic(b.groupId)
    await this.client.submitRequest(signed)
    this.results.set(b.requestId, { status: 'PENDING' })
    // Background watcher; harness owns process lifecycle (SIGKILL).
    void this.watch(b.requestId, signed.digest32, group)
    return json({ requestId: b.requestId }, 202)
  }

  private async watch(
    requestId: string,
    digest32: Uint8Array,
    group: Awaited<ReturnType<CoordClient['getGroupPublic']>>,
  ): Promise<void> {
    const deadline = Date.now() + this.cfg.RESULT_DEADLINE_MS
    try {
      while (Date.now() < deadline) {
        const p = await this.client.longpollResult(requestId)
        if (!isTerminal(p.status))
          continue
        if (p.status.toUpperCase() === 'RETURNED') {
          const rsv = extractRSV(p)
          if (!rsv) {
            this.results.set(requestId, { status: 'FAILED', reason: 'RETURNED without RSV' })
            return
          }
          const v = verifyGroupRSV(group, digest32, rsv)
          this.results.set(requestId, {
            status: 'RETURNED',
            rsvB64: toB64(rsv),
            recovered: {
              ok: v.ok,
              evmAddress: v.evmAddress,
              tronAddress: v.tronAddress,
              evmMatches: v.evmMatches,
              tronMatches: v.tronMatches,
              pubkeyMatches: v.pubkeyMatches,
            },
          })
          return
        }
        this.results.set(requestId, { status: p.status.toUpperCase(), reason: p.reason })
        return
      }
      this.results.set(requestId, { status: 'FAILED', reason: 'result deadline exceeded' })
    }
    catch (e) {
      this.results.set(requestId, {
        status: 'FAILED',
        reason: e instanceof Error ? e.message : 'watch error',
      })
    }
  }

  private result(requestId: string): Response {
    const state = this.results.get(requestId)
    if (!state)
      return errBody('NOT_FOUND', `unknown requestId ${requestId}`, 404)
    return json(state)
  }

  listen(): { port: number } {
    if (!this.server) {
      this.server = Bun.serve({
        port: this.cfg.PORT,
        fetch: req => this.handle(req),
      })
    }
    return { port: this.server.port ?? this.cfg.PORT }
  }

  stop(): void {
    this.server?.stop(true)
    this.server = undefined
  }
}

if (import.meta.main) {
  const server = new ControlServer(loadConfig())
  const { port } = server.listen()
  process.stdout.write(`${JSON.stringify({ control: `http://127.0.0.1:${port}`, healthz: '/healthz' })}\n`)
}
