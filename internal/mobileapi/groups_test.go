package mobileapi

import (
	"encoding/json"
	"testing"

	"github.com/zzci/mpc/internal/mpc"
)

// TestMultiGroupAppendsShares proves setOwnShare does NOT clobber a
// previously-stored group; both shares + both group metas must survive a
// second setOwnShare call for a distinct groupID.
func TestMultiGroupAppendsShares(t *testing.T) {
	dir := t.TempDir()
	sdk, err := NewSDK(dir)
	if err != nil {
		t.Fatalf("NewSDK: %v", err)
	}

	share1 := mpc.Share{Moniker: "m-g1", SaveData: []byte("data-1")}
	sdk.setOwnShare("g1", share1, 1, 3, 0, "02aabb")

	share2 := mpc.Share{Moniker: "m-g2", SaveData: []byte("data-2")}
	sdk.setOwnShare("g2", share2, 2, 5, 1, "02ccdd")

	// snapshotShareForGroup must route by groupID.
	got1, t1, n1, idx1, ok1 := sdk.snapshotShareForGroup("g1")
	if !ok1 || got1.Moniker != "m-g1" || t1 != 1 || n1 != 3 || idx1 != 0 {
		t.Fatalf("g1: ok=%v share.Moniker=%q t/n/idx=%d/%d/%d", ok1, got1.Moniker, t1, n1, idx1)
	}
	got2, t2, n2, idx2, ok2 := sdk.snapshotShareForGroup("g2")
	if !ok2 || got2.Moniker != "m-g2" || t2 != 2 || n2 != 5 || idx2 != 1 {
		t.Fatalf("g2: ok=%v share.Moniker=%q t/n/idx=%d/%d/%d", ok2, got2.Moniker, t2, n2, idx2)
	}

	// snapshotOwnShare (legacy single-group helper) must report ok=false
	// when more than one group is held — caller has to use the explicit
	// snapshotShareForGroup(groupID).
	if _, _, _, _, ok := sdk.snapshotOwnShare(); ok {
		t.Fatalf("snapshotOwnShare should be ok=false when 2 groups are held")
	}
}

func TestMultiGroupUnknownGroup(t *testing.T) {
	dir := t.TempDir()
	sdk, _ := NewSDK(dir)
	sdk.setOwnShare("g1", mpc.Share{Moniker: "m", SaveData: []byte("x")}, 1, 3, 0, "02aa")
	if _, _, _, _, ok := sdk.snapshotShareForGroup("does-not-exist"); ok {
		t.Fatalf("unknown groupID should not route")
	}
}

func TestListGroupsJSON(t *testing.T) {
	dir := t.TempDir()
	sdk, _ := NewSDK(dir)
	sdk.setOwnShare("g1", mpc.Share{Moniker: "m1", SaveData: []byte("1")}, 1, 3, 0, "02aa")
	sdk.setOwnShare("g2", mpc.Share{Moniker: "m2", SaveData: []byte("2")}, 2, 5, 1, "02bb")

	js, err := sdk.ListGroupsJSON()
	if err != nil {
		t.Fatalf("ListGroupsJSON: %v", err)
	}
	var resp struct {
		Items []struct {
			GroupID     string `json:"groupId"`
			Threshold   int    `json:"threshold"`
			Parties     int    `json:"parties"`
			PartyIndex  int    `json:"partyIndex"`
			ECDSAPubHex string `json:"ecdsaPubHex"`
			Moniker     string `json:"moniker"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(js), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, js)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("want 2 items got %d", len(resp.Items))
	}
	// Map for order-independent assertion (map iteration order is random).
	got := map[string]struct {
		t, n, i int
		pub, mn string
	}{}
	for _, it := range resp.Items {
		got[it.GroupID] = struct {
			t, n, i int
			pub, mn string
		}{it.Threshold, it.Parties, it.PartyIndex, it.ECDSAPubHex, it.Moniker}
	}
	if got["g1"].t != 1 || got["g1"].mn != "m1" {
		t.Fatalf("g1 row wrong: %+v", got["g1"])
	}
	if got["g2"].n != 5 || got["g2"].pub != "02bb" {
		t.Fatalf("g2 row wrong: %+v", got["g2"])
	}
}

// TestSetOwnShareReplaceSameGroup proves a second call for the SAME
// groupID overwrites the previous entry (e.g. after a reshare that
// rotates the share for that group).
func TestSetOwnShareReplaceSameGroup(t *testing.T) {
	dir := t.TempDir()
	sdk, _ := NewSDK(dir)
	sdk.setOwnShare("g1", mpc.Share{Moniker: "old", SaveData: []byte("v1")}, 1, 3, 0, "02aa")
	sdk.setOwnShare("g1", mpc.Share{Moniker: "new", SaveData: []byte("v2")}, 2, 5, 1, "02bb")

	got, thr, parties, idx, ok := sdk.snapshotShareForGroup("g1")
	if !ok || got.Moniker != "new" || thr != 2 || parties != 5 || idx != 1 {
		t.Fatalf("post-reshare lookup: ok=%v moniker=%q t/n/idx=%d/%d/%d", ok, got.Moniker, thr, parties, idx)
	}
	// The previous moniker remains in shares (Import / Export still see
	// it) — losing the saved share material on reshare is the caller's
	// concern (wallet-cli, not the SDK).
	sdk.mu.Lock()
	_, oldStillThere := sdk.shares["old"]
	sdk.mu.Unlock()
	if !oldStillThere {
		t.Fatalf("old share moniker dropped from sdk.shares — should persist")
	}
}
