package mpc

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"math/big"
	"sync"
	"testing"
	"time"

	btcecdsa "github.com/btcsuite/btcd/btcec/v2/ecdsa"

	"github.com/bnb-chain/tss-lib/v3/tss"
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
