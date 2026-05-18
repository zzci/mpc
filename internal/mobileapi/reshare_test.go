package mobileapi

import (
	"encoding/json"
	"testing"
)

// TestReshareKeepsMasterKey: resharing the shared committee onto a new
// (t', n') produces a fresh committee whose master public key — and thus every
// derived address — is unchanged (docs/design/mcp/sdk.md §7), and the new shares
// can sign. It reuses the package's single real keygen (committeeSDK) so no
// extra heavy keygen is paid here.
func TestReshareKeepsMasterKey(t *testing.T) {
	sdk := committeeSDK(t)
	before := sharedCommittee(t).summary

	r := newRecorder()
	cfg, _ := json.Marshal(reshareConfig{
		OldThreshold: testThreshold,
		NewThreshold: testThreshold,
		NewParties:   testParties,
		Passphrase:   testPassphrase,
	})
	sdk.Reshare(string(cfg), rsAdapter{r})
	r.wait(t)

	code, msg, summary, _ := r.result()
	if code != "" {
		t.Fatalf("reshare errored: %s %s", code, msg)
	}
	var after keygenSummary
	if err := json.Unmarshal([]byte(summary), &after); err != nil {
		t.Fatalf("summary: %v", err)
	}
	if after.GroupPubKey != before.GroupPubKey {
		t.Fatalf("master key changed across reshare: %s -> %s", before.GroupPubKey, after.GroupPubKey)
	}
	if len(after.Monikers) != testParties {
		t.Fatalf("new committee size = %d", len(after.Monikers))
	}

	// New committee can sign and recovers the same key.
	r2 := newRecorder()
	ss := sdk.Sign(buildStart(t, nil), signAdapter{r2})
	r2.waitDecoded(t)
	ss.Approve()
	r2.wait(t)
	code2, msg2, _, rsv2 := r2.result()
	if code2 != "" || len(rsv2) != 65 {
		t.Fatalf("post-reshare sign failed: %s %s (rsv=%d)", code2, msg2, len(rsv2))
	}
}

func TestReshareWithoutSharesRejected(t *testing.T) {
	sdk := newTestSDK(t)
	r := newRecorder()
	cfg, _ := json.Marshal(reshareConfig{
		OldThreshold: testThreshold, NewThreshold: testThreshold,
		NewParties: testParties, Passphrase: "pw",
	})
	sdk.Reshare(string(cfg), rsAdapter{r})
	r.wait(t)
	if code, _, _, _ := r.result(); code != CodeNoShares {
		t.Fatalf("code=%q want %q", code, CodeNoShares)
	}
}
