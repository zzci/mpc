package coorddb

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
)

// G-001 derived chain address persistence: ProvisionGroup must, within its
// single persistence transaction, derive and store the EIP-55 EVM and
// Base58Check TRON addresses from ecdsa_pubkey; the v2 migration adds the
// columns reversibly, idempotently, and backfills pre-existing rows.

// scalar1 well-known vectors, identical to internal/addr addr_test.go (the
// authoritative derivation under test is internal/addr — not re-implemented).
const (
	scalar1EVM  = "0x7E5F4552091A69125d5DfCb7b8C2659029395Bdf"
	scalar1Tron = "TMVQGm1qAQYVdetCeGRRkTWYYrLXuHK2HC"
)

func pubFromScalar(n byte) []byte {
	priv := make([]byte, 32)
	priv[31] = n
	_, pub := btcec.PrivKeyFromBytes(priv)
	return pub.SerializeUncompressed()
}

func groupAddrs(t *testing.T, s *Store, groupID string) (evm, tron string) {
	t.Helper()
	db, err := s.conn()
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	if err := db.QueryRowContext(context.Background(),
		`SELECT evm_address, tron_address FROM groups WHERE group_id = ?`, groupID).
		Scan(&evm, &tron); err != nil {
		t.Fatalf("query addrs: %v", err)
	}
	return evm, tron
}

func columnExists(t *testing.T, s *Store, table, col string) bool {
	t.Helper()
	db, err := s.conn()
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	rows, err := db.QueryContext(context.Background(),
		`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("pragma_table_info: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan col: %v", err)
		}
		if name == col {
			return true
		}
	}
	return false
}

func TestProvisionGroup_DerivesChainAddresses(t *testing.T) {
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())

	// Real uncompressed secp256k1 pubkey -> deterministic known vectors.
	if err := s.ProvisionGroup(ctx, GroupRecord{
		GroupID: "grp-derive", ECDSAPubkey: pubFromScalar(1), GroupPubkey: []byte{4, 5, 6},
		ThresholdT: 2, PartiesN: 3, Epoch: 0, CreatedAt: "2026-05-18T00:00:00Z",
	}, []MemberRecord{{MemberID: "m0", IdentityPubkey: []byte{7}}}); err != nil {
		t.Fatalf("provision: %v", err)
	}
	evm, tron := groupAddrs(t, s, "grp-derive")
	if evm != scalar1EVM {
		t.Errorf("evm_address = %q, want %q", evm, scalar1EVM)
	}
	if tron != scalar1Tron {
		t.Errorf("tron_address = %q, want %q", tron, scalar1Tron)
	}

	// Degenerate non-curve pubkey (low-level test stub shape): best-effort ->
	// empty addresses, provisioning still succeeds in one tx (protects the
	// existing D-001/S-002/X-001 tests that seed junk pubkeys).
	if err := s.ProvisionGroup(ctx, GroupRecord{
		GroupID: "grp-junk", ECDSAPubkey: []byte{1, 2, 3}, GroupPubkey: []byte{4},
		ThresholdT: 1, PartiesN: 1, Epoch: 0, CreatedAt: "2026-05-18T00:00:00Z",
	}, []MemberRecord{{MemberID: "m0", IdentityPubkey: []byte{7}}}); err != nil {
		t.Fatalf("provision junk: %v", err)
	}
	if evm, tron := groupAddrs(t, s, "grp-junk"); evm != "" || tron != "" {
		t.Errorf("junk pubkey addrs = (%q,%q), want empty", evm, tron)
	}
}

func TestMigrationV2_ReversibleIdempotentBackfill(t *testing.T) {
	ctx := context.Background()
	s := NewStore(filepath.Join(t.TempDir(), "coord.db"))
	if err := s.Unlock(ctx, []byte(testPass)); err != nil { // runs v1 + v2
		t.Fatalf("unlock: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if !columnExists(t, s, "groups", "evm_address") || !columnExists(t, s, "groups", "tron_address") {
		t.Fatal("v2 columns missing after migrate up")
	}

	// Down to v1: derived-address columns removed, base groups table intact.
	if err := migrateDownTo(ctx, mustDB(t, s), 1); err != nil {
		t.Fatalf("down to v1: %v", err)
	}
	if columnExists(t, s, "groups", "evm_address") || columnExists(t, s, "groups", "tron_address") {
		t.Fatal("v2 columns still present after down-to-1")
	}
	if !tableExists(t, s, "groups") {
		t.Fatal("groups table lost on v2 down (rebuild must preserve it)")
	}

	// Insert a pre-existing (legacy) groups row lacking derived addresses,
	// then re-up: v2 must backfill it from ecdsa_pubkey.
	if _, err := mustDB(t, s).ExecContext(ctx,
		`INSERT INTO groups (group_id, ecdsa_pubkey, threshold_t, parties_n, group_pubkey, epoch, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"grp-legacy", pubFromScalar(1), 2, 3, []byte{9}, 0,
		"2026-05-18T00:00:00Z", "2026-05-18T00:00:00Z"); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	if err := migrateUp(ctx, mustDB(t, s)); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	if evm, tron := groupAddrs(t, s, "grp-legacy"); evm != scalar1EVM || tron != scalar1Tron {
		t.Errorf("backfill addrs = (%q,%q), want (%q,%q)", evm, tron, scalar1EVM, scalar1Tron)
	}

	// Idempotent: repeat up is a no-op (goose version-tracked), row unchanged.
	if err := migrateUp(ctx, mustDB(t, s)); err != nil {
		t.Fatalf("idempotent re-up: %v", err)
	}
	if evm, _ := groupAddrs(t, s, "grp-legacy"); evm != scalar1EVM {
		t.Fatalf("idempotent re-up mutated backfill: %q", evm)
	}

	// Down again preserves the legacy row's base columns (rebuild copy).
	if err := migrateDownTo(ctx, mustDB(t, s), 1); err != nil {
		t.Fatalf("second down: %v", err)
	}
	var gid string
	if err := mustDB(t, s).QueryRowContext(ctx,
		`SELECT group_id FROM groups WHERE group_id = ?`, "grp-legacy").Scan(&gid); err != nil {
		t.Fatalf("legacy row lost on rebuild: %v", err)
	}
}
