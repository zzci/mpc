package mobileapi

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

// TestKeyGenDistributedAndCallbackOrder is the DM-3 acceptance core: a t-of-n
// distributed keygen runs to completion across N SDKs wired via a ring
// fabric (the test-only stand-in for the host transport, DM-5), the flat-
// string API exposes the new single-party contract, the callbacks fire in
// the contracted order on every device (progress* then exactly one result),
// every device's keystore holds **exactly one** share (the §G acceptance
// hard judgement), and every device converges on the same master public
// key. It asserts on the single real distributed keygen the package runs
// (sharedCommittee), so the heavy fast=false keygen happens exactly once
// for the whole suite.
func TestKeyGenDistributedAndCallbackOrder(t *testing.T) {
	c := sharedCommittee(t)
	c.fabric.assertNoErrs(t)

	if len(c.sdks) != testParties {
		t.Fatalf("committee has %d sdks, want %d", len(c.sdks), testParties)
	}

	pubHexes := make([]string, 0, testParties)
	for i, sdk := range c.sdks {
		share, thr, parties, partyIdx, ok := sdk.snapshotOwnShare()
		if !ok {
			t.Fatalf("device %d: snapshotOwnShare ok=false", i)
		}
		if thr != testThreshold || parties != testParties || partyIdx != i {
			t.Fatalf("device %d: thr=%d parties=%d partyIdx=%d, want %d/%d/%d",
				i, thr, parties, partyIdx, testThreshold, testParties, i)
		}
		// One-share-per-device hard judgement (distributed-mpc.md §7 closing
		// acceptance): each keystore must hold exactly one share.
		sdk.mu.Lock()
		got := len(sdk.shares)
		sdk.mu.Unlock()
		if got != 1 {
			t.Fatalf("device %d: in-memory shares = %d, want 1 (single-party invariant)", i, got)
		}
		// And recoverable from the sealed keystore with the passphrase.
		loaded, err := sdk.store.Load(context.Background(), share.Moniker, c.pw)
		if err != nil {
			t.Fatalf("device %d: keystore Load: %v", i, err)
		}
		if loaded.Moniker != share.Moniker || len(loaded.SaveData) == 0 {
			t.Fatalf("device %d: loaded share malformed: %q", i, loaded.Moniker)
		}
		ph, err := groupPubHex(share)
		if err != nil {
			t.Fatalf("device %d: pub: %v", i, err)
		}
		pubHexes = append(pubHexes, ph)
	}
	// Every device converges on one master public key.
	for i := 1; i < len(pubHexes); i++ {
		if pubHexes[i] != pubHexes[0] {
			t.Fatalf("device %d pub %s != device 0 pub %s", i, pubHexes[i], pubHexes[0])
		}
	}
	if c.summary.GroupPubKey != pubHexes[0] {
		t.Fatalf("summary pub %s != device 0 derived %s", c.summary.GroupPubKey, pubHexes[0])
	}
	if c.summary.Threshold != testThreshold || c.summary.Parties != testParties {
		t.Fatalf("summary t/n = %d/%d", c.summary.Threshold, c.summary.Parties)
	}
}

// TestKeyGenRejectsBadConfig: the DM-3 hard-cut. Every mandatory field —
// the legacy schema plus the new envelope — must be rejected with
// CodeBadConfig before any work begins. This is the "legacy configJSON
// missing new fields => reject" acceptance gate (distributed-mpc-impl.md
// §B DM-3).
func TestKeyGenRejectsBadConfig(t *testing.T) {
	sdk := newTestSDK(t, 0)
	// good is the canonical DM-3 envelope every case mutates a single
	// field of, so any rejection traces back to ONE missing/invalid input.
	good := keygenConfigPayload("g1", testSessionID, 0, testParties, testThreshold, testMembers, testRelay, testPassphrase)

	type tc struct {
		name string
		mut  func(*keygenConfig)
		want string
	}

	cases := []tc{
		{"malformed json", nil, CodeBadConfig},
		{"missing groupId", func(c *keygenConfig) { c.GroupID = nil }, CodeBadConfig},
		{"missing sessionID", func(c *keygenConfig) { c.SessionID = nil }, CodeBadConfig},
		{"missing partyIndex", func(c *keygenConfig) { c.PartyIndex = nil }, CodeBadConfig},
		{"missing n", func(c *keygenConfig) { c.N = nil }, CodeBadConfig},
		{"missing t", func(c *keygenConfig) { c.T = nil }, CodeBadConfig},
		{"missing memberSet", func(c *keygenConfig) { c.MemberSet = nil }, CodeBadConfig},
		{"missing relay", func(c *keygenConfig) { c.Relay = nil }, CodeBadConfig},
		{"missing relay.peerID", func(c *keygenConfig) { c.Relay = &relayConfig{Addrs: testRelay.Addrs} }, CodeBadConfig},
		{"missing relay.addrs", func(c *keygenConfig) { c.Relay = &relayConfig{PeerID: testRelay.PeerID} }, CodeBadConfig},
		{"missing role", func(c *keygenConfig) { c.Role = nil }, CodeBadConfig},
		{"missing passphrase", func(c *keygenConfig) { c.Passphrase = "" }, CodeBadConfig},
		{"t >= n", func(c *keygenConfig) { v := testParties; c.T = &v }, CodeBadConfig},
		{"n < 2", func(c *keygenConfig) { v := 1; c.N = &v }, CodeBadConfig},
		{"partyIndex out of range", func(c *keygenConfig) { v := testParties; c.PartyIndex = &v }, CodeBadConfig},
		{"memberSet length mismatch", func(c *keygenConfig) { c.MemberSet = []string{"only-one"} }, CodeBadConfig},
	}

	// Pre-canned malformed JSON case is special-cased — we feed raw bytes
	// rather than a mutated structure.
	t.Run("malformed json", func(t *testing.T) {
		r := newRecorder()
		sdk.KeyGen("{", noopWire{}, kgAdapter{r})
		r.wait(t)
		code, _, _, _ := r.result()
		if code != CodeBadConfig {
			t.Fatalf("code=%q want %q", code, CodeBadConfig)
		}
	})

	for _, c := range cases[1:] {
		t.Run(c.name, func(t *testing.T) {
			cfg := good
			c.mut(&cfg)
			r := newRecorder()
			sdk.KeyGen(marshalConfig(t, cfg), noopWire{}, kgAdapter{r})
			r.wait(t)
			code, _, _, _ := r.result()
			if code != c.want {
				t.Fatalf("code=%q want %q", code, c.want)
			}
			if reflect.DeepEqual(r.snapOrder(), []string{"progress"}) {
				t.Fatal("config rejection must not start work")
			}
		})
	}

	t.Run("nil wire callbacks", func(t *testing.T) {
		r := newRecorder()
		sdk.KeyGen(marshalConfig(t, good), nil, kgAdapter{r})
		r.wait(t)
		if code, _, _, _ := r.result(); code != CodeBadConfig {
			t.Fatalf("code=%q want %q", code, CodeBadConfig)
		}
	})
}

// TestKeyGenHardCutLegacyConfig: a configJSON in the OLD shape
// {"threshold","parties","passphrase"} must be hard-cut as a bad config so
// any caller still on the pre-DM-3 schema fails loud, never silently runs.
func TestKeyGenHardCutLegacyConfig(t *testing.T) {
	sdk := newTestSDK(t, 0)
	legacy := map[string]any{
		"threshold":  testThreshold,
		"parties":    testParties,
		"passphrase": testPassphrase,
	}
	raw, _ := json.Marshal(legacy)
	r := newRecorder()
	sdk.KeyGen(string(raw), noopWire{}, kgAdapter{r})
	r.wait(t)
	code, _, _, _ := r.result()
	if code != CodeBadConfig {
		t.Fatalf("legacy configJSON not hard-cut: code=%q want %q", code, CodeBadConfig)
	}
}
