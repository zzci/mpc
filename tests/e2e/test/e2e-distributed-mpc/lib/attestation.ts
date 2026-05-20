/**
 * Byte-exact TS port of the B11 attestation canonical preimage
 * (internal/server/coord/attestation.go `attestationDigest` /
 * `attestationDomain`). Field order, domain tag, and length-prefix
 * widths MUST match — coord verifies the attestor's signature over
 * this preimage and any divergence silently rejects the request as
 * 401 UNAUTHENTICATED.
 *
 * Also bundles a minimal B11 client (the parent suite's coord-client
 * is a finalized DM-3/DM-4 artifact; the DM-6 batch keeps this helper
 * scoped to e2e-distributed-mpc/).
 */
import { bytesToB64, bytesToHex, concat, putI64, putLP, putStr, utf8Bytes } from '../../../src/lib/bytes.ts'
import { sha256, signDigestDER } from '../../../src/lib/crypto.ts'

function domain(tag: string): Uint8Array {
  return concat(utf8Bytes(tag), new Uint8Array([0x00]))
}

const ATTESTATION_DOMAIN = domain('TSS-COORD-ATTESTATION-CANONICAL-v1')

export interface AttestationView {
  groupId: string
  identityPubkey: Uint8Array
  holdsShare: boolean
  /** required when holdsShare=true (33 or 65 bytes). */
  groupPubkey: Uint8Array
  /** required when holdsShare=true (32 bytes). */
  chaincode: Uint8Array
  ts: number
}

/** SHA-256 over the B11 attestation canonical preimage. */
export function attestationDigest(v: AttestationView): Uint8Array {
  return sha256(concat(
    ATTESTATION_DOMAIN,
    putStr(v.groupId),
    putLP(v.identityPubkey),
    new Uint8Array([v.holdsShare ? 1 : 0]),
    putLP(v.groupPubkey),
    putLP(v.chaincode),
    putI64(v.ts),
  ))
}

export interface AttestationBody {
  identityPubkey: string
  holdsShare: boolean
  groupPubkeyHex?: string
  chaincodeHex?: string
  ts: number
  sig: string
}

/** Build a signed attestation body for the given member. */
export function buildAttestation(
  v: AttestationView,
  privKey: Uint8Array,
): AttestationBody {
  const digest = attestationDigest(v)
  const sig = signDigestDER(privKey, digest)
  const body: AttestationBody = {
    identityPubkey: bytesToHex(v.identityPubkey),
    holdsShare: v.holdsShare,
    ts: v.ts,
    sig: bytesToB64(sig),
  }
  if (v.holdsShare) {
    body.groupPubkeyHex = bytesToHex(v.groupPubkey)
    body.chaincodeHex = bytesToHex(v.chaincode)
  }
  return body
}
