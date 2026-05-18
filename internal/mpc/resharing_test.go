package mpc

import (
	"context"
	"crypto/ecdsa"
	"math/big"
	"testing"
	"time"

	"github.com/bnb-chain/tss-lib/v3/common"
	"github.com/bnb-chain/tss-lib/v3/ecdsa/signing"
	"github.com/bnb-chain/tss-lib/v3/tss"
)

// makeOldCommittee runs an in-process keygen and serializes it to the Share
// form Reshare consumes, returning the shares plus the master public key.
func makeOldCommittee(t *testing.T, threshold, parties int) ([]Share, *big.Int, *big.Int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	saves, pids, err := simulateKeygen(ctx, threshold, parties, loadTestPreParams(t, parties), 0, true)
	if err != nil {
		t.Fatalf("simulateKeygen: %v", err)
	}
	shares := make([]Share, parties)
	for i, sd := range saves {
		bz, mErr := MarshalSaveData(sd)
		if mErr != nil {
			t.Fatalf("MarshalSaveData party %d: %v", i, mErr)
		}
		shares[i] = Share{Moniker: pids[i].Moniker, SaveData: bz}
	}
	pub := saves[0].ECDSAPub
	return shares, pub.X(), pub.Y()
}

// signWithCommittee runs an in-process signing round over the given committee
// and ECDSA-verifies the signature against (pubX, pubY). It proves the
// committee can sign for the unchanged master key (t-of-n liveness + address
// invariance). Signing itself lands in a sibling task; this is a test-only
// use of the upstream signing package, mirroring the reference resharing test.
func signWithCommittee(t *testing.T, shares []Share, threshold int, pubX, pubY *big.Int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Signing party IDs must be derived from the shares' embedded ShareIDs
	// (the same identity tss-lib persists), so they line up with each save
	// data's Ks — exactly how the old committee is reconstructed.
	pids, keys, _, err := reconstructOldCommittee(shares)
	if err != nil {
		t.Fatalf("reconstruct signing committee: %v", err)
	}
	n := len(pids)
	p2pCtx := tss.NewPeerContext(pids)
	outCh := make(chan tss.Message, n)
	endCh := make(chan *common.SignatureData, n)

	msg := big.NewInt(42)
	parties := make([]tss.Party, n)
	for i := 0; i < n; i++ {
		params := tss.NewParameters(tss.S256(), p2pCtx, pids[i], n, threshold)
		parties[i] = signing.NewLocalParty(msg, params, *keys[i], outCh, endCh)
	}

	sigs, err := runProtocol(ctx, parties, outCh, endCh)
	if err != nil {
		t.Fatalf("signing protocol: %v", err)
	}
	pk := ecdsa.PublicKey{Curve: tss.S256(), X: pubX, Y: pubY}
	for i, sig := range sigs {
		ok := ecdsa.Verify(&pk, msg.Bytes(),
			new(big.Int).SetBytes(sig.R), new(big.Int).SetBytes(sig.S))
		if !ok {
			t.Fatalf("ecdsa verify failed for signature %d (reshared key does not match master pub)", i)
		}
	}
}

func TestReshareSameCommittee(t *testing.T) {
	old, wantX, wantY := makeOldCommittee(t, testThreshold, testParties)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	saves, pids, err := simulateReshare(ctx, ReshareConfig{
		OldThreshold: testThreshold,
		OldShares:    old,
		NewThreshold: testThreshold,
		NewParties:   testParties,
		PreParams:    loadTestPreParams(t, testParties),
	}, true)
	if err != nil {
		t.Fatalf("simulateReshare: %v", err)
	}
	if len(saves) != testParties || len(pids) != testParties {
		t.Fatalf("expected %d new shares/pids, got %d/%d", testParties, len(saves), len(pids))
	}

	// Master public key — and every derived address — must be invariant.
	for i, sd := range saves {
		if sd.ECDSAPub.X().Cmp(wantX) != 0 || sd.ECDSAPub.Y().Cmp(wantY) != 0 {
			t.Fatalf("new party %d public key changed across resharing", i)
		}
	}

	// Old shares must be invalidated: resharing draws a fresh polynomial, so
	// the new share IDs are disjoint from the old ones.
	oldIDs := make(map[string]bool)
	for _, sh := range old {
		sd, _ := UnmarshalSaveData(sh.SaveData)
		oldIDs[sd.ShareID.String()] = true
	}
	for i, sd := range saves {
		if oldIDs[sd.ShareID.String()] {
			t.Fatalf("new party %d reuses an old share ID; old shares not rotated", i)
		}
	}
}

func TestReshareExpandCommittee(t *testing.T) {
	// 2-of-3 → 3-of-5: member-set change + expansion, public key preserved,
	// and the new committee can sign for the unchanged master key.
	old, wantX, wantY := makeOldCommittee(t, testThreshold, testParties)

	cfg := ReshareConfig{
		OldThreshold: testThreshold,
		OldShares:    old,
		NewThreshold: 2,
		NewParties:   5,
		PreParams:    loadTestPreParams(t, 5),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	saves, _, err := simulateReshare(ctx, cfg, true)
	if err != nil {
		t.Fatalf("simulateReshare: %v", err)
	}
	if len(saves) != 5 {
		t.Fatalf("expected 5 new shares, got %d", len(saves))
	}
	for i, sd := range saves {
		if sd.ECDSAPub.X().Cmp(wantX) != 0 || sd.ECDSAPub.Y().Cmp(wantY) != 0 {
			t.Fatalf("new party %d public key changed across resharing", i)
		}
	}

	newShares := make([]Share, len(saves))
	for i, sd := range saves {
		bz, mErr := MarshalSaveData(sd)
		if mErr != nil {
			t.Fatalf("MarshalSaveData new %d: %v", i, mErr)
		}
		newShares[i] = Share{Moniker: "N", SaveData: bz}
	}
	signWithCommittee(t, newShares, cfg.NewThreshold, wantX, wantY)
}

func TestReshareThresholdSubsetParticipates(t *testing.T) {
	// Only t+1 of the old committee need participate in resharing.
	old, wantX, wantY := makeOldCommittee(t, testThreshold, testParties)
	subset := old[:testThreshold+1]

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	saves, _, err := simulateReshare(ctx, ReshareConfig{
		OldThreshold: testThreshold,
		OldShares:    subset,
		NewThreshold: testThreshold,
		NewParties:   testParties,
		PreParams:    loadTestPreParams(t, testParties),
	}, true)
	if err != nil {
		t.Fatalf("simulateReshare (subset): %v", err)
	}
	for i, sd := range saves {
		if sd.ECDSAPub.X().Cmp(wantX) != 0 || sd.ECDSAPub.Y().Cmp(wantY) != 0 {
			t.Fatalf("new party %d public key changed across resharing", i)
		}
	}
}

func TestReshareReconstructionOrderIndependent(t *testing.T) {
	old, _, _ := makeOldCommittee(t, testThreshold, testParties)
	reversed := make([]Share, len(old))
	for i := range old {
		reversed[i] = old[len(old)-1-i]
	}

	a, _, _, err := reconstructOldCommittee(old)
	if err != nil {
		t.Fatalf("reconstruct (forward): %v", err)
	}
	b, _, _, err := reconstructOldCommittee(reversed)
	if err != nil {
		t.Fatalf("reconstruct (reversed): %v", err)
	}
	if len(a) != len(b) {
		t.Fatalf("committee size differs: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].KeyInt().Cmp(b[i].KeyInt()) != 0 {
			t.Fatalf("party %d differs under input reordering: reconstruction not deterministic", i)
		}
	}
}

func TestResharePublicAPI(t *testing.T) {
	old, wantX, wantY := makeOldCommittee(t, testThreshold, testParties)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	newShares, err := Reshare(ctx, ReshareConfig{
		OldThreshold: testThreshold,
		OldShares:    old,
		NewThreshold: testThreshold,
		NewParties:   testParties,
		PreParams:    loadTestPreParams(t, testParties),
	})
	if err != nil {
		t.Fatalf("Reshare: %v", err)
	}
	if len(newShares) != testParties {
		t.Fatalf("expected %d new shares, got %d", testParties, len(newShares))
	}
	for i, sh := range newShares {
		if sh.Moniker == "" || len(sh.SaveData) == 0 {
			t.Fatalf("new share %d malformed", i)
		}
		sd, uErr := UnmarshalSaveData(sh.SaveData)
		if uErr != nil {
			t.Fatalf("UnmarshalSaveData new %d: %v", i, uErr)
		}
		if sd.ECDSAPub.X().Cmp(wantX) != 0 || sd.ECDSAPub.Y().Cmp(wantY) != 0 {
			t.Fatalf("new share %d public key changed across resharing", i)
		}
	}
}

func TestSimulateReshareRejectsBadParams(t *testing.T) {
	old, _, _ := makeOldCommittee(t, testThreshold, testParties)
	ctx := context.Background()
	cases := []struct {
		name string
		cfg  ReshareConfig
	}{
		{"too few old shares", ReshareConfig{OldThreshold: 1, OldShares: old[:1], NewThreshold: 1, NewParties: 3}},
		{"old t+1 exceeds participants", ReshareConfig{OldThreshold: 5, OldShares: old, NewThreshold: 1, NewParties: 3}},
		{"new threshold not below n'", ReshareConfig{OldThreshold: 1, OldShares: old, NewThreshold: 3, NewParties: 3}},
		{"new threshold too low", ReshareConfig{OldThreshold: 1, OldShares: old, NewThreshold: 0, NewParties: 3}},
		{"too few new parties", ReshareConfig{OldThreshold: 1, OldShares: old, NewThreshold: 1, NewParties: 1}},
		{
			"preparams length mismatch",
			ReshareConfig{OldThreshold: 1, OldShares: old, NewThreshold: 1, NewParties: 3, PreParams: loadTestPreParams(t, 2)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := simulateReshare(ctx, tc.cfg, true); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}
