import type { Config } from './config.ts'
import type { BusinessInfo, GroupPublic, ResultPayload, SigningRequest } from './contract/index.ts'
import type { RSVVerification } from './verify.ts'
import { ENVELOPE_VERSION_V1, metaHash, signEnvelope } from './contract/index.ts'
import { CoordClient } from './coord-client.ts'
import { extractRSV, isTerminal } from './result.ts'
import { verifyGroupRSV } from './verify.ts'
import { WebhookServer } from './webhook-server.ts'

// Orchestrates the external-business-service side of the E2E loop
// (testing.md §3.1): (b) request address -> (a) self-build & submit envelope -> (c) receive & verify {R,S,V}.
// E2E-001 drives this against a live node(coord+relay)+CLI members.

export interface EnvelopeInput {
  groupId: string
  chain: string
  unsignedTx: Uint8Array
  digest32: Uint8Array
  proposer: string
  createdAt?: number // unix ms; default Date.now()
  expiry: number // unix ms, absolute
  businessInfo?: BusinessInfo | null
}

export interface RunOutcome {
  requestId: string
  status: string
  result: ResultPayload
  verification?: RSVVerification
}

/**
 * Build a fully-signed envelope: metaHash = H(businessInfo), proposerSig over
 * the canonical digest. The bytes signed are wire-format independent
 * (S-001 §8), so coord re-derives the identical preimage.
 */
export function buildEnvelope(input: EnvelopeInput, proposerPriv: Uint8Array): SigningRequest {
  const req: SigningRequest = {
    version: ENVELOPE_VERSION_V1,
    requestId: crypto.randomUUID(),
    groupId: input.groupId,
    chain: input.chain,
    unsignedTx: input.unsignedTx,
    digest32: input.digest32,
    proposer: input.proposer,
    createdAt: input.createdAt ?? Date.now(),
    expiry: input.expiry,
    businessInfo: input.businessInfo ?? null,
    metaHash: metaHash(input.businessInfo ?? null),
    proposerSig: new Uint8Array(0),
  }
  return signEnvelope(proposerPriv, req)
}

export class MockExtSvc {
  private readonly client: CoordClient
  private readonly webhook?: WebhookServer

  constructor(private readonly cfg: Config, fetchImpl: typeof fetch = fetch) {
    this.client = new CoordClient(cfg, fetchImpl)
    if (cfg.RESULT_MODE === 'webhook') {
      this.webhook = new WebhookServer(cfg)
      this.webhook.start()
    }
  }

  /** (b) Apply for the group's derived chain addresses (api.md A1). */
  requestAddress(groupId: string): Promise<GroupPublic> {
    return this.client.getGroupPublic(groupId)
  }

  /** The coord callback URL to register when RESULT_MODE=webhook. */
  get callbackURL(): string | undefined {
    return this.webhook?.callbackURL
  }

  /**
   * Full loop for one request: A1 (re-fetch group for verification) → A2
   * submit → await terminal result → on RETURNED, independently verify the
   * {R,S,V} against the group's A1 evm/tron addresses (testing.md §3.1).
   * EXPIRED/REJECTED/FAILED return with no verification (3.2: external receives EXPIRED).
   */
  async run(input: EnvelopeInput, proposerPriv: Uint8Array): Promise<RunOutcome> {
    const group = await this.client.getGroupPublic(input.groupId)
    const req = buildEnvelope(input, proposerPriv)
    await this.client.submitRequest(req)
    const result = await this.awaitResult(req.requestId)

    let verification: RSVVerification | undefined
    if (result.status.toUpperCase() === 'RETURNED') {
      const rsv = extractRSV(result)
      if (!rsv)
        throw new Error('coord reported RETURNED without an RSV signature')
      verification = verifyGroupRSV(group, req.digest32, rsv)
    }
    return { requestId: req.requestId, status: result.status, result, verification }
  }

  private async awaitResult(requestId: string): Promise<ResultPayload> {
    if (this.webhook)
      return this.webhook.waitForResult(requestId, this.cfg.RESULT_DEADLINE_MS)

    const deadline = Date.now() + this.cfg.RESULT_DEADLINE_MS
    while (Date.now() < deadline) {
      const p = await this.client.longpollResult(requestId)
      if (isTerminal(p.status))
        return p
    }
    throw new Error(`longpoll deadline exceeded for ${requestId}`)
  }

  close(): void {
    this.webhook?.stop()
  }
}
