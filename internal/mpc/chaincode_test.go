package mpc

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

// AD-2 unit gates for the chaincode commit-reveal helpers
// (docs/design/mcp/address-derivation.md §3). The network driver lives in
// internal/cli; these tests pin the math so a regression in the primitive
// surfaces here before any transport carrier can mask it.

// fixedReveal is a deterministic 32-byte reveal value used across tests so
// that "same inputs ⇒ same chaincode" can be asserted byte-for-byte.
func fixedReveal(b byte) []byte {
	out := make([]byte, ChaincodeRandLen)
	for i := range out {
		out[i] = b
	}
	return out
}

func TestChaincodeCommit_DeterministicAndBoundToInputs(t *testing.T) {
	r := fixedReveal(0xAA)
	c1, err := ChaincodeCommit("grp-1", 1, r)
	if err != nil {
		t.Fatalf("ChaincodeCommit: %v", err)
	}
	if len(c1) != ChaincodeCommitLen {
		t.Fatalf("commitment length = %d, want %d", len(c1), ChaincodeCommitLen)
	}
	// Same inputs → identical commitment.
	c2, _ := ChaincodeCommit("grp-1", 1, r)
	if !bytes.Equal(c1, c2) {
		t.Fatal("ChaincodeCommit is not deterministic for identical inputs")
	}
	// Different group → different commitment (§3 cross-group binding).
	cOther, _ := ChaincodeCommit("grp-2", 1, r)
	if bytes.Equal(c1, cOther) {
		t.Fatal("group_id is not folded into the commitment preimage")
	}
	// Different party index → different commitment.
	cIdx, _ := ChaincodeCommit("grp-1", 2, r)
	if bytes.Equal(c1, cIdx) {
		t.Fatal("party index is not folded into the commitment preimage")
	}
	// Different randomness → different commitment.
	cR, _ := ChaincodeCommit("grp-1", 1, fixedReveal(0xBB))
	if bytes.Equal(c1, cR) {
		t.Fatal("randomness is not folded into the commitment preimage")
	}
}

func TestChaincodeCommit_RejectsMalformedRandomness(t *testing.T) {
	cases := []struct {
		name string
		r    []byte
	}{
		{"empty", nil},
		{"short", make([]byte, ChaincodeRandLen-1)},
		{"long", make([]byte, ChaincodeRandLen+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ChaincodeCommit("grp", 1, tc.r); err == nil {
				t.Fatal("ChaincodeCommit accepted malformed randomness; want error")
			}
		})
	}
}

func TestChaincodeCommit_RejectsZeroPartyIndex(t *testing.T) {
	if _, err := ChaincodeCommit("grp", 0, fixedReveal(0x01)); err == nil {
		t.Fatal("ChaincodeCommit accepted party index 0; want 1-based error")
	}
}

func TestVerifyChaincodeCommit_MatchAndMismatch(t *testing.T) {
	r := fixedReveal(0x33)
	good, _ := ChaincodeCommit("g", 1, r)
	if err := VerifyChaincodeCommit("g", 1, r, good); err != nil {
		t.Fatalf("VerifyChaincodeCommit (match): %v", err)
	}

	// Tampering with the reveal must fail verification.
	tampered := append([]byte(nil), r...)
	tampered[0] ^= 0xFF
	if err := VerifyChaincodeCommit("g", 1, tampered, good); err == nil {
		t.Fatal("VerifyChaincodeCommit accepted a tampered reveal")
	}
	// Tampering with the commitment must fail verification.
	bad := append([]byte(nil), good...)
	bad[0] ^= 0xFF
	if err := VerifyChaincodeCommit("g", 1, r, bad); err == nil {
		t.Fatal("VerifyChaincodeCommit accepted a tampered commitment")
	}
	// Wrong group binding must fail (cross-group replay defence).
	if err := VerifyChaincodeCommit("h", 1, r, good); err == nil {
		t.Fatal("VerifyChaincodeCommit accepted a wrong group_id")
	}
	// Wrong party index must fail.
	if err := VerifyChaincodeCommit("g", 2, r, good); err == nil {
		t.Fatal("VerifyChaincodeCommit accepted a wrong party index")
	}
}

func TestVerifyChaincodeCommit_RejectsMalformedCommitment(t *testing.T) {
	r := fixedReveal(0x33)
	if err := VerifyChaincodeCommit("g", 1, r, make([]byte, ChaincodeCommitLen-1)); err == nil {
		t.Fatal("VerifyChaincodeCommit accepted a short commitment; want error")
	}
}

func TestDeriveChaincode_DeterministicAnd32Bytes(t *testing.T) {
	rs := [][]byte{fixedReveal(0x01), fixedReveal(0x02), fixedReveal(0x03)}
	c1, err := DeriveChaincode("grp", rs)
	if err != nil {
		t.Fatalf("DeriveChaincode: %v", err)
	}
	if len(c1) != ChaincodeLen {
		t.Fatalf("chaincode length = %d, want %d", len(c1), ChaincodeLen)
	}
	c2, _ := DeriveChaincode("grp", rs)
	if !bytes.Equal(c1, c2) {
		t.Fatal("DeriveChaincode is not deterministic for identical inputs")
	}
}

func TestDeriveChaincode_GroupBoundSalt(t *testing.T) {
	rs := [][]byte{fixedReveal(0x01), fixedReveal(0x02)}
	a, _ := DeriveChaincode("alpha", rs)
	b, _ := DeriveChaincode("beta", rs)
	if bytes.Equal(a, b) {
		t.Fatal("DeriveChaincode does not fold group_id into the HKDF salt")
	}
}

func TestDeriveChaincode_OrderSensitive(t *testing.T) {
	r1, r2 := fixedReveal(0x10), fixedReveal(0x20)
	a, _ := DeriveChaincode("g", [][]byte{r1, r2})
	b, _ := DeriveChaincode("g", [][]byte{r2, r1})
	if bytes.Equal(a, b) {
		t.Fatal("DeriveChaincode IKM is not order-sensitive (party order must matter)")
	}
}

func TestDeriveChaincode_RejectsBadInput(t *testing.T) {
	t.Run("too few parties", func(t *testing.T) {
		if _, err := DeriveChaincode("g", [][]byte{fixedReveal(1)}); err == nil {
			t.Fatal("DeriveChaincode accepted a single-party input; want error")
		}
	})
	t.Run("malformed party randomness", func(t *testing.T) {
		bad := [][]byte{fixedReveal(1), make([]byte, ChaincodeRandLen-1)}
		_, err := DeriveChaincode("g", bad)
		if err == nil {
			t.Fatal("DeriveChaincode accepted a short randomness slice; want error")
		}
		if !strings.Contains(err.Error(), "randomness[1]") {
			t.Fatalf("error %q does not name the offending index", err.Error())
		}
	})
}

func TestGenerateChaincodeRandomness_LengthAndNotConstant(t *testing.T) {
	a, err := GenerateChaincodeRandomness()
	if err != nil {
		t.Fatalf("GenerateChaincodeRandomness: %v", err)
	}
	if len(a) != ChaincodeRandLen {
		t.Fatalf("randomness length = %d, want %d", len(a), ChaincodeRandLen)
	}
	b, _ := GenerateChaincodeRandomness()
	if bytes.Equal(a, b) {
		t.Fatal("GenerateChaincodeRandomness returned two equal values (entropy broken)")
	}
}

func TestChaincode_EndToEndAgreement(t *testing.T) {
	// Simulate a 3-party round in-memory: each party generates r_j, commits,
	// reveals; every party then verifies all C_j and derives c. All three
	// parties MUST end on the same 32-byte chaincode (the network-driver
	// guarantee, modeled here without transport).
	const n = 3
	groupID := "wallet-ut"
	rs := make([][]byte, n)
	commits := make([][]byte, n)
	for j := 0; j < n; j++ {
		r, err := GenerateChaincodeRandomness()
		if err != nil {
			t.Fatalf("rand %d: %v", j, err)
		}
		c, err := ChaincodeCommit(groupID, uint32(j+1), r)
		if err != nil {
			t.Fatalf("commit %d: %v", j, err)
		}
		rs[j] = r
		commits[j] = c
	}
	// Each party verifies every other party's (reveal, commit) pair.
	for verifier := 0; verifier < n; verifier++ {
		for j := 0; j < n; j++ {
			if err := VerifyChaincodeCommit(groupID, uint32(j+1), rs[j], commits[j]); err != nil {
				t.Fatalf("verifier %d rejected party %d: %v", verifier, j, err)
			}
		}
	}
	// Each party derives c independently and they must all agree.
	var want []byte
	for verifier := 0; verifier < n; verifier++ {
		got, err := DeriveChaincode(groupID, rs)
		if err != nil {
			t.Fatalf("verifier %d derive: %v", verifier, err)
		}
		if verifier == 0 {
			want = got
			continue
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("verifier %d derived %s, want %s", verifier, hex.EncodeToString(got), hex.EncodeToString(want))
		}
	}
	if len(want) != ChaincodeLen {
		t.Fatalf("derived chaincode length = %d, want %d", len(want), ChaincodeLen)
	}
}

// TestChaincode_AbortOnEquivocation models the §3 strict-abort step 5: if any
// party's revealed r_j fails to recompute its broadcast commitment, the
// derivation is rejected before any group artefact is written. Here we
// simulate a malicious party that broadcasts C_j honestly but then reveals a
// different r_j' than the one it committed to; an honest verifier MUST refuse
// to proceed, regardless of the values' otherwise-valid 32-byte length.
func TestChaincode_AbortOnEquivocation(t *testing.T) {
	groupID := "g"
	honestR := fixedReveal(0x77)
	honestC, _ := ChaincodeCommit(groupID, 2, honestR)
	maliciousR := fixedReveal(0x88) // != honestR but still 32 bytes
	if err := VerifyChaincodeCommit(groupID, 2, maliciousR, honestC); err == nil {
		t.Fatal("verifier accepted an equivocating reveal; want abort")
	}
}
