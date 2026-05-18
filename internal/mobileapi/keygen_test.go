package mobileapi

import (
	"context"
	"reflect"
	"testing"
)

// TestKeyGenInProcessAndCallbackOrder is the §B-001 acceptance core: a t-of-n
// keygen runs to completion through the flat string API in-process, the
// callbacks fire in the contracted order (progress* then exactly one result),
// and every share is sealed in the keystore and held in memory. It asserts on
// the single real keygen the package runs (sharedCommittee), so the heavy
// fast=false keygen happens exactly once for the whole suite.
func TestKeyGenInProcessAndCallbackOrder(t *testing.T) {
	c := sharedCommittee(t)

	order := c.order
	if len(order) < 2 || order[len(order)-1] != "result" {
		t.Fatalf("expected progress*…result, got %v", order)
	}
	for _, o := range order[:len(order)-1] {
		if o != "progress" {
			t.Fatalf("only progress may precede result, got %v", order)
		}
	}
	if len(c.progress) == 0 || c.progress[0] != "preparams" {
		t.Fatalf("first progress stage = %v, want preparams first", c.progress)
	}

	sum := c.summary
	if sum.Threshold != testThreshold || sum.Parties != testParties {
		t.Fatalf("summary t/n = %d/%d", sum.Threshold, sum.Parties)
	}
	if len(sum.Monikers) != testParties || sum.GroupPubKey == "" {
		t.Fatalf("bad summary: %+v", sum)
	}

	// Shares held in memory for in-process signing…
	shares, thr, ok := c.sdk.snapshotShares()
	if !ok || len(shares) != testParties || thr != testThreshold {
		t.Fatalf("snapshotShares ok=%v n=%d t=%d", ok, len(shares), thr)
	}
	// …and recoverable from the sealed keystore with the passphrase.
	got, err := c.sdk.store.Load(context.Background(), sum.Monikers[0], c.pw)
	if err != nil {
		t.Fatalf("keystore Load: %v", err)
	}
	if got.Moniker != sum.Monikers[0] || len(got.SaveData) == 0 {
		t.Fatalf("loaded share malformed: %q", got.Moniker)
	}

	// Every share converges on one master public key.
	hex0, err := groupPubHex(shares[0])
	if err != nil {
		t.Fatalf("pub: %v", err)
	}
	for _, sh := range shares[1:] {
		h, err := groupPubHex(sh)
		if err != nil || h != hex0 {
			t.Fatalf("share %s pub mismatch (%v)", sh.Moniker, err)
		}
	}
	if hex0 != sum.GroupPubKey {
		t.Fatalf("summary pub %s != derived %s", sum.GroupPubKey, hex0)
	}
}

func TestKeyGenRejectsBadConfig(t *testing.T) {
	sdk := newTestSDK(t)
	cases := []struct{ name, cfg string }{
		{"not json", `{`},
		{"t>=n", `{"threshold":3,"parties":3,"passphrase":"pw"}`},
		{"n<2", `{"threshold":1,"parties":1,"passphrase":"pw"}`},
		{"empty passphrase", `{"threshold":1,"parties":3,"passphrase":""}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRecorder()
			sdk.KeyGen(tc.cfg, kgAdapter{r})
			r.wait(t)
			code, _, _, _ := r.result()
			if code != CodeBadConfig {
				t.Fatalf("code=%q want %q", code, CodeBadConfig)
			}
			if reflect.DeepEqual(r.snapOrder(), []string{"progress"}) {
				t.Fatal("config rejection must not start work")
			}
		})
	}
}
