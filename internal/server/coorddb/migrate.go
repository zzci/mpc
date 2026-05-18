package coorddb

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sync"

	"github.com/pressly/goose/v3"
)

// Migrations are embedded in the binary; the schema is produced only by
// goose versioned migrations (database.md §1: no hand-edited schema).
// goose tracks applied versions in goose_db_version, supporting
// up/down and idempotent re-runs (applied versions are not re-applied).
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

const migrationsDir = "migrations"

var gooseInit sync.Once

func configureGoose() error {
	var err error
	gooseInit.Do(func() {
		goose.SetBaseFS(migrationsFS)
		goose.SetLogger(goose.NopLogger())
		err = goose.SetDialect("sqlite3")
	})
	return err
}

// migrateUp advances to the latest version on the (key-mounted)
// encrypted connection.
func migrateUp(ctx context.Context, db *sql.DB) error {
	if err := configureGoose(); err != nil {
		return fmt.Errorf("coorddb: goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db, migrationsDir); err != nil {
		return fmt.Errorf("coorddb: migrate up: %w", err)
	}
	return nil
}

// migrateDownTo rolls back to the given version (version=0 = full
// rollback); for migration-reversibility checks / ops use only.
func migrateDownTo(ctx context.Context, db *sql.DB, version int64) error {
	if err := configureGoose(); err != nil {
		return fmt.Errorf("coorddb: goose dialect: %w", err)
	}
	if err := goose.DownToContext(ctx, db, migrationsDir, version); err != nil {
		return fmt.Errorf("coorddb: migrate down: %w", err)
	}
	return nil
}
