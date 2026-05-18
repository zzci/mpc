/**
 * coord HTTP clients (api.md A/B). The member B-side is hand-rolled here
 * exactly as internal/cli/coordflow_test.go does (B-002 coord client is
 * unmerged) — the suite signs the B1 member authenticator the coord
 * `memberGate` verifies. The external A-side uses the `X-API-Key` header.
 */
import type { GroupProvisioning } from './canonical.ts'
import { bytesToB64, utf8Bytes } from './bytes.ts'
import { groupProvisionDigest, memberAuthDigest, memberAuthParams } from './canonical.ts'
import { signDigestDER } from './crypto.ts'

export interface MemberKey {
  memberId: string
  /** secp256k1 private scalar (identity key). */
  priv: Uint8Array
  /** compressed identity pubkey. */
  pub: Uint8Array
}

export class ExternalClient {
  constructor(private readonly baseUrl: string, private readonly apiKey: string) {}

  private headers(extra?: Record<string, string>): Record<string, string> {
    return { 'X-API-Key': this.apiKey, 'Content-Type': 'application/json', ...extra }
  }

  /**
   * POST /v1/groups (S-002). Signs the provisioning digest with the
   * cap/group key and collects >= thresholdT member co-signatures, then
   * sends every []byte field base64-std (Go json default).
   */
  async provisionGroup(
    prov: GroupProvisioning,
    groupKeyPriv: Uint8Array,
    coSigners: MemberKey[],
  ): Promise<void> {
    const digest = groupProvisionDigest(prov)
    const groupSig = signDigestDER(groupKeyPriv, digest)
    const memberCoSigs = coSigners.map(s => ({
      memberId: s.memberId,
      sig: bytesToB64(signDigestDER(s.priv, digest)),
    }))
    const body = {
      version: Number(prov.version),
      groupId: prov.groupId,
      ecdsaPubkey: bytesToB64(prov.ecdsaPubkey),
      groupPubkey: bytesToB64(prov.groupPubkey),
      thresholdT: prov.thresholdT,
      partiesN: prov.partiesN,
      members: prov.members.map(m => ({
        memberId: m.memberId,
        identityPubkey: bytesToB64(m.identityPubkey),
      })),
      createdAt: prov.createdAt,
      groupSig: bytesToB64(groupSig),
      memberCoSigs,
    }
    const r = await fetch(`${this.baseUrl}/v1/groups`, {
      method: 'POST',
      headers: this.headers(),
      body: JSON.stringify(body),
    })
    if (r.status !== 201)
      throw new Error(`provisionGroup: ${r.status} ${await r.text()}`)
  }

  /** GET /v1/requests/{id} (A3). */
  async status(requestId: string): Promise<{ status: string, failReason?: string }> {
    const r = await fetch(`${this.baseUrl}/v1/requests/${requestId}`, { headers: this.headers() })
    const j = (await r.json()) as { status: string, fail_reason?: string }
    return { status: j.status, failReason: j.fail_reason }
  }

  /** GET /v1/requests/{id}/result?wait= (A4 longpoll). */
  async resultLongpoll(
    requestId: string,
    waitSec = 10,
  ): Promise<{ status: string, rsvB64?: string, reason?: string }> {
    const r = await fetch(
      `${this.baseUrl}/v1/requests/${requestId}/result?wait=${waitSec}s`,
      { headers: this.headers() },
    )
    const j = (await r.json()) as { status: string, rsv?: string, reason?: string }
    return { status: j.status, rsvB64: j.rsv, reason: j.reason }
  }
}

/** Hand-rolled B1 member client (mirrors coordflow_test.go `coordClient`). */
export class MemberClient {
  constructor(
    private readonly baseUrl: string,
    private readonly groupId: string,
  ) {}

  private memberHeaders(
    key: MemberKey,
    method: string,
    rawParams: Uint8Array,
  ): Record<string, string> {
    const ts = BigInt(Date.now())
    const nonce = crypto.getRandomValues(new Uint8Array(16))
    const params = memberAuthParams(method, this.groupId, rawParams)
    const digest = memberAuthDigest(key.memberId, method, params, ts, nonce)
    const sig = signDigestDER(key.priv, digest)
    return {
      'X-Member-Id': key.memberId,
      'X-Member-Ts': ts.toString(),
      'X-Member-Nonce': bytesToB64(nonce),
      'X-Member-Sig': bytesToB64(sig),
      'Content-Type': 'application/json',
    }
  }

  /** POST /v1/members/self/heartbeat (B5) -> 204. */
  async heartbeat(key: MemberKey, relayPeerId: string): Promise<void> {
    const body = JSON.stringify({ groupId: this.groupId, memberId: key.memberId, relayPeerID: relayPeerId })
    const r = await fetch(`${this.baseUrl}/v1/members/self/heartbeat`, {
      method: 'POST',
      headers: this.memberHeaders(key, 'B5:heartbeat', utf8Bytes(body)),
      body,
    })
    if (r.status !== 204)
      throw new Error(`heartbeat ${key.memberId}: ${r.status} ${await r.text()}`)
  }

  /** POST /v1/requests/{id}/decision (B4) -> 200. */
  async decide(key: MemberKey, requestId: string, decision: 'approved' | 'rejected'): Promise<void> {
    const body = JSON.stringify({ memberId: key.memberId, decision })
    const r = await fetch(`${this.baseUrl}/v1/requests/${requestId}/decision`, {
      method: 'POST',
      headers: this.memberHeaders(key, `B4:decision:${decision}`, utf8Bytes(body)),
      body,
    })
    if (r.status !== 200)
      throw new Error(`decision ${key.memberId}: ${r.status} ${await r.text()}`)
  }

  /**
   * GET /v1/groups/{groupId}/dispatch?wait= (B6). Polls until a START whose
   * requestId matches, or the deadline. Returns the START envelope JSON.
   */
  async waitForStart(
    key: MemberKey,
    requestId: string,
    deadlineMs: number,
  ): Promise<StartSigning> {
    while (Date.now() < deadlineMs) {
      const query = 'wait=5s'
      const r = await fetch(`${this.baseUrl}/v1/groups/${this.groupId}/dispatch?${query}`, {
        headers: this.memberHeaders(key, 'B6:dispatch', utf8Bytes(query)),
      })
      if (r.ok) {
        const j = (await r.json()) as Partial<StartSigning>
        if (j.requestId === requestId)
          return j as StartSigning
      }
    }
    throw new Error(`waitForStart: no START for ${requestId} before deadline`)
  }

  /** POST /v1/requests/{id}/result (B7) -> 200. `rsvCompact` is [V+27‖R‖S]. */
  async postResult(key: MemberKey, requestId: string, rsvCompact: Uint8Array): Promise<void> {
    const body = JSON.stringify({ memberId: key.memberId, rsv: bytesToB64(rsvCompact) })
    const r = await fetch(`${this.baseUrl}/v1/requests/${requestId}/result`, {
      method: 'POST',
      headers: this.memberHeaders(key, 'B7:result', utf8Bytes(body)),
      body,
    })
    if (r.status !== 200)
      throw new Error(`postResult ${key.memberId}: ${r.status} ${await r.text()}`)
  }
}

export interface StartSigning {
  requestId: string
  envelope: {
    version: number
    requestId: string
    groupId: string
    chain: string
    unsignedTx: string
    digest32: string
    proposer: string
    createdAt: number
    expiry: number
    metaHash: string
    proposerSig: string
  }
  signers: string[]
  deadline: number
}
