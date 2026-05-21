package walletcli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zzci/mpc/internal/mobileapi"
	"github.com/zzci/mpc/internal/mpc"
	"github.com/zzci/mpc/sdk"
)

// testGroup is one row of the SDK's in-memory group set that test
// helpers seed via the internal setOwnShare path.
type testGroup struct {
	moniker    string
	threshold  int
	parties    int
	partyIndex int
	pub        string
}

// newTestSession builds a wallet-cli session backed by a real (empty) SDK
// rooted at dir. Tests that exercise the in-process /api/v1 layer or the
// shell commands use this directly so they do not have to spin up `cli
// serve`.
func newGroupsTestSession(t *testing.T, dir string, out, errw io.Writer) *session {
	t.Helper()
	s, err := sdk.NewSDK(dir)
	if err != nil {
		t.Fatalf("NewSDK: %v", err)
	}
	return &session{sdk: s, keystoreDir: dir, out: out, errw: errw}
}

// mustSDKWith returns an SDK seeded with the given groups via the
// internal setOwnShare path. The wallet-cli session uses sdk.SDK
// (the public facade) but its inner is mobileapi.SDK; we reach through
// that to call setOwnShare in tests without driving a full MPC ceremony.
func mustSDKWith(t *testing.T, dir string, groups map[string]testGroup) *sdk.SDK {
	t.Helper()
	innerOnly, err := mobileapi.NewSDK(dir)
	if err != nil {
		t.Fatalf("mobileapi.NewSDK: %v", err)
	}
	for gid, g := range groups {
		innerOnly.SetOwnShareForTest(gid, mpc.Share{Moniker: g.moniker, SaveData: []byte(g.pub)},
			g.threshold, g.parties, g.partyIndex, g.pub)
	}
	return sdk.WrapForTest(innerOnly)
}

// TestPairingsListMigratesLegacy proves the new multi-group loader picks
// up a pre-multi-group pair.json record when no pairings.json exists yet,
// returning it as a single-element list.
func TestPairingsListMigratesLegacy(t *testing.T) {
	dir := t.TempDir()
	rec := pairPersisted{
		GroupID: "g-legacy", CoordBaseURL: "https://old.example.com",
		IdentityPubHex: "02deadbeef", IdentityPrivHex: "00",
		PairedAtMS: time.Now().UnixMilli(),
	}
	b, _ := json.Marshal(rec)
	if err := os.WriteFile(filepath.Join(dir, pairFileName), b, 0o600); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}

	list, err := loadPairings(dir)
	if err != nil {
		t.Fatalf("loadPairings: %v", err)
	}
	if len(list) != 1 || list[0].GroupID != "g-legacy" {
		t.Fatalf("legacy migration lost record: %+v", list)
	}
}

func TestPairingsPersistAppends(t *testing.T) {
	dir := t.TempDir()
	if err := persistPair(dir, pairPersisted{GroupID: "g1", IdentityPubHex: "02aa"}); err != nil {
		t.Fatalf("persist g1: %v", err)
	}
	if err := persistPair(dir, pairPersisted{GroupID: "g2", IdentityPubHex: "02bb"}); err != nil {
		t.Fatalf("persist g2: %v", err)
	}
	list, err := loadPairings(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 pairings got %d: %+v", len(list), list)
	}
}

func TestPairingsPersistReplaceByGroupID(t *testing.T) {
	dir := t.TempDir()
	_ = persistPair(dir, pairPersisted{GroupID: "g1", Label: "first", IdentityPubHex: "02aa"})
	_ = persistPair(dir, pairPersisted{GroupID: "g1", Label: "second", IdentityPubHex: "02bb"})
	list, _ := loadPairings(dir)
	if len(list) != 1 || list[0].Label != "second" || list[0].IdentityPubHex != "02bb" {
		t.Fatalf("replace-by-groupId lost: %+v", list)
	}
}

func TestCmdGroupsListsSDKOnly(t *testing.T) {
	dir := t.TempDir()
	out := &strings.Builder{}
	errw := &strings.Builder{}
	se := newGroupsTestSession(t, dir, out, errw)
	// Seed a single group in the SDK (no persisted pairing).
	se.sdk = mustSDKWith(t, dir, map[string]testGroup{
		"groupA": {moniker: "mA", threshold: 1, parties: 3, partyIndex: 0, pub: "02aa"},
	})
	se.cmdGroups(nil)
	if errw.Len() != 0 {
		t.Fatalf("stderr: %s", errw.String())
	}
	var resp struct {
		Items []groupsView `json:"items"`
	}
	if err := json.Unmarshal([]byte(out.String()), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, out.String())
	}
	if len(resp.Items) != 1 || resp.Items[0].GroupID != "groupA" || resp.Items[0].Source != "sdk" {
		t.Fatalf("sdk-only row missing: %+v", resp.Items)
	}
}

func TestCmdGroupsMergesSDKAndPair(t *testing.T) {
	dir := t.TempDir()
	// Persist one pair record.
	_ = persistPair(dir, pairPersisted{
		GroupID: "groupA", CoordBaseURL: "https://c.example.com",
		IdentityPubHex: "02zz", RelayPeerID: "12D3K", Label: "Alice",
	})
	out := &strings.Builder{}
	errw := &strings.Builder{}
	se := newGroupsTestSession(t, dir, out, errw)
	se.sdk = mustSDKWith(t, dir, map[string]testGroup{
		"groupA": {moniker: "mA", threshold: 1, parties: 3, partyIndex: 0, pub: "02aa"},
		"groupB": {moniker: "mB", threshold: 2, parties: 5, partyIndex: 1, pub: "02bb"},
	})
	se.cmdGroups(nil)
	if errw.Len() != 0 {
		t.Fatalf("stderr: %s", errw.String())
	}
	var resp struct {
		Items []groupsView `json:"items"`
	}
	if err := json.Unmarshal([]byte(out.String()), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, out.String())
	}
	if len(resp.Items) != 2 {
		t.Fatalf("want 2 rows got %d", len(resp.Items))
	}
	bySrc := map[string]groupsView{}
	for _, r := range resp.Items {
		bySrc[r.GroupID] = r
	}
	if bySrc["groupA"].Source != "sdk+pair" || bySrc["groupA"].Label != "Alice" {
		t.Fatalf("groupA: %+v (want sdk+pair, Alice)", bySrc["groupA"])
	}
	if bySrc["groupB"].Source != "sdk" {
		t.Fatalf("groupB: %+v (want sdk-only)", bySrc["groupB"])
	}
}
