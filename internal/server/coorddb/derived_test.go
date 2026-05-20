package coorddb

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// AD-6 group_derived_addresses: §7.bis persistence of HD-derived
// (group_id, child_index) -> (evm, tron) bindings. Coverage:
//   - 00005 migration is up/down/idempotent; the v5 table comes back on re-up
//     and the parent groups row survives the rebuild path.
//   - RegisterDerivedAddress inserts atomically; re-register with identical
//     addresses is idempotent (api.md B12); mismatch yields
//     ErrDerivedAddressConflict (handler -> 409 STATE_CONFLICT).
//   - Parent-group FK is enforced application-side (PRAGMA off, 00001 comment):
//     a missing groups row gives ErrDerivedGroupMissing.
//   - ListDerivedAddresses filters by since cursor and orders by
//     (created_at, child_index) so the cursor pages monotonically.
//   - child_index >= 2^31 is refused before any row is written, mirroring
//     the schema CHECK; defense-in-depth raw INSERT exercises the CHECK
//     itself so a future caller bypassing the repo cannot smuggle a
//     hardened index in.

const (
	gdaGroup     = "grp-gda"
	gdaEVM0      = "0x7E5F4552091A69125d5DfCb7b8C2659029395Bdf"
	gdaTron0     = "TMVQGm1qAQYVdetCeGRRkTWYYrLXuHK2HC"
	gdaEVMother  = "0x0000000000000000000000000000000000000000"
	gdaTronOther = "TLsV52sRDL79HXGGm9yzwKibb6BeruhUzy"
)

func seedDerivedGroup(t *testing.T, s *Store, groupID string) {
	t.Helper()
	if err := s.ProvisionGroup(context.Background(),
		GroupRecord{
			GroupID: groupID, ECDSAPubkey: pubFromScalar(1), GroupPubkey: []byte{4, 5, 6},
			ThresholdT: 2, PartiesN: 3, Epoch: 0, CreatedAt: "2026-05-20T00:00:00Z",
			Chaincode: bytes.Repeat([]byte{0xCC}, 32),
		},
		[]MemberRecord{{MemberID: "m0", IdentityPubkey: []byte{7, 8, 9}}}); err != nil {
		t.Fatalf("provision parent group: %v", err)
	}
}

func TestRegisterDerivedAddress_InsertAndIdempotent(t *testing.T) {
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())
	seedDerivedGroup(t, s, gdaGroup)

	rec := DerivedAddressRecord{
		GroupID: gdaGroup, ChildIndex: 5,
		EVMAddress: gdaEVM0, TronAddress: gdaTron0,
		ChildPubkey: bytes.Repeat([]byte{0xAB}, 33),
		CreatedAt:   1_700_000_000,
	}
	if err := s.RegisterDerivedAddress(ctx, rec); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Idempotent re-register with identical addresses: no error.
	if err := s.RegisterDerivedAddress(ctx, rec); err != nil {
		t.Fatalf("idempotent re-register: %v", err)
	}

	// List returns the single row.
	got, err := s.ListDerivedAddresses(ctx, gdaGroup, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("list len = %d, want 1", len(got))
	}
	if got[0].ChildIndex != 5 || got[0].EVMAddress != gdaEVM0 || got[0].TronAddress != gdaTron0 {
		t.Fatalf("row mismatch: %+v", got[0])
	}
	if !bytes.Equal(got[0].ChildPubkey, rec.ChildPubkey) {
		t.Fatalf("child_pubkey = %x, want %x", got[0].ChildPubkey, rec.ChildPubkey)
	}
	if got[0].CreatedAt != rec.CreatedAt {
		t.Fatalf("created_at = %d, want %d", got[0].CreatedAt, rec.CreatedAt)
	}
}

func TestRegisterDerivedAddress_ConflictMismatch(t *testing.T) {
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())
	seedDerivedGroup(t, s, gdaGroup)

	first := DerivedAddressRecord{
		GroupID: gdaGroup, ChildIndex: 9,
		EVMAddress: gdaEVM0, TronAddress: gdaTron0, CreatedAt: 1_700_000_000,
	}
	if err := s.RegisterDerivedAddress(ctx, first); err != nil {
		t.Fatalf("first register: %v", err)
	}

	clash := first
	clash.EVMAddress = gdaEVMother
	err := s.RegisterDerivedAddress(ctx, clash)
	if !errors.Is(err, ErrDerivedAddressConflict) {
		t.Fatalf("conflict register: got %v, want ErrDerivedAddressConflict", err)
	}

	// Stored row is unchanged after conflict (atomic rollback).
	got, err := s.ListDerivedAddresses(ctx, gdaGroup, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].EVMAddress != gdaEVM0 {
		t.Fatalf("conflict mutated row: %+v", got)
	}
}

func TestRegisterDerivedAddress_MissingGroup(t *testing.T) {
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())
	// No parent group seeded.

	err := s.RegisterDerivedAddress(ctx, DerivedAddressRecord{
		GroupID: "orphan", ChildIndex: 1,
		EVMAddress: gdaEVM0, TronAddress: gdaTron0, CreatedAt: 1_700_000_000,
	})
	if !errors.Is(err, ErrDerivedGroupMissing) {
		t.Fatalf("missing group: got %v, want ErrDerivedGroupMissing", err)
	}

	// No row inserted (atomic rollback through WithTx).
	got, err := s.ListDerivedAddresses(ctx, "orphan", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("orphan list len = %d, want 0", len(got))
	}
}

func TestRegisterDerivedAddress_RejectsHardenedIndex(t *testing.T) {
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())
	seedDerivedGroup(t, s, gdaGroup)

	// 2^31 is the first hardened index in BIP32 conventions; non-hardened HD
	// stops strictly below it. Application layer refuses before any tx runs.
	err := s.RegisterDerivedAddress(ctx, DerivedAddressRecord{
		GroupID: gdaGroup, ChildIndex: 1 << 31,
		EVMAddress: gdaEVM0, TronAddress: gdaTron0, CreatedAt: 1_700_000_000,
	})
	if err == nil {
		t.Fatal("accepted hardened child_index; want error")
	}
}

func TestRegisterDerivedAddress_DBLevelIndexCheck(t *testing.T) {
	// Defense-in-depth: a raw INSERT bypassing RegisterDerivedAddress must be
	// rejected by the schema CHECK in 00005_group_derived_addresses.sql.
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())
	seedDerivedGroup(t, s, gdaGroup)
	db := mustDB(t, s)

	// 2^31 (hardened boundary) is forbidden by the CHECK.
	_, err := db.ExecContext(ctx,
		`INSERT INTO group_derived_addresses
		 (group_id, child_index, evm_address, tron_address, child_pubkey, created_at)
		 VALUES (?, ?, ?, ?, NULL, ?)`,
		gdaGroup, int64(1)<<31, gdaEVM0, gdaTron0, 1_700_000_000)
	if err == nil {
		t.Fatal("raw INSERT with child_index=2^31 succeeded; CHECK missing")
	}

	// 0 and 2^31 - 1 are accepted by the same INSERT path.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO group_derived_addresses
		 (group_id, child_index, evm_address, tron_address, child_pubkey, created_at)
		 VALUES (?, ?, ?, ?, NULL, ?)`,
		gdaGroup, 0, gdaEVM0, gdaTron0, 1_700_000_000); err != nil {
		t.Fatalf("raw INSERT child_index=0 rejected: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO group_derived_addresses
		 (group_id, child_index, evm_address, tron_address, child_pubkey, created_at)
		 VALUES (?, ?, ?, ?, NULL, ?)`,
		gdaGroup, (int64(1)<<31)-1, gdaEVMother, gdaTronOther, 1_700_000_001); err != nil {
		t.Fatalf("raw INSERT child_index=2^31-1 rejected: %v", err)
	}
}

func TestListDerivedAddresses_SinceCursorOrdering(t *testing.T) {
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())
	seedDerivedGroup(t, s, gdaGroup)

	// Two seeds at distinct created_at, plus one tie at the latest second so
	// the secondary order (child_index ascending) is exercised.
	seeds := []DerivedAddressRecord{
		{GroupID: gdaGroup, ChildIndex: 1, EVMAddress: gdaEVM0, TronAddress: gdaTron0, CreatedAt: 100},
		{GroupID: gdaGroup, ChildIndex: 7, EVMAddress: gdaEVMother, TronAddress: gdaTronOther, CreatedAt: 200},
		{GroupID: gdaGroup, ChildIndex: 3, EVMAddress: gdaEVMother, TronAddress: gdaTronOther, CreatedAt: 200},
	}
	for _, r := range seeds {
		if err := s.RegisterDerivedAddress(ctx, r); err != nil {
			t.Fatalf("seed %+v: %v", r, err)
		}
	}

	// since=0 -> all three, ordered by (created_at, child_index).
	all, err := s.ListDerivedAddresses(ctx, gdaGroup, 0)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	gotIdx := []uint32{all[0].ChildIndex, all[1].ChildIndex, all[2].ChildIndex}
	wantIdx := []uint32{1, 3, 7}
	for i := range gotIdx {
		if gotIdx[i] != wantIdx[i] {
			t.Fatalf("ordering[%d] = %d, want %d (full=%v)", i, gotIdx[i], wantIdx[i], gotIdx)
		}
	}

	// since=100 (strictly greater) drops the first row.
	tail, err := s.ListDerivedAddresses(ctx, gdaGroup, 100)
	if err != nil {
		t.Fatalf("list tail: %v", err)
	}
	if len(tail) != 2 || tail[0].ChildIndex != 3 || tail[1].ChildIndex != 7 {
		t.Fatalf("tail = %+v, want indices [3,7]", tail)
	}

	// since=200 strictly excludes the tie batch -> empty.
	none, err := s.ListDerivedAddresses(ctx, gdaGroup, 200)
	if err != nil {
		t.Fatalf("list none: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("since=200 returned %d rows, want 0", len(none))
	}
}

func TestListDerivedAddresses_LockedFailClosed(t *testing.T) {
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())
	seedDerivedGroup(t, s, gdaGroup)
	if err := s.RegisterDerivedAddress(ctx, DerivedAddressRecord{
		GroupID: gdaGroup, ChildIndex: 1, EVMAddress: gdaEVM0, TronAddress: gdaTron0,
		CreatedAt: 1_700_000_000,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := s.Relock(); err != nil {
		t.Fatalf("relock: %v", err)
	}
	if _, err := s.ListDerivedAddresses(ctx, gdaGroup, 0); !errors.Is(err, ErrLocked) {
		t.Fatalf("list under LOCKED: got %v, want ErrLocked", err)
	}
	if err := s.RegisterDerivedAddress(ctx, DerivedAddressRecord{
		GroupID: gdaGroup, ChildIndex: 2, EVMAddress: gdaEVM0, TronAddress: gdaTron0,
		CreatedAt: 1_700_000_001,
	}); !errors.Is(err, ErrLocked) {
		t.Fatalf("register under LOCKED: got %v, want ErrLocked", err)
	}
}

func TestMigrationV5_UpDownIdempotent(t *testing.T) {
	ctx := context.Background()
	s := NewStore(filepath.Join(t.TempDir(), "coord.db"))
	if err := s.Unlock(ctx, []byte(testPass)); err != nil { // runs 00001..00005
		t.Fatalf("unlock: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if !tableExists(t, s, "group_derived_addresses") {
		t.Fatal("v5 table group_derived_addresses missing after migrate up")
	}

	// Seed a parent group + a derived row; v5 down drops the table, v5 up
	// restores it empty (a fresh DB has no rows to backfill, mirroring 00004
	// chaincode F5 behavior).
	seedDerivedGroup(t, s, "grp-rt")
	if err := s.RegisterDerivedAddress(ctx, DerivedAddressRecord{
		GroupID: "grp-rt", ChildIndex: 11, EVMAddress: gdaEVM0, TronAddress: gdaTron0,
		CreatedAt: 1_700_000_000,
	}); err != nil {
		t.Fatalf("register pre-down: %v", err)
	}

	// Down to v4: derived table disappears, parent groups row + chaincode
	// column survive (v5 only adds a new table, no rebuild needed).
	if err := migrateDownTo(ctx, mustDB(t, s), 4); err != nil {
		t.Fatalf("down to v4: %v", err)
	}
	if tableExists(t, s, "group_derived_addresses") {
		t.Fatal("group_derived_addresses still present after down-to-4")
	}
	if !columnExists(t, s, "groups", "chaincode") {
		t.Fatal("00004 chaincode column lost on v5 down (must not touch other migrations)")
	}
	var gid string
	if err := mustDB(t, s).QueryRowContext(ctx,
		`SELECT group_id FROM groups WHERE group_id = ?`, "grp-rt").Scan(&gid); err != nil {
		t.Fatalf("parent group lost on v5 down: %v", err)
	}

	// Re-up to v5: table comes back empty.
	if err := migrateUp(ctx, mustDB(t, s)); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	if !tableExists(t, s, "group_derived_addresses") {
		t.Fatal("group_derived_addresses not restored after re-up")
	}
	got, err := s.ListDerivedAddresses(ctx, "grp-rt", 0)
	if err != nil {
		t.Fatalf("list after re-up: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("re-up retained %d rows; want 0 (rebuild loses data)", len(got))
	}

	// Idempotent re-up (applied versions not re-applied).
	if err := migrateUp(ctx, mustDB(t, s)); err != nil {
		t.Fatalf("idempotent re-up: %v", err)
	}
}
