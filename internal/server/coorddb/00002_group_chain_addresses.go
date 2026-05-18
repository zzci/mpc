package coorddb

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
)

// 迁移 v2：groups 增派生链地址列 evm_address / tron_address（L1 设计变更：group
// 须持久化派生实际链地址，地址≠单纯公钥；docs/design/server/database.md groups schema
// + 地址记录小节）。仍走 D-001 既有 goose 框架——schema 变更经「新增版本化迁移
// 文件 + goose_db_version 跟踪」（database.md §1：禁手改 schema）。
//
// 00001 为内嵌 SQL（embed FS 仅 *.sql）；本步含确定性地址派生（keccak256 +
// EIP-55 / Base58Check，非 SQL 可表达），故为 Go 迁移：embed FS 无对应 .go，
// goose 按「仅注册、不在 FS 提供 .go」整体并入（collectGoMigrations），版本号
// 取本文件名前缀 00002，与 SQL 迁移按版本统一排序；前进/回滚/幂等仍由
// goose_db_version 保证。
func init() {
	goose.AddMigrationContext(upGroupChainAddresses, downGroupChainAddresses)
}

// groupsBaseDDL 是 00001 中 groups 的原始列定义（不含派生地址列）。Down 以整表
// 重建移除新列：go-sqlcipher v4.4.2 内嵌 SQLite 早于 3.35，ALTER TABLE DROP
// COLUMN 不可依赖；groups 无索引/触发器、未启用 PRAGMA foreign_keys
// （00001_init.sql 注），整表重建版本无关且安全。
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

// upGroupChainAddresses 加 evm_address / tron_address（TEXT NOT NULL DEFAULT ”，
// 与 ProvisionGroup INSERT 列对齐、读路径免 NULL 处理），再就 ecdsa_pubkey 对
// 既有 groups 行 backfill 派生地址（无行则仅加列）。确定性、可回滚、幂等（由
// goose_db_version 跟踪，已应用版本不重复执行）。
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

// downGroupChainAddresses 以整表重建移除两列，保留全部既有业务列与数据。
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
