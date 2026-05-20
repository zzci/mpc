package mpc

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"math/big"
	"sync"
	"testing"
	"time"

	btcecdsa "github.com/btcsuite/btcd/btcec/v2/ecdsa"

	"github.com/bnb-chain/tss-lib/v3/tss"

	"github.com/zzci/mpc/internal/hd"
)

// keygenShares runs an in-process 2-of-3 keygen (fast proofs, test fixtures)
// and returns the serialized shares plus the group master public key.
func keygenShares(t *testing.T) (shares []Share, pubX, pubY *big.Int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pre := loadTestPreParams(t, testParties)
	saves, pids, err := simulateKeygen(ctx, testThreshold, testParties, pre, 0, true)
	if err != nil {
		t.Fatalf("simulateKeygen: %v", err)
	}
	shares = make([]Share, testParties)
	for i, sd := range saves {
		bz, mErr := MarshalSaveData(sd)
		if mErr != nil {
			t.Fatalf("MarshalSaveData party %d: %v", i, mErr)
		}
		shares[i] = Share{Moniker: pids[i].Moniker, SaveData: bz}
	}
	return shares, saves[0].ECDSAPub.X(), saves[0].ECDSAPub.Y()
}

func randomDigest(t *testing.T) []byte {
	t.Helper()
	d := make([]byte, digestLen)
	if _, err := rand.Read(d); err != nil {
		t.Fatalf("rand digest: %v", err)
	}
	return d
}

// assertRecoversPub asserts the {R,S,V} signature recovers exactly the group
// master public key via secp256k1 ecrecover over the 32-byte digest.
func assertRecoversPub(t *testing.T, sig Signature, digest []byte, pubX, pubY *big.Int) {
	t.Helper()
	recovered, wasCompressed, err := btcecdsa.RecoverCompact(sig.Compact(), digest)
	if err != nil {
		t.Fatalf("ecrecover: %v", err)
	}
	if wasCompressed {
		t.Fatal("recovery produced a compressed key flag; expected uncompressed")
	}
	rk := recovered.ToECDSA()
	if rk.X.Cmp(pubX) != 0 || rk.Y.Cmp(pubY) != 0 {
		t.Fatalf("recovered pubkey != group master pubkey")
	}
}

// assertLowS asserts S is in the lower half of the curve order (BIP-0062
// canonical / low-S), which tss-lib's finalization round enforces.
func assertLowS(t *testing.T, sig Signature) {
	t.Helper()
	halfN := new(big.Int).Rsh(tss.S256().Params().N, 1)
	s := new(big.Int).SetBytes(sig.S[:])
	if s.Sign() == 0 {
		t.Fatal("S is zero")
	}
	if s.Cmp(halfN) > 0 {
		t.Fatalf("S is not low-S normalized: S > N/2")
	}
}

func TestSignInProcess(t *testing.T) {
	shares, pubX, pubY := keygenShares(t)
	digest := randomDigest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	sig, err := Sign(ctx, SignConfig{
		SessionID: "req-0001",
		Threshold: testThreshold,
		Shares:    shares[:testThreshold+1], // 2-of-3
		Digest:    digest,
	})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	assertLowS(t, sig)
	assertRecoversPub(t, sig, digest, pubX, pubY)

	// Cross-check with stdlib ECDSA verification against the master key.
	pk := ecdsa.PublicKey{Curve: tss.S256(), X: pubX, Y: pubY}
	r := new(big.Int).SetBytes(sig.R[:])
	s := new(big.Int).SetBytes(sig.S[:])
	if !ecdsa.Verify(&pk, digest, r, s) {
		t.Fatal("stdlib ecdsa.Verify failed for {R,S} against master pubkey")
	}
}

// TestSignConcurrentSessionsIsolated runs two signing sessions concurrently
// with distinct sessionIds, digests, and signer subsets, and asserts each
// signature is valid for its own digest — proving no cross-session message
// bleed (docs/design/mcp/sdk.md §5, contract/protocol.md §3).
func TestSignConcurrentSessionsIsolated(t *testing.T) {
	shares, pubX, pubY := keygenShares(t)

	type result struct {
		sig    Signature
		digest []byte
		err    error
	}

	run := func(sessionID string, subset []Share) result {
		digest := randomDigest(t)
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		defer cancel()
		sig, err := Sign(ctx, SignConfig{
			SessionID: sessionID,
			Threshold: testThreshold,
			Shares:    subset,
			Digest:    digest,
		})
		return result{sig: sig, digest: digest, err: err}
	}

	var wg sync.WaitGroup
	results := make([]result, 2)
	wg.Add(2)
	go func() { defer wg.Done(); results[0] = run("req-A", shares[0:2]) }()
	go func() { defer wg.Done(); results[1] = run("req-B", shares[1:3]) }()
	wg.Wait()

	for i, r := range results {
		if r.err != nil {
			t.Fatalf("session %d Sign: %v", i, r.err)
		}
		assertLowS(t, r.sig)
		assertRecoversPub(t, r.sig, r.digest, pubX, pubY)
	}
	// Distinct digests must yield distinct signatures (no shared state).
	if results[0].sig == results[1].sig {
		t.Fatal("two independent sessions produced an identical signature")
	}
}

func TestSignRejectsBadParams(t *testing.T) {
	shares, _, _ := keygenShares(t)
	ctx := context.Background()
	cases := []struct {
		name string
		cfg  SignConfig
	}{
		{"empty session id", SignConfig{SessionID: "", Threshold: testThreshold, Shares: shares[:2], Digest: make([]byte, 32)}},
		{"short digest", SignConfig{SessionID: "s", Threshold: testThreshold, Shares: shares[:2], Digest: make([]byte, 31)}},
		{"too few shares", SignConfig{SessionID: "s", Threshold: testThreshold, Shares: shares[:1], Digest: make([]byte, 32)}},
		{"threshold too low", SignConfig{SessionID: "s", Threshold: 0, Shares: shares[:2], Digest: make([]byte, 32)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Sign(ctx, tc.cfg); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

// TestSignKDD_RecoversChildPub proves the KDD signing wiring
// (address-derivation.md §6): a 2-of-3 group signs a 32-byte digest with a
// caller-supplied (KeyDerivationDelta, ChildPub) computed offline via
// internal/hd, and the recovered public key is exactly Q_child — NOT Q_master.
// The reverse check is also done: a master-key signature for the same digest
// must NOT recover to Q_child, so a regression that drops the delta is caught.
func TestSignKDD_RecoversChildPub(t *testing.T) {
	shares, pubX, pubY := keygenShares(t)
	digest := randomDigest(t)

	// Deterministic chaincode keeps the test hermetic; the real chaincode
	// is the post-DKG commit-reveal output (AD-2).
	chaincode := bytes.Repeat([]byte{0xc7}, hd.ChaincodeLen)
	masterPub := &ecdsa.PublicKey{Curve: tss.S256(), X: pubX, Y: pubY}

	const childIndex uint32 = 42
	il, childPub, err := hd.Derive(masterPub, chaincode, childIndex)
	if err != nil {
		t.Fatalf("hd.Derive: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	sig, err := Sign(ctx, SignConfig{
		SessionID:          "req-kdd-42",
		Threshold:          testThreshold,
		Shares:             shares[:testThreshold+1],
		Digest:             digest,
		ChildPub:           childPub,
		KeyDerivationDelta: il,
	})
	if err != nil {
		t.Fatalf("KDD Sign: %v", err)
	}
	assertLowS(t, sig)

	// The signature must recover to Q_child (the address-derivation.md §6
	// invariant: signing produces a signature for the derived child key, not
	// the group master key).
	assertRecoversPub(t, sig, digest, childPub.X, childPub.Y)

	// And it must NOT recover to Q_master — that would mean the KDD wiring
	// silently fell back to master signing.
	recovered, _, rerr := btcecdsa.RecoverCompact(sig.Compact(), digest)
	if rerr != nil {
		t.Fatalf("ecrecover: %v", rerr)
	}
	rk := recovered.ToECDSA()
	if rk.X.Cmp(pubX) == 0 && rk.Y.Cmp(pubY) == 0 {
		t.Fatal("KDD signature recovered to Q_master; should recover to Q_child")
	}

	// stdlib ecdsa verify against Q_child closes the loop.
	pk := ecdsa.PublicKey{Curve: tss.S256(), X: childPub.X, Y: childPub.Y}
	r := new(big.Int).SetBytes(sig.R[:])
	s := new(big.Int).SetBytes(sig.S[:])
	if !ecdsa.Verify(&pk, digest, r, s) {
		t.Fatal("stdlib ecdsa.Verify(child) failed for {R,S}")
	}
}

// TestSignKDD_DistinctIndicesDistinctChildren signs the same digest at two
// different child indices and verifies each recovers to its own Q_child, so a
// caller that drives multiple indices off one group never sees cross-index
// signature reuse.
func TestSignKDD_DistinctIndicesDistinctChildren(t *testing.T) {
	shares, pubX, pubY := keygenShares(t)
	digest := randomDigest(t)
	chaincode := bytes.Repeat([]byte{0x3e}, hd.ChaincodeLen)
	masterPub := &ecdsa.PublicKey{Curve: tss.S256(), X: pubX, Y: pubY}

	signAt := func(t *testing.T, index uint32, sessionID string) (Signature, *ecdsa.PublicKey) {
		t.Helper()
		il, childPub, err := hd.Derive(masterPub, chaincode, index)
		if err != nil {
			t.Fatalf("hd.Derive(%d): %v", index, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		defer cancel()
		sig, err := Sign(ctx, SignConfig{
			SessionID:          sessionID,
			Threshold:          testThreshold,
			Shares:             shares[:testThreshold+1],
			Digest:             digest,
			ChildPub:           childPub,
			KeyDerivationDelta: il,
		})
		if err != nil {
			t.Fatalf("KDD Sign(%d): %v", index, err)
		}
		return sig, childPub
	}

	sig0, child0 := signAt(t, 0, "req-kdd-0")
	sig1, child1 := signAt(t, 1, "req-kdd-1")

	if child0.X.Cmp(child1.X) == 0 && child0.Y.Cmp(child1.Y) == 0 {
		t.Fatal("Q_child(0) == Q_child(1); HD derivation is degenerate")
	}
	if sig0 == sig1 {
		t.Fatal("two child indices produced an identical signature for the same digest")
	}
	assertRecoversPub(t, sig0, digest, child0.X, child0.Y)
	assertRecoversPub(t, sig1, digest, child1.X, child1.Y)
}

// TestSignKDD_RejectsHalfSetDelta proves the API guard catches a caller bug
// where only one of {ChildPub, KeyDerivationDelta} is set. Both forms must
// be rejected; otherwise the signer would silently sign against the master
// key while the caller assumed child signing.
func TestSignKDD_RejectsHalfSetDelta(t *testing.T) {
	shares, pubX, pubY := keygenShares(t)
	digest := randomDigest(t)
	cases := []struct {
		name string
		cfg  SignConfig
	}{
		{
			"ChildPub set, delta nil",
			SignConfig{
				SessionID: "req", Threshold: testThreshold, Shares: shares[:2], Digest: digest,
				ChildPub: &ecdsa.PublicKey{Curve: tss.S256(), X: pubX, Y: pubY},
			},
		},
		{
			"delta set, ChildPub nil",
			SignConfig{
				SessionID: "req", Threshold: testThreshold, Shares: shares[:2], Digest: digest,
				KeyDerivationDelta: big.NewInt(1),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Sign(context.Background(), tc.cfg); err == nil {
				t.Fatal("expected error for half-set KDD config")
			}
		})
	}
}

func TestSignRejectsCorruptShare(t *testing.T) {
	shares, _, _ := keygenShares(t)
	bad := []Share{
		shares[0],
		{Moniker: shares[1].Moniker, SaveData: []byte("not json")},
	}
	_, err := Sign(context.Background(), SignConfig{
		SessionID: "req-corrupt",
		Threshold: testThreshold,
		Shares:    bad,
		Digest:    make([]byte, 32),
	})
	if err == nil {
		t.Fatal("expected error for corrupt share save data")
	}
}
