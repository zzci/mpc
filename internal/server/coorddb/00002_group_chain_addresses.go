package coorddb

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
)

// Migration v2: groups gains derived chain-address columns
// evm_address / tron_address (L1 design change: a group must persist its
// derived actual chain addresses; an address is not the bare pubkey;
// database.md groups schema + address-record). Still uses D-001's
// existing goose framework — a schema change goes through "a new
// versioned migration file + goose_db_version tracking" (database.md §1:
// no hand-edited schema).
//
// 00001 is embedded SQL (the embed FS is *.sql only); this step performs
// deterministic address derivation (keccak256 + EIP-55 / Base58Check,
// not expressible in SQL), so it is a Go migration: no .go in the embed
// FS, goose folds it in via collectGoMigrations ("registered only, not
// provided in the FS"), the version is this file's 00002 prefix, ordered
// uniformly by version with the SQL migrations; up/down/idempotent are
// still guaranteed by goose_db_version.
func init() {
	goose.AddMigrationContext(upGroupChainAddresses, downGroupChainAddresses)
}

// groupsBaseDDL is the original groups column definition from 00001
// (without the derived-address columns). Down removes the new columns by
// rebuilding the whole table: go-sqlcipher v4.4.2 embeds a SQLite older
// than 3.35, so ALTER TABLE DROP COLUMN cannot be relied on; groups has
// no indexes/triggers and PRAGMA foreign_keys is not enabled
// (00001_init.sql note), so a full-table rebuild is version-independent
// and safe.
const groupsBaseDDL = `CREATE TABLE groups (
    group_id     TEXT    PRIMARY KEY,
    ecdsa_pubkey BLOB    NOT NULL,
    threshold_t  INTEGER NOT NULL,
    parties_n    INTEGER NOT NULL,
    group_pubkey BLOB    NOT NULL,
    epoch        INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT    NOT NULL,
    updated_at   TEXT    NOT NULL
)`

const groupsBaseCols = `group_id, ecdsa_pubkey, threshold_t, parties_n, group_pubkey, epoch, created_at, updated_at`

// upGroupChainAddresses adds evm_address / tron_address (TEXT NOT NULL
// DEFAULT ”, aligned with the ProvisionGroup INSERT columns so the read
// path needs no NULL handling), then backfills derived addresses for
// existing groups rows from ecdsa_pubkey (adds columns only if no rows).
// Deterministic, reversible, idempotent (tracked by goose_db_version;
// applied versions are not re-applied).
func upGroupChainAddresses(ctx context.Context, tx *sql.Tx) error {
	for _, stmt := range []string{
		`ALTER TABLE groups ADD COLUMN evm_address  TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE groups ADD COLUMN tron_address TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("coorddb: migrate v2 add column: %w", err)
		}
	}

	rows, err := tx.QueryContext(ctx, `SELECT group_id, ecdsa_pubkey FROM groups`)
	if err != nil {
		return fmt.Errorf("coorddb: migrate v2 scan groups: %w", err)
	}
	type backfill struct {
		groupID   string
		evm, tron string
	}
	var todo []backfill
	for rows.Next() {
		var gid string
		var pub []byte
		if err := rows.Scan(&gid, &pub); err != nil {
			_ = rows.Close()
			return fmt.Errorf("coorddb: migrate v2 scan row: %w", err)
		}
		evm, tron := deriveChainAddrs(pub)
		todo = append(todo, backfill{groupID: gid, evm: evm, tron: tron})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("coorddb: migrate v2 rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("coorddb: migrate v2 close: %w", err)
	}
	for _, b := range todo {
		if _, err := tx.ExecContext(ctx,
			`UPDATE groups SET evm_address = ?, tron_address = ? WHERE group_id = ?`,
			b.evm, b.tron, b.groupID); err != nil {
			return fmt.Errorf("coorddb: migrate v2 backfill %s: %w", b.groupID, err)
		}
	}
	return nil
}

// downGroupChainAddresses removes the two columns by rebuilding the
// whole table, preserving all existing business columns and data.
func downGroupChainAddresses(ctx context.Context, tx *sql.Tx) error {
	stmts := []string{
		`ALTER TABLE groups RENAME TO groups_g001_drop`,
		groupsBaseDDL,
		`INSERT INTO groups (` + groupsBaseCols + `) SELECT ` + groupsBaseCols + ` FROM groups_g001_drop`,
		`DROP TABLE groups_g001_drop`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("coorddb: migrate v2 down: %w", err)
		}
	}
	return nil
}
