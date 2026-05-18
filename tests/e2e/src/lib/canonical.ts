/**
 * Byte-exact ports of the Go canonical preimages that get signed and
 * cross-verified by coord/devices. Field order, domain tags, length-prefix
 * widths and NFC usage MUST match:
 *  - internal/server/coord/provision_canonical.go  (groupProvisionDigest)
 *  - internal/server/coord/auth.go                 (memberAuthDigest)
 *  - internal/contract/canonical.go                (EnvelopeDigest)
 *  - internal/contract/metahash.go                 (MetaHash)
 */
import {
  concat,
  hexToBytes,
  putI64,
  putLP,
  putRawStr,
  putStr,
  putU32,
  putU64,
  rfc3339ToMillis,
  utf8Bytes,
} from './bytes.ts'
import { sha256 } from './crypto.ts'

function domain(tag: string): Uint8Array {
  return concat(utf8Bytes(tag), new Uint8Array([0x00]))
}

const GROUP_PROVISION_DOMAIN = domain('TSS-GROUP-PROVISIONING-CANONICAL-v1')
const MEMBER_AUTH_DOMAIN = domain('TSS-COORD-MEMBER-AUTH-v1')
const ENVELOPE_DOMAIN = domain('TSS-ENVELOPE-CANONICAL-v1')

export interface ProvisionMember {
  memberId: string
  identityPubkey: Uint8Array
}

export interface GroupProvisioning {
  version: bigint | number
  groupId: string
  ecdsaPubkey: Uint8Array
  groupPubkey: Uint8Array
  thresholdT: number
  partiesN: number
  members: ProvisionMember[]
  /** RFC3339Nano. */
  createdAt: string
}

/** SHA-256 over the S-002 group-provisioning canonical preimage. */
export function groupProvisionDigest(p: GroupProvisioning): Uint8Array {
  const parts: Uint8Array[] = [
    GROUP_PROVISION_DOMAIN,
    putU64(p.version),
    putStr(p.groupId),
    putLP(p.ecdsaPubkey),
    putLP(p.groupPubkey),
    putI64(p.thresholdT),
    putI64(p.partiesN),
    putU32(p.members.length),
  ]
  for (const m of p.members)
    parts.push(putStr(m.memberId), putLP(m.identityPubkey))
  parts.push(putI64(rfc3339ToMillis(p.createdAt)))
  return sha256(concat(...parts))
}

/**
 * SHA-256 over the B1 member-auth canonical preimage. NOTE: memberId/method
 * are length-prefixed WITHOUT NFC (Go uses `appendLP([]byte(s))`, distinct
 * from the NFC `putStr` used by the provisioning preimage); `ts` is unix
 * milliseconds as an 8-byte BE uint64.
 */
export function memberAuthDigest(
  memberId: string,
  method: string,
  params: Uint8Array,
  tsMillis: bigint | number,
  nonce: Uint8Array,
): Uint8Array {
  return sha256(concat(
    MEMBER_AUTH_DOMAIN,
    putRawStr(memberId),
    putRawStr(method),
    putLP(params),
    putU64(tsMillis),
    putLP(nonce),
  ))
}

/**
 * The `params` argument the coord `memberGate` reconstructs before hashing:
 * `sha256(method + "|" + groupId + "|" + rawParams)`. `rawParams` is the raw
 * request body for POST endpoints, or the raw URL query for GET endpoints.
 */
export function memberAuthParams(method: string, groupId: string, rawParams: Uint8Array): Uint8Array {
  return sha256(concat(utf8Bytes(`${method}|${groupId}|`), rawParams))
}

export interface Envelope {
  version: bigint | number
  requestId: string
  groupId: string
  chain: string
  unsignedTx: Uint8Array
  digest32: Uint8Array
  proposer: string
  /** unix milliseconds */
  createdAt: bigint | number
  /** unix milliseconds */
  expiry: bigint | number
  metaHash: Uint8Array
}

/** requestId -> 16 raw UUID bytes: accepts 36-char hyphenated or 32-hex. */
function uuidBytes(requestId: string): Uint8Array {
  const hex = requestId.replace(/-/g, '')
  if (hex.length !== 32)
    throw new Error(`uuidBytes: requestId must be 16 bytes hex (got ${hex.length} nibbles)`)
  return hexToBytes(hex)
}

/** SHA-256 over the envelope canonical preimage (proposerSig is over this). */
export function envelopeDigest(e: Envelope): Uint8Array {
  if (e.digest32.length !== 32)
    throw new Error('envelopeDigest: digest32 must be 32 bytes')
  if (e.metaHash.length !== 32)
    throw new Error('envelopeDigest: metaHash must be 32 bytes')
  return sha256(concat(
    ENVELOPE_DOMAIN,
    putU64(e.version),
    uuidBytes(e.requestId),
    putStr(e.groupId),
    putStr(e.chain),
    putLP(e.unsignedTx),
    e.digest32,
    putStr(e.proposer),
    putI64(e.createdAt),
    putI64(e.expiry),
    e.metaHash,
  ))
}

/** sha256(nil) — Go `contract.EmptyMetaHash` (nil businessInfo). */
export const EMPTY_META_HASH: Uint8Array = sha256(new Uint8Array(0))
