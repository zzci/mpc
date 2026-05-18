import type { Config } from './config.ts'
import type { GroupPublic, ResultPayload, SigningRequest, SubmitAccepted } from './contract/index.ts'
import { GroupPublicSchema, ResultPayloadSchema, SubmitAcceptedSchema } from './contract/index.ts'
import { toB64 } from './shared/bytes.ts'

// External-service ↔ coord HTTP client (api.md A1/A2/A3). REST + JSON, byte
// fields base64, /v1 prefix. Auth = coord.external.auth api_key header (mTLS
// is not exercised by the in-process E2E double).

/** Transport-boundary error carrying the coord HTTP status and api.md C code. */
export class CoordError extends Error {
  override readonly name = 'CoordError'
  constructor(
    message: string,
    readonly status: number,
    readonly code?: string,
    options?: ErrorOptions,
  ) {
    super(message, options)
  }
}

/**
 * The A2 request body — contract.SigningRequest JSON shape. Per
 * docs/spec/envelope-canonical.md S-001 §5 the time closure is int64 unix ms
 * (overriding api.md's RFC3339 prose); byte fields are base64. coord decodes
 * this straight into contract.SigningRequest, then re-derives the same
 * canonical preimage we signed.
 */
export function envelopeToA2JSON(req: SigningRequest): Record<string, unknown> {
  const body: Record<string, unknown> = {
    version: req.version,
    requestId: req.requestId,
    groupId: req.groupId,
    chain: req.chain,
    unsignedTx: toB64(req.unsignedTx),
    digest32: toB64(req.digest32),
    proposer: req.proposer,
    createdAt: req.createdAt,
    expiry: req.expiry,
    metaHash: toB64(req.metaHash),
    proposerSig: toB64(req.proposerSig),
  }
  if (req.businessInfo !== null && req.businessInfo !== undefined)
    body.businessInfo = req.businessInfo
  return body
}

export class CoordClient {
  constructor(private readonly cfg: Config, private readonly fetchImpl: typeof fetch = fetch) {}

  private headers(extra?: Record<string, string>): Record<string, string> {
    const h: Record<string, string> = { accept: 'application/json', ...extra }
    if (this.cfg.COORD_API_KEY)
      h[this.cfg.COORD_API_KEY_HEADER] = this.cfg.COORD_API_KEY
    return h
  }

  private url(path: string): string {
    return `${this.cfg.COORD_BASE_URL.replace(/\/$/, '')}${path}`
  }

  private async parseError(resp: Response): Promise<never> {
    let code: string | undefined
    let message = `coord ${resp.status}`
    try {
      const body = await resp.json() as { error?: { code?: string, message?: string } }
      code = body.error?.code
      if (body.error?.message)
        message = body.error.message
    }
    catch {
      // non-JSON error body; keep the status-only message
    }
    throw new CoordError(message, resp.status, code)
  }

  /** A1: GET /v1/groups/{groupId}/public — "申请地址" (XA-001 endpoint). */
  async getGroupPublic(groupId: string): Promise<GroupPublic> {
    const resp = await this.fetchImpl(this.url(`/v1/groups/${encodeURIComponent(groupId)}/public`), {
      method: 'GET',
      headers: this.headers(),
    })
    if (!resp.ok)
      await this.parseError(resp)
    return GroupPublicSchema.parse(await resp.json())
  }

  /** A2: POST /v1/requests — submit the self-constructed signing envelope. */
  async submitRequest(req: SigningRequest): Promise<SubmitAccepted> {
    const resp = await this.fetchImpl(this.url('/v1/requests'), {
      method: 'POST',
      headers: this.headers({ 'content-type': 'application/json' }),
      body: JSON.stringify(envelopeToA2JSON(req)),
    })
    if (!resp.ok)
      await this.parseError(resp)
    return SubmitAcceptedSchema.parse(await resp.json())
  }

  /** A3: GET /v1/requests/{requestId} — poll status. */
  async getStatus(requestId: string): Promise<ResultPayload> {
    const resp = await this.fetchImpl(this.url(`/v1/requests/${encodeURIComponent(requestId)}`), {
      method: 'GET',
      headers: this.headers(),
    })
    if (!resp.ok)
      await this.parseError(resp)
    return ResultPayloadSchema.parse(await resp.json())
  }

  /**
   * A4 longpoll: GET /v1/requests/{id}/result?wait=… blocks until a terminal
   * state. Returns the terminal ResultPayload (RETURNED/EXPIRED/REJECTED/FAILED).
   */
  async longpollResult(requestId: string, waitS = this.cfg.LONGPOLL_WAIT_S): Promise<ResultPayload> {
    const resp = await this.fetchImpl(
      this.url(`/v1/requests/${encodeURIComponent(requestId)}/result?wait=${waitS}`),
      { method: 'GET', headers: this.headers() },
    )
    if (!resp.ok)
      await this.parseError(resp)
    return ResultPayloadSchema.parse(await resp.json())
  }
}
