package mobileapi

import (
	"encoding/json"
	"testing"
)

// TestReshareKeepsMasterKey: a distributed reshare across the committee
// produces a fresh share_i on every device whose master public key — and
// thus every derived address — is unchanged (docs/design/mcp/sdk.md §7),
// and the rotated committee can sign. The reshare runs over the ring
// fabric (DM-3 single-party with host-owned wire) and reuses the package's
// single real keygen.
func TestReshareKeepsMasterKey(t *testing.T) {
	sdks, fabric, c := committeeSDKs(t)
	beforePub := c.summary.GroupPubKey

	const reshareSessionID = "test-session-reshare-0001"
	recs := make([]*recorder, testParties)
	for i := 0; i < testParties; i++ {
		recs[i] = newRecorder()
		cfg := reshareConfigPayload(c.groupID, reshareSessionID, i, testParties, testThreshold, testThreshold, testMembers, testRelay, testPassphrase)
		sdks[i].Reshare(marshalConfig(t, cfg), fabric.wcFor(i), rsAdapter{recs[i]})
	}
	for i := 0; i < testParties; i++ {
		recs[i].wait(t)
		code, msg, _, _ := recs[i].result()
		if code != "" {
			t.Fatalf("device %d reshare errored: %s %s", i, code, msg)
		}
	}
	fabric.assertNoErrs(t)

	// Every device's new share recovers the original master key.
	for i := 0; i < testParties; i++ {
		_, _, sum, _ := recs[i].result()
		var summary keygenSummary
		if err := json.Unmarshal([]byte(sum), &summary); err != nil {
			t.Fatalf("device %d summary: %v", i, err)
		}
		if summary.GroupPubKey != beforePub {
			t.Fatalf("device %d master key changed across reshare: %s -> %s", i, beforePub, summary.GroupPubKey)
		}
		share, _, _, _, ok := sdks[i].snapshotOwnShare()
		if !ok {
			t.Fatalf("device %d holds no share after reshare", i)
		}
		ph, err := groupPubHex(share)
		if err != nil {
			t.Fatalf("device %d pub: %v", i, err)
		}
		if ph != beforePub {
			t.Fatalf("device %d in-memory share pub %s != original %s", i, ph, beforePub)
		}
	}
}

// TestReshareWithoutSharesRejected: a Reshare without a prior keygen yields
// CodeNoShares (the single-party device cannot reshare from nothing).
func TestReshareWithoutSharesRejected(t *testing.T) {
	sdk := newTestSDK(t, 0)
	r := newRecorder()
	cfg := reshareConfigPayload("g-no-share", "reshare-no-share", 0, testParties, testThreshold, testThreshold, testMembers, testRelay, testPassphrase)
	sdk.Reshare(marshalConfig(t, cfg), noopWire{}, rsAdapter{r})
	r.wait(t)
	if code, _, _, _ := r.result(); code != CodeNoShares {
		t.Fatalf("code=%q want %q", code, CodeNoShares)
	}
}

// TestReshareRejectsBadConfig: the DM-3 hard-cut for reshare. Each mandatory
// field absence yields CodeBadConfig.
func TestReshareRejectsBadConfig(t *testing.T) {
	sdk := newTestSDK(t, 0)
	good := reshareConfigPayload("g1", "reshare-sid-0001", 0, testParties, testThreshold, testThreshold, testMembers, testRelay, testPassphrase)
	type tc struct {
		name string
		mut  func(*reshareConfig)
	}
	cases := []tc{
		{"missing groupId", func(c *reshareConfig) { c.GroupID = nil }},
		{"missing sessionID", func(c *reshareConfig) { c.SessionID = nil }},
		{"missing partyIndex", func(c *reshareConfig) { c.PartyIndex = nil }},
		{"missing n", func(c *reshareConfig) { c.N = nil }},
		{"missing oldT", func(c *reshareConfig) { c.OldT = nil }},
		{"missing newT", func(c *reshareConfig) { c.NewT = nil }},
		{"missing memberSet", func(c *reshareConfig) { c.MemberSet = nil }},
		{"missing relay", func(c *reshareConfig) { c.Relay = nil }},
		{"missing role", func(c *reshareConfig) { c.Role = nil }},
		{"missing passphrase", func(c *reshareConfig) { c.Passphrase = "" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := good
			c.mut(&cfg)
			r := newRecorder()
			sdk.Reshare(marshalConfig(t, cfg), noopWire{}, rsAdapter{r})
			r.wait(t)
			if code, _, _, _ := r.result(); code != CodeBadConfig {
				t.Fatalf("code=%q want %q", code, CodeBadConfig)
			}
		})
	}
}
