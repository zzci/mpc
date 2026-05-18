/**
 * Pinned external anchors, identical to the ones internal/cli, internal/
 * txdecode and E-001 pin. The relay-MPC `digestHex` and the mock-extsvc
 * envelope `digest32` are this SAME real ETH EIP-155 spec digest, which is
 * what cryptographically binds the two paired test replicas (docs/design/
 * testing.md §3.1/§3.2).
 */

/** keccak256(EIP155_RLP) — the real ETH EIP-155 spec signing digest. */
export const EIP155_DIGEST = 'daf5a779ae972f972197303d7b574746c7ef83eadac0f2791ad23db92e4c8e53'

/** The EIP-155 spec example signing RLP (its keccak256 == EIP155_DIGEST). */
export const EIP155_RLP = 'ec098504a817c800825208943535353535353535353535353535353535353535880de0b6b3a764000080018080'

/** pnet PSK shared by the relay config and every member subprocess. */
export const HARNESS_PSK_HEX = '0f0e0d0c0b0a09080706050403020100ffeeddccbbaa99887766554433221100'

/** EnvelopeVersionV1 / group provisioning version (internal/contract). */
export const ENVELOPE_VERSION_V1 = 1
