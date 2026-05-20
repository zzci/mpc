package hd

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"math/big"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"

	tssCrypto "github.com/bnb-chain/tss-lib/v3/crypto"
	"github.com/bnb-chain/tss-lib/v3/tss"

	"github.com/zzci/mpc/internal/addr"
)

// scalarPub returns the secp256k1 public key for the scalar n (a small
// positive integer), giving a deterministic master key for the tests. Routes
// through tss-lib's crypto.ScalarBaseMult wrapper so the test does not call
// the deprecated elliptic.Curve.ScalarBaseMult directly.
func scalarPub(t *testing.T, n int64) *ecdsa.PublicKey {
	t.Helper()
	p := tssCrypto.ScalarBaseMult(tss.S256(), new(big.Int).SetInt64(n))
	return p.ToECDSAPubKey()
}

// addG returns base + k·G on secp256k1, via tss-lib's ECPoint helpers (so the
// deprecated curve.{Add,ScalarBaseMult} calls stay inside one audited place).
func addG(t *testing.T, base *ecdsa.PublicKey, k *big.Int) *ecdsa.PublicKey {
	t.Helper()
	curve := tss.S256()
	gK := tssCrypto.ScalarBaseMult(curve, k)
	bP, err := tssCrypto.NewECPoint(curve, base.X, base.Y)
	if err != nil {
		t.Fatalf("NewECPoint(base): %v", err)
	}
	sum, err := bP.Add(gK)
	if err != nil {
		t.Fatalf("ECPoint.Add: %v", err)
	}
	return sum.ToECDSAPubKey()
}

// pubsEqual compares two secp256k1 affine points by (X, Y).
func pubsEqual(a, b *ecdsa.PublicKey) bool {
	return a != nil && b != nil &&
		a.X != nil && b.X != nil && a.X.Cmp(b.X) == 0 &&
		a.Y != nil && b.Y != nil && a.Y.Cmp(b.Y) == 0
}

func TestDerive_RecomputesQchild(t *testing.T) {
	// Q_master = 7·G; chaincode = deterministic fill so the test is hermetic.
	master := scalarPub(t, 7)
	cc := bytes.Repeat([]byte{0x5a}, ChaincodeLen)

	il, child, err := Derive(master, cc, 42)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	if il == nil || il.Sign() == 0 {
		t.Fatal("IL must be non-zero")
	}
	if il.Cmp(tss.S256().Params().N) >= 0 {
		t.Fatal("IL must be < N(secp256k1)")
	}

	// Independent recomputation: Q_child must equal Q_master + IL·G.
	expected := addG(t, master, il)
	if !pubsEqual(child, expected) {
		t.Fatalf("Derive returned Q_child != Q_master + IL·G")
	}
}

func TestDerive_DistinctIndicesProduceDistinctChildren(t *testing.T) {
	master := scalarPub(t, 11)
	cc := bytes.Repeat([]byte{0xab}, ChaincodeLen)

	seen := make(map[string]struct{}, 16)
	for i := uint32(0); i < 16; i++ {
		_, child, err := Derive(master, cc, i)
		if err != nil {
			t.Fatalf("Derive(%d): %v", i, err)
		}
		key := child.X.String() + ":" + child.Y.String()
		if _, dup := seen[key]; dup {
			t.Fatalf("Derive collision at index %d", i)
		}
		seen[key] = struct{}{}
	}
}

func TestDerive_IndexBoundary(t *testing.T) {
	master := scalarPub(t, 3)
	cc := bytes.Repeat([]byte{0x01}, ChaincodeLen)

	// 2^31 - 1 (last legal non-hardened index) must succeed; 2^31 must reject.
	if _, _, err := Derive(master, cc, MaxIndex-1); err != nil {
		t.Fatalf("Derive(MaxIndex-1): unexpected error %v", err)
	}
	_, _, err := Derive(master, cc, MaxIndex)
	if !errors.Is(err, ErrIndexOutOfRange) {
		t.Fatalf("Derive(MaxIndex): want ErrIndexOutOfRange, got %v", err)
	}
}

func TestDerive_RejectsBadInputs(t *testing.T) {
	master := scalarPub(t, 5)
	cases := []struct {
		name      string
		pub       *ecdsa.PublicKey
		chaincode []byte
		index     uint32
	}{
		{"nil master", nil, bytes.Repeat([]byte{1}, ChaincodeLen), 0},
		{"master no X", &ecdsa.PublicKey{Curve: tss.S256()}, bytes.Repeat([]byte{1}, ChaincodeLen), 0},
		{"chaincode too short", master, make([]byte, ChaincodeLen-1), 0},
		{"chaincode too long", master, make([]byte, ChaincodeLen+1), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := Derive(tc.pub, tc.chaincode, tc.index); err == nil {
				t.Fatalf("Derive(%s): expected error", tc.name)
			}
		})
	}
}

func TestDerive_BindsToChaincode(t *testing.T) {
	master := scalarPub(t, 17)
	cc1 := bytes.Repeat([]byte{0x11}, ChaincodeLen)
	cc2 := bytes.Repeat([]byte{0x22}, ChaincodeLen)

	_, c1, err := Derive(master, cc1, 0)
	if err != nil {
		t.Fatalf("Derive cc1: %v", err)
	}
	_, c2, err := Derive(master, cc2, 0)
	if err != nil {
		t.Fatalf("Derive cc2: %v", err)
	}
	if pubsEqual(c1, c2) {
		t.Fatal("changing the chaincode must change the child key")
	}
}

func TestDerive_BIP32BindingFormat(t *testing.T) {
	// Cross-check Derive against an independent computation of BIP-32's
	// non-hardened formula: IL = HMAC-SHA512(chaincode, compressed(Q) ‖ i_be32)[:32],
	// implemented here in 9 lines so a future reviewer can read the contract
	// against the design (address-derivation.md §2) without leaving the test
	// file.
	master := scalarPub(t, 23)
	cc := bytes.Repeat([]byte{0x73}, ChaincodeLen)
	const idx uint32 = 0xCAFE

	il, child, err := Derive(master, cc, idx)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}

	// Re-compute IL with a hand-rolled HMAC-SHA512 over compressed(master) ‖ idx_be32.
	pubKey, perr := btcec.ParsePubKey(elementaryUncompressed(master))
	if perr != nil {
		t.Fatalf("btcec parse: %v", perr)
	}
	compressed := pubKey.SerializeCompressed()
	if len(compressed) != 33 {
		t.Fatalf("compressed pub: want 33 bytes, got %d", len(compressed))
	}
	var idxBE [4]byte
	binary.BigEndian.PutUint32(idxBE[:], idx)

	expectedIL := hmacSHA512Left32(cc, append(append([]byte{}, compressed...), idxBE[:]...))
	if expectedIL.Cmp(il) != 0 {
		t.Fatalf("IL mismatch: got %x, want %x", il.Bytes(), expectedIL.Bytes())
	}

	// And the returned child must equal Q_master + IL·G.
	expected := addG(t, master, il)
	if !pubsEqual(child, expected) {
		t.Fatal("child != Q_master + IL·G")
	}
}

func TestChildPubBytes_FeedsAddr(t *testing.T) {
	master := scalarPub(t, 91)
	cc := make([]byte, ChaincodeLen)
	if _, err := rand.Read(cc); err != nil {
		t.Fatalf("rand: %v", err)
	}
	_, child, err := Derive(master, cc, 1)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	pub := ChildPubBytes(child)
	if len(pub) != 65 || pub[0] != 0x04 {
		t.Fatalf("ChildPubBytes: bad uncompressed form")
	}
	// internal/addr must accept the bytes without error for all three chains.
	if _, err := addr.ETHAddress(pub); err != nil {
		t.Fatalf("ETHAddress: %v", err)
	}
	if _, err := addr.BSCAddress(pub); err != nil {
		t.Fatalf("BSCAddress: %v", err)
	}
	if _, err := addr.TronAddress(pub); err != nil {
		t.Fatalf("TronAddress: %v", err)
	}
}

func TestChildPubBytes_NilGuards(t *testing.T) {
	if ChildPubBytes(nil) != nil {
		t.Fatal("ChildPubBytes(nil) must be nil")
	}
	if ChildPubBytes(&ecdsa.PublicKey{Curve: tss.S256()}) != nil {
		t.Fatal("ChildPubBytes with empty X/Y must be nil")
	}
}

// elementaryUncompressed returns the 65-byte (0x04 ‖ X ‖ Y) form of pub. Kept
// inline (rather than reusing ChildPubBytes) so the binding test does not
// circular-validate the helper it is verifying.
func elementaryUncompressed(pub *ecdsa.PublicKey) []byte {
	out := make([]byte, 65)
	out[0] = 0x04
	pub.X.FillBytes(out[1:33])
	pub.Y.FillBytes(out[33:65])
	return out
}

// hmacSHA512Left32 returns the first 32 bytes of HMAC-SHA512(key, data) as a
// *big.Int, matching the BIP-32 "IL" derivation. Local to this test file so
// the package under test never re-exports a low-level hash construction it
// does not need (only Derive is the public contract).
func hmacSHA512Left32(key, data []byte) *big.Int {
	h := hmac.New(sha512.New, key)
	h.Write(data)
	mac := h.Sum(nil)
	return new(big.Int).SetBytes(mac[:32])
}
