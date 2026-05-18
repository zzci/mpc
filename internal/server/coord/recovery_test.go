package coord

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/royqta/mcp-wallet/internal/contract"
)

// K-002 lost-member recovery, coord side. The reshare commit reuses the S-002
// membership-update path (epoch-monotonic, ecdsaPubkey-invariant, ≥ old-t
// stored-active co-signatures); K-002 verifies the lost-member specifics:
// removal degrades reported redundancy, recovery restores it, and the
// pubkey-invariant / anti-rollback guards hold on the removal case.

func (h *harness) groupView(t *testing.T, g *testGroup, asMember string) groupPublicView {
	t.Helper()
	hdr := h.memberHdr(g, asMember, "S2:groupRead", g.groupID, []byte(""))
	r := h.do(t, http.MethodGet, "/v1/groups/"+g.groupID, hdr, nil)
	if r.code != http.StatusOK {
		t.Fatalf("group view: %d %s", r.code, r.text())
	}
	var v groupPublicView
	if err := json.Unmarshal(r.body, &v); err != nil {
		t.Fatalf("decode group view: %v", err)
	}
	return v
}

// signedMembership builds a membershipUpdate authorized by the stored group
// key plus the named stored-active member co-signers.
func (h *harness) signedMembership(g *testGroup, p *membershipUpdate, cosigners []string) []byte {
	dig, _ := membershipUpdateDigest(p)
	p.GroupSig = contract.SignDigest(g.capPriv, dig)
	for _, id := range cosigners {
		p.MemberCoSigs = append(p.MemberCoSigs, coSig{id, contract.SignDigest(g.members[id], dig)})
	}
	b, _ := json.Marshal(p)
	return b
}

func TestLostMemberRecoveryFlow(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-recover", 2, 3)

	// Full redundancy at provisioning: 3 active == n, not degraded.
	if v := h.groupView(t, g, "m0"); v.ActiveMembers != 3 || v.Degraded {
		t.Fatalf("fresh group should be full: active=%d degraded=%v", v.ActiveMembers, v.Degraded)
	}

	// m2's device is lost. The survivors authorize a reshare removing m2
	// (pubkey invariant, epoch 1, ≥ old t=2 stored-active co-signers).
	rm := &membershipUpdate{
		Version: contract.EnvelopeVersionV1, GroupID: g.groupID, Epoch: 1,
		ECDSAPubkeyAssert: g.mainPub, GroupPubkey: g.capPubC,
		ThresholdT: 2, PartiesN: 3,
		RemovedMemberIDs: []string{"m2"},
		UpdatedAt:        h.clk.Now().Format(time.RFC3339Nano),
	}
	if r := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/membership", nil,
		h.signedMembership(g, rm, []string{"m0", "m1"})); r.code != http.StatusOK {
		t.Fatalf("remove lost member: %d %s", r.code, r.text())
	}

	// Redundancy window: 2 active < provisioned n=3 -> degraded, prompts the
	// SDK to complete recovery (sdk.md §7).
	if v := h.groupView(t, g, "m0"); v.ActiveMembers != 2 || !v.Degraded || v.Epoch != 1 {
		t.Fatalf("post-loss should be degraded: active=%d degraded=%v epoch=%d",
			v.ActiveMembers, v.Degraded, v.Epoch)
	}

	// Anti-rollback: a non-monotonic epoch is rejected even with valid sigs.
	stale := &membershipUpdate{
		Version: contract.EnvelopeVersionV1, GroupID: g.groupID, Epoch: 1,
		ECDSAPubkeyAssert: g.mainPub, GroupPubkey: g.capPubC,
		ThresholdT: 2, PartiesN: 3,
		AddedMembers: []memberEntry{{MemberID: "mX", IdentityPubkey: g.memberPub["m0"]}},
		UpdatedAt:    h.clk.Now().Format(time.RFC3339Nano),
	}
	if r := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/membership", nil,
		h.signedMembership(g, stale, []string{"m0", "m1"})); r.code != http.StatusConflict {
		t.Fatalf("stale epoch: want 409 got %d %s", r.code, r.text())
	}

	// Main-pubkey-invariant guard: a reshare asserting a different ecdsaPubkey
	// is refused (the master key must never change across recovery).
	_, badPub, _ := newKey(t)
	badAssert := &membershipUpdate{
		Version: contract.EnvelopeVersionV1, GroupID: g.groupID, Epoch: 2,
		ECDSAPubkeyAssert: badPub, GroupPubkey: g.capPubC,
		ThresholdT: 2, PartiesN: 3,
		AddedMembers: []memberEntry{{MemberID: "m3", IdentityPubkey: g.memberPub["m1"]}},
		UpdatedAt:    h.clk.Now().Format(time.RFC3339Nano),
	}
	if r := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/membership", nil,
		h.signedMembership(g, badAssert, []string{"m0", "m1"})); r.code != http.StatusConflict {
		t.Fatalf("pubkey-changed reshare: want 409 got %d %s", r.code, r.text())
	}

	// Recovery completes: a fresh replacement member m3 is added, restoring
	// the committee to full strength. Epoch advances monotonically.
	_, _, m3pub := newKey(t)
	rec := &membershipUpdate{
		Version: contract.EnvelopeVersionV1, GroupID: g.groupID, Epoch: 2,
		ECDSAPubkeyAssert: g.mainPub, GroupPubkey: g.capPubC,
		ThresholdT: 2, PartiesN: 3,
		AddedMembers: []memberEntry{{MemberID: "m3", IdentityPubkey: m3pub}},
		UpdatedAt:    h.clk.Now().Format(time.RFC3339Nano),
	}
	if r := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/membership", nil,
		h.signedMembership(g, rec, []string{"m0", "m1"})); r.code != http.StatusOK {
		t.Fatalf("recovery commit: %d %s", r.code, r.text())
	}

	// Redundancy restored: 3 active == n, no longer degraded.
	if v := h.groupView(t, g, "m0"); v.ActiveMembers != 3 || v.Degraded || v.Epoch != 2 {
		t.Fatalf("post-recovery should be full: active=%d degraded=%v epoch=%d",
			v.ActiveMembers, v.Degraded, v.Epoch)
	}
}
