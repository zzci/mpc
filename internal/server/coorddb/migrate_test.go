package coorddb

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func tableExists(t *testing.T, s *Store, name string) bool {
	t.Helper()
	db, err := s.conn()
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	var n int
	if err := db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	return n == 1
}

// Migrations are produced by goose (no hand-written schema) and are
// up/down/idempotent (database.md §1).
func TestMigrations_UpDownIdempotent(t *testing.T) {
	ctx := context.Background()
	s := NewStore(filepath.Join(t.TempDir(), "coord.db"))
	if err := s.Unlock(ctx, []byte(testPass)); err != nil { // Unlock runs migrateUp internally
		t.Fatalf("unlock: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	wantTables := []string{
		"groups", "group_members", "signing_requests", "request_approvals",
		"request_events", "admin_audit", "goose_db_version",
	}
	for _, tbl := range wantTables {
		if !tableExists(t, s, tbl) {
			t.Fatalf("table %q missing after migrate up", tbl)
		}
	}

	// idempotent re-up (applied versions are not re-applied)
	if err := migrateUp(ctx, mustDB(t, s)); err != nil {
		t.Fatalf("idempotent up: %v", err)
	}

	// down-to-0 removes all business tables
	if err := migrateDownTo(ctx, mustDB(t, s), 0); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if tableExists(t, s, "signing_requests") {
		t.Fatal("signing_requests still present after down-to-0")
	}

	// up again restores
	if err := migrateUp(ctx, mustDB(t, s)); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	if !tableExists(t, s, "signing_requests") {
		t.Fatal("signing_requests not restored after re-up")
	}
}

// TestMigration_DropPushTokensConverges proves the v3 push_tokens
// retirement keeps the fresh-DB and already-deployed-DB upgrade paths
// equivalent (charter rule 10 / database.md §1: schema change via a NEW
// versioned migration, never a hand-edit of finalized 00001_init.sql).
//
//   - fresh DB: full migrate up runs 00001..00003 -> push_tokens absent.
//   - deployed DB: a node that already applied through v2 (push_tokens
//     present from 00001) applies the remaining chain -> push_tokens
//     dropped. Both converge to the same schema.
func TestMigration_DropPushTokensConverges(t *testing.T) {
	ctx := context.Background()
	s := NewStore(filepath.Join(t.TempDir(), "coord.db"))
	if err := s.Unlock(ctx, []byte(testPass)); err != nil { // runs migrateUp (fresh path)
		t.Fatalf("unlock: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// Fresh path: after the full chain push_tokens must be gone.
	if tableExists(t, s, "push_tokens") {
		t.Fatal("fresh DB: push_tokens must be absent after migrate up (00003)")
	}

	// Simulate an already-deployed DB stopped at v2 (00001 + 00002
	// applied): rolling back to v2 re-creates push_tokens via 00003 Down,
	// reproducing the pre-v3 deployed schema.
	if err := migrateDownTo(ctx, mustDB(t, s), 2); err != nil {
		t.Fatalf("down to v2: %v", err)
	}
	if !tableExists(t, s, "push_tokens") {
		t.Fatal("deployed (v2) DB: push_tokens must exist before v3 is applied")
	}

	// The deployed node upgrades: applying the remaining chain drops it,
	// converging with the fresh path.
	if err := migrateUp(ctx, mustDB(t, s)); err != nil {
		t.Fatalf("upgrade up: %v", err)
	}
	if tableExists(t, s, "push_tokens") {
		t.Fatal("deployed DB after upgrade: push_tokens must be dropped (converged with fresh)")
	}
}

func mustDB(t *testing.T, s *Store) *sql.DB {
	t.Helper()
	db, err := s.conn()
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	return db
}
