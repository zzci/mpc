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

// 迁移由 goose 工具产出（非手写 schema），且可前进/回滚/幂等（database.md §1）。
func TestMigrations_UpDownIdempotent(t *testing.T) {
	ctx := context.Background()
	s := NewStore(filepath.Join(t.TempDir(), "coord.db"))
	if err := s.Unlock(ctx, []byte(testPass)); err != nil { // Unlock 内部跑 migrateUp
		t.Fatalf("unlock: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	wantTables := []string{
		"groups", "group_members", "signing_requests", "request_approvals",
		"request_events", "push_tokens", "admin_audit", "goose_db_version",
	}
	for _, tbl := range wantTables {
		if !tableExists(t, s, tbl) {
			t.Fatalf("table %q missing after migrate up", tbl)
		}
	}

	// 重复 up 幂等（已应用版本不重复执行）
	if err := migrateUp(ctx, mustDB(t, s)); err != nil {
		t.Fatalf("idempotent up: %v", err)
	}

	// 回滚到 0：业务表全部移除
	if err := migrateDownTo(ctx, mustDB(t, s), 0); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if tableExists(t, s, "signing_requests") {
		t.Fatal("signing_requests still present after down-to-0")
	}

	// 再次 up 可恢复
	if err := migrateUp(ctx, mustDB(t, s)); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	if !tableExists(t, s, "signing_requests") {
		t.Fatal("signing_requests not restored after re-up")
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
