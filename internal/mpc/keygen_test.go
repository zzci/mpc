package mpc

import (
	"context"
	"testing"
	"time"

	"github.com/bnb-chain/tss-lib/v3/ecdsa/keygen"
)

const (
	testParties   = 3
	testThreshold = 1 // 2-of-3
)

// loadTestPreParams reuses tss-lib's bundled keygen fixtures purely as a
// source of pre-computed safe primes, so tests don't pay the multi-minute
// safe-prime search. The pre-params are independent of (t, n); only the safe
// primes / Paillier key are taken.
func loadTestPreParams(t *testing.T, n int) []keygen.LocalPreParams {
	t.Helper()
	fixtures, _, err := keygen.LoadKeygenTestFixtures(n)
	if err != nil {
		t.Fatalf("load keygen fixtures (run tss-lib keygen tests to generate them): %v", err)
	}
	pre := make([]keygen.LocalPreParams, n)
	for i := 0; i < n; i++ {
		pre[i] = fixtures[i].LocalPreParams
	}
	return pre
}

func TestSimulateKeygenInProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pre := loadTestPreParams(t, testParties)

	saves, pids, err := simulateKeygen(ctx, testThreshold, testParties, pre, 0, true)
	if err != nil {
		t.Fatalf("simulateKeygen: %v", err)
	}
	if len(saves) != testParties {
		t.Fatalf("expected %d save data, got %d", testParties, len(saves))
	}
	if len(pids) != testParties {
		t.Fatalf("expected %d party IDs, got %d", testParties, len(pids))
	}

	// Every party must converge on the same ECDSA public key.
	want := saves[0].ECDSAPub
	if want == nil {
		t.Fatal("party 0 produced nil ECDSAPub")
	}
	for i, sd := range saves {
		if sd == nil {
			t.Fatalf("party %d produced nil save data", i)
		}
		if sd.ECDSAPub.X().Cmp(want.X()) != 0 || sd.ECDSAPub.Y().Cmp(want.Y()) != 0 {
			t.Fatalf("party %d public key mismatch", i)
		}
	}

	// Save data must be non-empty and survive a serialize round trip with
	// the public key preserved.
	for i, sd := range saves {
		bz, err := MarshalSaveData(sd)
		if err != nil {
			t.Fatalf("MarshalSaveData party %d: %v", i, err)
		}
		if len(bz) == 0 {
			t.Fatalf("party %d serialized save data is empty", i)
		}
		got, err := UnmarshalSaveData(bz)
		if err != nil {
			t.Fatalf("UnmarshalSaveData party %d: %v", i, err)
		}
		if got.ECDSAPub.X().Cmp(want.X()) != 0 || got.ECDSAPub.Y().Cmp(want.Y()) != 0 {
			t.Fatalf("party %d public key not preserved across round trip", i)
		}
		if got.Xi == nil || got.Xi.Sign() == 0 {
			t.Fatalf("party %d share secret Xi lost across round trip", i)
		}
	}
}

func TestKeygenPublicAPI(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cfg := KeygenConfig{
		Threshold: testThreshold,
		Parties:   testParties,
		PreParams: loadTestPreParams(t, testParties),
	}
	shares, err := Keygen(ctx, cfg)
	if err != nil {
		t.Fatalf("Keygen: %v", err)
	}
	if len(shares) != testParties {
		t.Fatalf("expected %d shares, got %d", testParties, len(shares))
	}

	var wantX, wantY string
	for i, sh := range shares {
		if sh.Moniker == "" {
			t.Fatalf("share %d has empty moniker", i)
		}
		if len(sh.SaveData) == 0 {
			t.Fatalf("share %d has empty save data", i)
		}
		sd, err := UnmarshalSaveData(sh.SaveData)
		if err != nil {
			t.Fatalf("UnmarshalSaveData share %d: %v", i, err)
		}
		x, y := sd.ECDSAPub.X().String(), sd.ECDSAPub.Y().String()
		if i == 0 {
			wantX, wantY = x, y
			continue
		}
		if x != wantX || y != wantY {
			t.Fatalf("share %d public key mismatch", i)
		}
	}
}

func TestSimulateKeygenRejectsBadParams(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name      string
		threshold int
		parties   int
	}{
		{"too few parties", 1, 1},
		{"threshold too low", 0, 3},
		{"threshold not below n", 3, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := simulateKeygen(ctx, tc.threshold, tc.parties, nil, 0, true); err == nil {
				t.Fatalf("expected error for t=%d n=%d", tc.threshold, tc.parties)
			}
		})
	}
}
