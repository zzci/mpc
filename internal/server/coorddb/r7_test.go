package coorddb

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// R7 (distributed-mpc.md R7 / impl §E): groups.ecdsa_pubkey is
// append-only. The application-layer guardR7AppendOnly is the primary
// refusal; the 00006 migration's SQLite triggers form the deep-defense
// second layer. These tests exercise both layers against the three
// negative cases the design enumerates:
//
//   (a) write NULL/empty as the first ecdsa_pubkey  → application guard
//   (b) UPDATE an existing pubkey to a different value → DB trigger
//   (c) UPDATE an existing pubkey to NULL → DB trigger
//   (d) DELETE a row with an existing pubkey → DB trigger

func TestR7_AppLayer_EmptyPubkeyRejected(t *testing.T) {
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())
	// ProvisionGroup with an empty (zero-length) ecdsa_pubkey: the
	// in-transaction guard refuses before the INSERT runs.
	err := s.ProvisionGroup(ctx,
		GroupRecord{
			GroupID: "g-empty", ECDSAPubkey: nil, GroupPubkey: []byte("gp"),
			ThresholdT: 2, PartiesN: 3, CreatedAt: "2026-05-20T00:00:00Z",
		},
		[]MemberRecord{{MemberID: "m0", IdentityPubkey: []byte{1, 2, 3}}})
	if !errors.Is(err, ErrR7Violation) {
		t.Fatalf("want ErrR7Violation, got %v", err)
	}
}

func TestR7_AppLayer_OverwriteDifferentRejected(t *testing.T) {
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())
	// Seed a fresh row, then attempt a second ProvisionGroup with the
	// same group_id but a different pubkey. The PK conflict would catch
	// this even without R7, but the R7 guard fires first inside the
	// transaction with a clearer error.
	if err := s.ProvisionGroup(ctx,
		GroupRecord{
			GroupID: "g-r7", ECDSAPubkey: []byte("pubkey-v1"), GroupPubkey: []byte("gp"),
			ThresholdT: 2, PartiesN: 3, CreatedAt: "2026-05-20T00:00:00Z",
		},
		[]MemberRecord{{MemberID: "m0", IdentityPubkey: []byte{1}}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	err := s.ProvisionGroup(ctx,
		GroupRecord{
			GroupID: "g-r7", ECDSAPubkey: []byte("pubkey-v2-different"), GroupPubkey: []byte("gp"),
			ThresholdT: 2, PartiesN: 3, CreatedAt: "2026-05-20T00:00:00Z",
		},
		[]MemberRecord{{MemberID: "m0", IdentityPubkey: []byte{1}}})
	if !errors.Is(err, ErrR7Violation) {
		t.Fatalf("want ErrR7Violation, got %v", err)
	}
}

func TestR7_DBLayer_TriggersBlockUpdateAndDelete(t *testing.T) {
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())
	if err := s.ProvisionGroup(ctx,
		GroupRecord{
			GroupID: "g-trig", ECDSAPubkey: []byte("pubkey-v1"), GroupPubkey: []byte("gp"),
			ThresholdT: 2, PartiesN: 3, CreatedAt: "2026-05-20T00:00:00Z",
		},
		[]MemberRecord{{MemberID: "m0", IdentityPubkey: []byte{1}}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	db := mustDB(t, s)

	// (b) UPDATE to a different non-NULL value → trigger ABORTs.
	if _, err := db.ExecContext(ctx,
		`UPDATE groups SET ecdsa_pubkey = ? WHERE group_id = ?`, []byte("pubkey-v2"), "g-trig"); err == nil {
		t.Fatal("UPDATE to different value: want trigger ABORT, got nil")
	} else if !strings.Contains(err.Error(), "R7") {
		t.Fatalf("UPDATE different: want R7 message, got %v", err)
	}

	// (c) UPDATE to NULL → trigger ABORTs.
	if _, err := db.ExecContext(ctx,
		`UPDATE groups SET ecdsa_pubkey = NULL WHERE group_id = ?`, "g-trig"); err == nil {
		t.Fatal("UPDATE to NULL: want trigger ABORT, got nil")
	} else if !strings.Contains(err.Error(), "R7") {
		t.Fatalf("UPDATE NULL: want R7 message, got %v", err)
	}

	// (d) DELETE row with non-NULL pubkey → trigger ABORTs.
	if _, err := db.ExecContext(ctx,
		`DELETE FROM groups WHERE group_id = ?`, "g-trig"); err == nil {
		t.Fatal("DELETE keyed row: want trigger ABORT, got nil")
	} else if !strings.Contains(err.Error(), "R7") {
		t.Fatalf("DELETE: want R7 message, got %v", err)
	}

	// Sanity: pubkey is unchanged after the failed mutations.
	var pub []byte
	if err := db.QueryRowContext(ctx,
		`SELECT ecdsa_pubkey FROM groups WHERE group_id = ?`, "g-trig").Scan(&pub); err != nil {
		t.Fatalf("post-read: %v", err)
	}
	if string(pub) != "pubkey-v1" {
		t.Fatalf("pubkey mutated despite trigger ABORT: %q", string(pub))
	}
}

func TestR7_DBLayer_UpdateSameValueAllowed(t *testing.T) {
	// An UPDATE that doesn't change ecdsa_pubkey is a no-op for R7 and
	// must succeed (so legitimate touch-row updates of OTHER columns,
	// e.g. updated_at, still work).
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())
	if err := s.ProvisionGroup(ctx,
		GroupRecord{
			GroupID: "g-same", ECDSAPubkey: []byte("pk"), GroupPubkey: []byte("gp"),
			ThresholdT: 2, PartiesN: 3, CreatedAt: "2026-05-20T00:00:00Z",
		},
		[]MemberRecord{{MemberID: "m0", IdentityPubkey: []byte{1}}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	db := mustDB(t, s)
	// touch the updated_at column without changing ecdsa_pubkey.
	if _, err := db.ExecContext(ctx,
		`UPDATE groups SET updated_at = ? WHERE group_id = ?`, "2026-05-20T01:00:00Z", "g-same"); err != nil {
		t.Fatalf("unrelated UPDATE blocked by R7 trigger: %v", err)
	}
}

func TestR7_Migration_TriggersExist(t *testing.T) {
	ctx := context.Background()
	s := NewStore(filepath.Join(t.TempDir(), "coord.db"))
	if err := s.Unlock(ctx, []byte(testPass)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	db := mustDB(t, s)
	var (
		hasAppend int
		hasDelete int
	)
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name=?`,
		"trg_groups_ecdsa_pubkey_append_only").Scan(&hasAppend); err != nil {
		t.Fatalf("query append trigger: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name=?`,
		"trg_groups_ecdsa_pubkey_no_delete").Scan(&hasDelete); err != nil {
		t.Fatalf("query delete trigger: %v", err)
	}
	if hasAppend != 1 || hasDelete != 1 {
		t.Fatalf("R7 triggers missing: append=%d delete=%d", hasAppend, hasDelete)
	}
	// Down to v5 (one step back) removes the triggers; re-up restores
	// them — proving the goose migration is reversible (charter-10).
	if err := migrateDownTo(ctx, db, 5); err != nil {
		t.Fatalf("down to v5: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name=?`,
		"trg_groups_ecdsa_pubkey_append_only").Scan(&hasAppend); err != nil {
		t.Fatalf("query post-down: %v", err)
	}
	if hasAppend != 0 {
		t.Fatal("trigger persisted after down")
	}
	if err := migrateUp(ctx, db); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name=?`,
		"trg_groups_ecdsa_pubkey_append_only").Scan(&hasAppend); err != nil {
		t.Fatalf("query re-up: %v", err)
	}
	if hasAppend != 1 {
		t.Fatal("trigger not restored after re-up")
	}
}

// silence unused import if any
var _ sql.NullByte
