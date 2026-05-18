package mpc

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/royqta/mcp-wallet/internal/addr"
)

// uncompressedPub builds the 65-byte uncompressed secp256k1 public key
// (0x04 || X32 || Y32) the addr package expects, from a reshared committee's
// master public point. Address invariance is the K-002 acceptance beyond the
// raw public-key check, so the test derives real chain addresses from it.
func uncompressedPub(x, y *big.Int) []byte {
	out := make([]byte, 65)
	out[0] = 0x04
	x.FillBytes(out[1:33])
	y.FillBytes(out[33:65])
	return out
}

// chainAddrs derives the ETH/BSC/TRON triad for the master key; any change
// across recovery (which must not happen) surfaces as an address mismatch.
func chainAddrs(t *testing.T, x, y *big.Int) (string, string, string) {
	t.Helper()
	pub := uncompressedPub(x, y)
	eth, err := addr.ETHAddress(pub)
	if err != nil {
		t.Fatalf("ETHAddress: %v", err)
	}
	bsc, err := addr.BSCAddress(pub)
	if err != nil {
		t.Fatalf("BSCAddress: %v", err)
	}
	tron, err := addr.TronAddress(pub)
	if err != nil {
		t.Fatalf("TronAddress: %v", err)
	}
	return eth, bsc, tron
}

// TestRecoverLostMember is the K-002 acceptance: a 2-of-3 wallet loses one
// share (device lost); the two survivors recover redundancy back to 2-of-3;
// the master public key and all three chain addresses are unchanged; the old
// shares are invalidated; and the rebuilt committee can sign.
func TestRecoverLostMember(t *testing.T) {
	old, wantX, wantY := makeOldCommittee(t, testThreshold, testParties)
	wantETH, wantBSC, wantTRON := chainAddrs(t, wantX, wantY)

	// Simulate a lost device: drop share 0, keep the remaining t+1 (=2).
	survivors := old[1:]
	if len(survivors) != testThreshold+1 {
		t.Fatalf("expected %d survivors, got %d", testThreshold+1, len(survivors))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	rebuilt, err := RecoverLostMember(ctx, RecoverConfig{
		SurvivingShares: survivors,
		Threshold:       testThreshold,
		Parties:         testParties,
		PreParams:       loadTestPreParams(t, testParties),
	})
	if err != nil {
		t.Fatalf("RecoverLostMember: %v", err)
	}
	if len(rebuilt) != testParties {
		t.Fatalf("redundancy not restored: expected %d shares, got %d", testParties, len(rebuilt))
	}

	oldIDs := map[string]bool{}
	for _, sh := range old {
		sd, uErr := UnmarshalSaveData(sh.SaveData)
		if uErr != nil {
			t.Fatalf("UnmarshalSaveData old: %v", uErr)
		}
		oldIDs[sd.ShareID.String()] = true
	}
	for i, sh := range rebuilt {
		sd, uErr := UnmarshalSaveData(sh.SaveData)
		if uErr != nil {
			t.Fatalf("UnmarshalSaveData rebuilt %d: %v", i, uErr)
		}
		if sd.ECDSAPub.X().Cmp(wantX) != 0 || sd.ECDSAPub.Y().Cmp(wantY) != 0 {
			t.Fatalf("rebuilt share %d: master public key changed across recovery", i)
		}
		gotETH, gotBSC, gotTRON := chainAddrs(t, sd.ECDSAPub.X(), sd.ECDSAPub.Y())
		if gotETH != wantETH || gotBSC != wantBSC || gotTRON != wantTRON {
			t.Fatalf("rebuilt share %d: chain address changed across recovery\n eth %s->%s\n bsc %s->%s\n tron %s->%s",
				i, wantETH, gotETH, wantBSC, gotBSC, wantTRON, gotTRON)
		}
		if oldIDs[sd.ShareID.String()] {
			t.Fatalf("rebuilt share %d reuses an old share ID; lost share not invalidated", i)
		}
	}

	// The recovered committee must be able to sign for the unchanged key.
	signWithCommittee(t, rebuilt, testThreshold, wantX, wantY)
}

// TestRecoverLostMemberUnrecoverable: losing too many shares (survivors < t+1)
// is unrecoverable by design — there is no backend escrow.
func TestRecoverLostMemberUnrecoverable(t *testing.T) {
	old, _, _ := makeOldCommittee(t, testThreshold, testParties)
	ctx := context.Background()
	_, err := RecoverLostMember(ctx, RecoverConfig{
		SurvivingShares: old[:1], // 1 < t+1 = 2
		Threshold:       testThreshold,
		Parties:         testParties,
	})
	if err == nil {
		t.Fatal("expected unrecoverable error when survivors < threshold+1")
	}
}

func TestRecoverLostMemberRejectsBadParams(t *testing.T) {
	old, _, _ := makeOldCommittee(t, testThreshold, testParties)
	ctx := context.Background()
	cases := []struct {
		name string
		cfg  RecoverConfig
	}{
		{"threshold below 1", RecoverConfig{SurvivingShares: old, Threshold: 0, Parties: 3}},
		{"parties not above threshold", RecoverConfig{SurvivingShares: old, Threshold: 2, Parties: 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := RecoverLostMember(ctx, tc.cfg); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}
