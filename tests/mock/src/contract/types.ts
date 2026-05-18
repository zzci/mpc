import { z } from 'zod'

// Logical types of docs/design/contract/protocol.md §1 as the external business
// service holds them BEFORE canonicalization. Per docs/spec/envelope-canonical.md
// (S-001) signatures/hashes are derived from these values, never from any wire
// form, so the JSON the service POSTs to coord (api.md A2) and the bytes it
// signs are decoupled — exactly the Go contract package's invariant.

/**
 * Optional structured human-review payload (api.md:17). Field names match the
 * Go `json:` tags of contract.BusinessInfo; Go `omitempty` semantics are
 * reproduced in metahash.ts so the JCS preimage is byte-identical.
 */
export interface BusinessInfo {
  title?: string
  summary?: string
  items?: string[]
  refs?: Record<string, string>
  requester?: string
  memo?: string
  displayHints?: Record<string, string>
}

/** The authoritative signing envelope (protocol.md:10-23). */
export interface SigningRequest {
  version: number
  requestId: string
  groupId: string
  chain: string
  unsignedTx: Uint8Array
  digest32: Uint8Array
  proposer: string
  createdAt: number // unix ms (S-001 §5)
  expiry: number // unix ms, absolute
  businessInfo?: BusinessInfo | null
  metaHash: Uint8Array
  proposerSig: Uint8Array
}

/** The only envelope version this build accepts (contract.EnvelopeVersionV1). */
export const ENVELOPE_VERSION_V1 = 1

/**
 * coord A1 response: GET /v1/groups/{groupId}/public (api.md A1, XA-001).
 * Only public group data; no member/share/epoch leakage.
 */
export const GroupPublicSchema = z.object({
  groupId: z.string().min(1),
  ecdsa_pubkey: z.string().min(1), // base64 secp256k1 pubkey
  evm_address: z.string().min(1),
  tron_address: z.string().min(1),
  threshold_t: z.number().int().nonnegative(),
  parties_n: z.number().int().positive(),
})

export type GroupPublic = z.infer<typeof GroupPublicSchema>

/** coord A2 response: POST /v1/requests → 202 (api.md A2). */
export const SubmitAcceptedSchema = z.object({
  requestId: z.string().min(1),
  status: z.string().min(1),
})

export type SubmitAccepted = z.infer<typeof SubmitAcceptedSchema>

/**
 * coord A4 webhook body / A3 longpoll result terminal shape (api.md A3/A4).
 * RSV is the 65-byte [V+27 || R(32) || S(32)] compact form, base64.
 */
export const ResultPayloadSchema = z.object({
  requestId: z.string().min(1),
  status: z.string().min(1),
  RSV: z.string().optional(),
  rsv: z.string().optional(), // tolerate either casing across A3/A4 shapes
  result: z
    .object({ R: z.string(), S: z.string(), V: z.number() })
    .optional(),
  reason: z.string().optional(),
})

export type ResultPayload = z.infer<typeof ResultPayloadSchema>
