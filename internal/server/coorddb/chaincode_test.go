package coorddb

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// AD-3 chaincode persistence: ProvisionGroup must write the 32-byte HD
// chaincode in the same transaction as ecdsa_pubkey/evm_address/
// tron_address (docs/design/mcp/address-derivation.md §4); the v4
// migration adds the column reversibly, idempotently, with a CHECK
// guaranteeing BLOB(32) NULL semantics. Legacy callers that pass no
// chaincode keep the column NULL (F5: legacy groups remain non-HD).

func groupChaincode(t *testing.T, s *Store, groupID string) ([]byte, bool) {
	t.Helper()
	db, err := s.conn()
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	var cc []byte
	row := db.QueryRowContext(context.Background(),
		`SELECT chaincode FROM groups WHERE group_id = ?`, groupID)
	if err := row.Scan(&cc); err != nil {
		t.Fatalf("scan chaincode: %v", err)
	}
	if cc == nil {
		return nil, false
	}
	return cc, true
}

func TestProvisionGroup_PersistsChaincode(t *testing.T) {
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())

	want := bytes.Repeat([]byte{0xAA}, 32)
	if err := s.ProvisionGroup(ctx, GroupRecord{
		GroupID: "grp-hd", ECDSAPubkey: pubFromScalar(1), GroupPubkey: []byte{4},
		ThresholdT: 2, PartiesN: 3, Epoch: 0, CreatedAt: "2026-05-19T00:00:00Z",
		Chaincode: want,
	}, []MemberRecord{{MemberID: "m0", IdentityPubkey: []byte{7}}}); err != nil {
		t.Fatalf("provision: %v", err)
	}
	got, ok := groupChaincode(t, s, "grp-hd")
	if !ok {
		t.Fatal("chaincode column is NULL, want 32-byte payload")
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("chaincode = %x, want %x", got, want)
	}
}

func TestProvisionGroup_LegacyNilChaincode(t *testing.T) {
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())

	// No Chaincode field set -> stored as NULL (F5: legacy non-HD group).
	if err := s.ProvisionGroup(ctx, GroupRecord{
		GroupID: "grp-legacy", ECDSAPubkey: pubFromScalar(1), GroupPubkey: []byte{4},
		ThresholdT: 2, PartiesN: 3, Epoch: 0, CreatedAt: "2026-05-19T00:00:00Z",
	}, []MemberRecord{{MemberID: "m0", IdentityPubkey: []byte{7}}}); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if _, ok := groupChaincode(t, s, "grp-legacy"); ok {
		t.Fatal("legacy provisioning persisted a chaincode; want NULL")
	}
}

func TestProvisionGroup_RejectsMalformedChaincode(t *testing.T) {
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())

	// Application-layer guard: non-32-byte chaincode is refused before any
	// row is written, so a malformed length cannot reach the schema CHECK
	// (defense-in-depth — DB CHECK still backstops a future caller that
	// bypasses ProvisionGroup).
	err := s.ProvisionGroup(ctx, GroupRecord{
		GroupID: "grp-bad", ECDSAPubkey: pubFromScalar(1), GroupPubkey: []byte{4},
		ThresholdT: 2, PartiesN: 3, Epoch: 0, CreatedAt: "2026-05-19T00:00:00Z",
		Chaincode: bytes.Repeat([]byte{1}, 31), // off by one
	}, []MemberRecord{{MemberID: "m0", IdentityPubkey: []byte{7}}})
	if err == nil {
		t.Fatal("ProvisionGroup accepted a 31-byte chaincode; want error")
	}
	if !strings.Contains(err.Error(), "chaincode") {
		t.Fatalf("error %q does not mention chaincode", err.Error())
	}
}

func TestMigrationV4_DBLevelChaincodeCheck(t *testing.T) {
	// Defense-in-depth: a raw INSERT with a malformed chaincode must be
	// rejected by the schema CHECK (00004 migration). This guards against
	// future code paths that bypass ProvisionGroup.
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())
	db := mustDB(t, s)

	_, err := db.ExecContext(ctx,
		`INSERT INTO groups (group_id, ecdsa_pubkey, threshold_t, parties_n, group_pubkey, epoch,
		                     created_at, updated_at, evm_address, tron_address, chaincode)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"grp-cc-bad", []byte{1}, 1, 1, []byte{2}, 0,
		"2026-05-19T00:00:00Z", "2026-05-19T00:00:00Z", "", "",
		bytes.Repeat([]byte{1}, 16))
	if err == nil {
		t.Fatal("raw INSERT with 16-byte chaincode succeeded; CHECK missing")
	}

	// A 32-byte chaincode is accepted by the same INSERT path.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO groups (group_id, ecdsa_pubkey, threshold_t, parties_n, group_pubkey, epoch,
		                     created_at, updated_at, evm_address, tron_address, chaincode)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"grp-cc-ok", []byte{1}, 1, 1, []byte{2}, 0,
		"2026-05-19T00:00:00Z", "2026-05-19T00:00:00Z", "", "",
		bytes.Repeat([]byte{1}, 32)); err != nil {
		t.Fatalf("raw INSERT with 32-byte chaincode rejected: %v", err)
	}

	// And a NULL chaincode is allowed (legacy F5 path).
	if _, err := db.ExecContext(ctx,
		`INSERT INTO groups (group_id, ecdsa_pubkey, threshold_t, parties_n, group_pubkey, epoch,
		                     created_at, updated_at, evm_address, tron_address, chaincode)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		"grp-cc-null", []byte{1}, 1, 1, []byte{2}, 0,
		"2026-05-19T00:00:00Z", "2026-05-19T00:00:00Z", "", ""); err != nil {
		t.Fatalf("raw INSERT with NULL chaincode rejected: %v", err)
	}
}

func TestMigrationV4_UpDownIdempotent(t *testing.T) {
	ctx := context.Background()
	s := NewStore(filepath.Join(t.TempDir(), "coord.db"))
	if err := s.Unlock(ctx, []byte(testPass)); err != nil { // runs 00001..00004
		t.Fatalf("unlock: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if !columnExists(t, s, "groups", "chaincode") {
		t.Fatal("v4 column chaincode missing after migrate up")
	}

	// Seed a row with a chaincode to validate the rebuild preserves data on
	// the way down to v3 (chaincode dropped) and that the round-trip back to
	// v4 leaves NULL (legacy F5: post-down rebuild lost the column, the
	// re-up has nothing to backfill).
	want := bytes.Repeat([]byte{0x55}, 32)
	if err := s.ProvisionGroup(ctx, GroupRecord{
		GroupID: "grp-rt", ECDSAPubkey: pubFromScalar(1), GroupPubkey: []byte{4},
		ThresholdT: 2, PartiesN: 3, Epoch: 0, CreatedAt: "2026-05-19T00:00:00Z",
		Chaincode: want,
	}, []MemberRecord{{MemberID: "m0", IdentityPubkey: []byte{7}}}); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if got, ok := groupChaincode(t, s, "grp-rt"); !ok || !bytes.Equal(got, want) {
		t.Fatalf("pre-down chaincode = %x (ok=%v), want %x", got, ok, want)
	}

	// Down to v3: chaincode column removed, but the base row + 00002 derived
	// addresses must survive the rebuild.
	if err := migrateDownTo(ctx, mustDB(t, s), 3); err != nil {
		t.Fatalf("down to v3: %v", err)
	}
	if columnExists(t, s, "groups", "chaincode") {
		t.Fatal("chaincode column still present after down-to-3")
	}
	if !columnExists(t, s, "groups", "evm_address") || !columnExists(t, s, "groups", "tron_address") {
		t.Fatal("00002 columns lost on v4 down (rebuild must preserve them)")
	}
	if !tableExists(t, s, "groups") {
		t.Fatal("groups table lost on v4 down (rebuild must preserve it)")
	}
	var gid, evm, tron string
	if err := mustDB(t, s).QueryRowContext(ctx,
		`SELECT group_id, evm_address, tron_address FROM groups WHERE group_id = ?`,
		"grp-rt").Scan(&gid, &evm, &tron); err != nil {
		t.Fatalf("row lost on v4 down rebuild: %v", err)
	}
	if evm == "" || tron == "" {
		t.Fatalf("derived addresses lost on v4 down: evm=%q tron=%q", evm, tron)
	}

	// Re-up to v4: chaincode column comes back; the pre-down row has no
	// chaincode (column was dropped, re-up cannot reconstruct it). This
	// matches F5: chaincode cannot be back-injected.
	if err := migrateUp(ctx, mustDB(t, s)); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	if !columnExists(t, s, "groups", "chaincode") {
		t.Fatal("chaincode column not restored after re-up")
	}
	if _, ok := groupChaincode(t, s, "grp-rt"); ok {
		t.Fatal("rebuilt row gained a non-NULL chaincode; want NULL after rebuild")
	}

	// Idempotent re-up (applied versions not re-applied).
	if err := migrateUp(ctx, mustDB(t, s)); err != nil {
		t.Fatalf("idempotent re-up: %v", err)
	}
}
