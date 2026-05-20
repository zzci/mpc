package mpc

import (
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

// AD-2 chaincode commit-reveal: a transport-neutral implementation of the
// post-DKG one-shot protocol that produces the 32-byte HD chaincode c
// (docs/design/mcp/address-derivation.md §3). The functions here are pure
// primitives (no network, no goroutines, no time); the network driver lives in
// internal/cli/chaincode.go and feeds verified peer payloads through them.
//
// Design invariants pinned here so a reviewer can read them once:
//   - DST_CR / DST_CC are fixed domain-separation byte strings; never reformat.
//   - group_id is folded into BOTH the commitment preimage and the HKDF salt,
//     so a commit-reveal transcript is bound to one group and cannot be
//     replayed across groups (§3 binding-uniqueness).
//   - r_j is 32 bytes; c is 32 bytes; commitments are 32 bytes — the lengths
//     ARE the schema. A wrong-length input is rejected here, never papered
//     over.
//   - The party index in the commitment preimage is 1-based (j ∈ [1..n]),
//     matching the design's textual numbering; the network driver maps the
//     0-based device index it carries to 1-based at the boundary.

const (
	// DSTChaincodeCommit is the commitment-hash domain-separation tag
	// (address-derivation.md §3 step 1). The exact byte sequence is part of
	// the wire protocol — changing it breaks interop with any other
	// implementation following the same design.
	DSTChaincodeCommit = "mcp/v1/chaincode-commit"

	// DSTChaincodeDerive is the HKDF-salt domain-separation tag
	// (address-derivation.md §3 step 4).
	DSTChaincodeDerive = "mcp/v1/chaincode-derive"

	// ChaincodeLen is the fixed byte length of the derived HD chaincode
	// (address-derivation.md §3 step 4: HKDF L=32). The coorddb schema
	// CHECK and ProvisionGroup application-layer guard enforce the same
	// length on the storage side (internal/server/coorddb).
	ChaincodeLen = 32

	// ChaincodeCommitLen is the byte length of a commitment value
	// SHA-256(DST_CR ‖ group_id ‖ j_be32 ‖ r_j).
	ChaincodeCommitLen = sha256.Size

	// ChaincodeRandLen is the byte length of each party's local randomness
	// r_j. 32 bytes gives 256-bit entropy contribution (§3 step 1).
	ChaincodeRandLen = 32
)

// ChaincodeCommit computes C_j = SHA-256(DST_CR ‖ group_id ‖ j_be32 ‖ r_j)
// per address-derivation.md §3 step 1. partyIndex1Based is the design's j
// (1..n); randomness must be exactly ChaincodeRandLen bytes (a malformed length
// is rejected here rather than padded/truncated so a buggy caller fails
// fast).
func ChaincodeCommit(groupID string, partyIndex1Based uint32, randomness []byte) ([]byte, error) {
	if len(randomness) != ChaincodeRandLen {
		return nil, fmt.Errorf("mpc: chaincode randomness must be %d bytes, got %d", ChaincodeRandLen, len(randomness))
	}
	if partyIndex1Based == 0 {
		return nil, fmt.Errorf("mpc: chaincode party index must be 1-based and non-zero")
	}
	h := sha256.New()
	h.Write([]byte(DSTChaincodeCommit))
	h.Write([]byte(groupID))
	var idx [4]byte
	binary.BigEndian.PutUint32(idx[:], partyIndex1Based)
	h.Write(idx[:])
	h.Write(randomness)
	return h.Sum(nil), nil
}

// VerifyChaincodeCommit recomputes the commitment and constant-time compares
// it against the broadcast value (address-derivation.md §3 step 3). Returns
// nil on match and a descriptive error on mismatch; the network driver
// surfaces any error as a strict abort (§3 step 5: any failure → abort the
// whole init, no partial success).
func VerifyChaincodeCommit(groupID string, partyIndex1Based uint32, randomness, commitment []byte) error {
	if len(commitment) != ChaincodeCommitLen {
		return fmt.Errorf("mpc: chaincode commitment must be %d bytes, got %d", ChaincodeCommitLen, len(commitment))
	}
	expect, err := ChaincodeCommit(groupID, partyIndex1Based, randomness)
	if err != nil {
		return err
	}
	if !hmac.Equal(expect, commitment) {
		return fmt.Errorf("mpc: chaincode commitment mismatch for party %d", partyIndex1Based)
	}
	return nil
}

// DeriveChaincode computes c = HKDF-SHA256(salt = DST_CC ‖ group_id,
// ikm = r_1 ‖ … ‖ r_n, info = "", L = 32) per address-derivation.md §3 step 4.
// randomness MUST contain the per-party 32-byte reveals in 1..n order
// (the network driver assembles them in party-tag order); a missing or
// malformed entry aborts the whole derivation.
func DeriveChaincode(groupID string, randomness [][]byte) ([]byte, error) {
	if len(randomness) < 2 {
		return nil, fmt.Errorf("mpc: chaincode needs at least 2 parties' randomness, got %d", len(randomness))
	}
	ikm := make([]byte, 0, len(randomness)*ChaincodeRandLen)
	for i, r := range randomness {
		if len(r) != ChaincodeRandLen {
			return nil, fmt.Errorf("mpc: chaincode randomness[%d] must be %d bytes, got %d", i, ChaincodeRandLen, len(r))
		}
		ikm = append(ikm, r...)
	}
	salt := make([]byte, 0, len(DSTChaincodeDerive)+len(groupID))
	salt = append(salt, []byte(DSTChaincodeDerive)...)
	salt = append(salt, []byte(groupID)...)
	reader := hkdf.New(sha256.New, ikm, salt, nil)
	out := make([]byte, ChaincodeLen)
	if _, err := io.ReadFull(reader, out); err != nil {
		return nil, fmt.Errorf("mpc: hkdf-sha256 derive chaincode: %w", err)
	}
	return out, nil
}

// GenerateChaincodeRandomness returns ChaincodeRandLen bytes from crypto/rand.
// Production code paths must use this (never a deterministic seed): the
// security argument of §3 step 1 relies on each party drawing fresh entropy
// the others cannot predict.
func GenerateChaincodeRandomness() ([]byte, error) {
	b := make([]byte, ChaincodeRandLen)
	if _, err := io.ReadFull(cryptorand.Reader, b); err != nil {
		return nil, fmt.Errorf("mpc: read randomness: %w", err)
	}
	return b, nil
}
