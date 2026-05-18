package coorddb

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sync"

	"github.com/pressly/goose/v3"
)

// 迁移文件随二进制内嵌；schema 仅由 goose 版本化迁移驱动产出（database.md §1：
// 禁手改 schema）。goose 经 goose_db_version 表跟踪已应用版本，支持前进/回滚、
// 重复执行幂等（已应用版本不重复执行）。
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

// migrateUp 在（已用密钥挂载的）加密连接上前进到最新版本。
func migrateUp(ctx context.Context, db *sql.DB) error {
	if err := configureGoose(); err != nil {
		return fmt.Errorf("coorddb: goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db, migrationsDir); err != nil {
		return fmt.Errorf("coorddb: migrate up: %w", err)
	}
	return nil
}

// migrateDownTo 回滚到指定版本（version=0 即全回滚）；仅供迁移可逆性验证/运维使用。
func migrateDownTo(ctx context.Context, db *sql.DB, version int64) error {
	if err := configureGoose(); err != nil {
		return fmt.Errorf("coorddb: goose dialect: %w", err)
	}
	if err := goose.DownToContext(ctx, db, migrationsDir, version); err != nil {
		return fmt.Errorf("coorddb: migrate down: %w", err)
	}
	return nil
}
