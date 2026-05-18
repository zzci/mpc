package coorddb

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"sync"

	_ "github.com/mutecomm/go-sqlcipher/v4" // 注册加密 sqlite3 驱动（cgo / SQLCipher）
)

// Store 是 coord 整库加密持久库 + LOCKED 生命周期（docs/design/server/server.md C9b、
// server/database.md §7）。
//
//	启动 → LOCKED（db 未挂载、内存无密钥）
//	  ──Unlock(口令→Argon2id→raw key→挂载+迁移)──▶ UNLOCKED（正常服务）
//	  ──Relock(清零内存密钥+卸载)──▶ LOCKED
//
// LOCKED 下一切数据访问 fail-closed 返回 ErrLocked（上层映射 503 LOCKED）。
// 口令仅经 Unlock 入参传入，本模块不持久化、不读配置/env；密钥仅驻内存、重锁即清零。
type Store struct {
	dbPath string

	// plaintext 为 true 时为 dev/test 整库加密禁用模式（database.md §7.1）：
	// OpenInsecure 直接挂载未加密库、不派生密钥、不经 LOCKED；Unlock 被拒。
	// 生产铁律护栏在 node 启动校验(internal/node Validate)拦截误用,本字段
	// 仅决定挂载方式。
	plaintext bool

	mu  sync.RWMutex
	db  *sql.DB
	key []byte // Argon2id 派生的 raw 加密密钥；仅内存，Relock 时 zeroize
}

// NewStore 构造一个处于 LOCKED 的加密 Store（不连接、不派生密钥）。dbPath 为
// 加密单文件路径，由上层（X-001）依配置解析后注入。
func NewStore(dbPath string) *Store {
	return &Store{dbPath: dbPath}
}

// NewPlaintextStore 构造一个 dev/test 整库加密禁用模式的 Store（database.md
// §7.1）。仍处于未挂载态——调用方须 OpenInsecure 才就绪——但挂载的是**未加密**
// 库、不派生密钥、不经 LOCKED 生命周期。仅供非生产；生产铁律护栏由
// internal/node Validate 在 node 启动期 fail-closed 拦截，本类型不重复判定。
func NewPlaintextStore(dbPath string) *Store {
	return &Store{dbPath: dbPath, plaintext: true}
}

// Unlock 用口令派生密钥、挂载加密库并前进迁移。成功后转 UNLOCKED。
// 口令由调用方（admin/X-001 经 N-001 node.UnlockProvider 获取）传入；本方法
// 不复制、不持久化口令，调用方应在返回后自行 zeroize 其口令缓冲。
func (s *Store) Unlock(ctx context.Context, passphrase []byte) error {
	if s.plaintext {
		return ErrPlaintextMode
	}
	if len(passphrase) == 0 {
		return ErrEmptyPassphrase
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return ErrUnlocked
	}

	params, err := loadOrInitKDF(s.dbPath)
	if err != nil {
		return err
	}
	key, err := deriveKey(passphrase, params)
	if err != nil {
		return err
	}

	db, err := sql.Open("sqlite3", s.dsn(key))
	if err != nil {
		zeroize(key)
		return fmt.Errorf("coorddb: open: %w", err)
	}
	// coord 单节点单写者（database.md §1/§5）：单连接 + BEGIN IMMEDIATE
	// （_txlock=immediate）串行化写事务，规避 SQLITE_BUSY。
	db.SetMaxOpenConns(1)

	// 校验口令：错误密钥下 SQLCipher 无法解出页 → 读 sqlite_master 即失败，
	// 等价「泄露 .db 文件无口令不可读」在访问层的体现。
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		zeroize(key)
		return fmt.Errorf("coorddb: unlock failed (bad passphrase or corrupt db): %w", err)
	}
	if _, err := db.ExecContext(ctx, "SELECT count(*) FROM sqlite_master"); err != nil {
		_ = db.Close()
		zeroize(key)
		return fmt.Errorf("coorddb: unlock failed (bad passphrase or corrupt db): %w", err)
	}

	if err := migrateUp(ctx, db); err != nil {
		_ = db.Close()
		zeroize(key)
		return err
	}

	s.db = db
	s.key = key
	return nil
}

// OpenInsecure 是 dev/test 整库加密禁用模式（database.md §7.1）的挂载入口：
// 打开**未加密** SQLite（无 _pragma_key、不派生密钥）、前进迁移并转 UNLOCKED-
// 等价,使 coord 启动即可服务、数据端点立即就绪——E2E 据此跑通完整环,不再用
// harness 解锁时序 hack。仅 plaintext Store 可调；加密 Store 调用即 ErrNotPlaintext。
//
// 生产铁律护栏在 node 启动校验拦截误用(缺 ALLOW_INSECURE_DB=1 即 fail-closed
// 拒启动),故本方法到达时已是经确认的非生产路径;禁用态库不得用于真实部署。
func (s *Store) OpenInsecure(ctx context.Context) error {
	if !s.plaintext {
		return ErrNotPlaintext
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return ErrUnlocked
	}

	db, err := sql.Open("sqlite3", s.dsnPlaintext())
	if err != nil {
		return fmt.Errorf("coorddb: open (plaintext): %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("coorddb: open plaintext db: %w", err)
	}
	if err := migrateUp(ctx, db); err != nil {
		_ = db.Close()
		return err
	}
	s.db = db
	return nil
}

// Relock 清零内存密钥并卸载库，回到 LOCKED。幂等：已 LOCKED 时直接返回。
func (s *Store) Relock() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	zeroize(s.key)
	s.key = nil
	if err != nil {
		return fmt.Errorf("coorddb: relock close: %w", err)
	}
	return nil
}

// Close 等价 Relock，用于进程退出/测试清理。
func (s *Store) Close() error { return s.Relock() }

// IsUnlocked 报告当前是否已解锁。
func (s *Store) IsUnlocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db != nil
}

// conn 返回已挂载连接；LOCKED 时 fail-closed 返回 ErrLocked。
func (s *Store) conn() (*sql.DB, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil, ErrLocked
	}
	return s.db, nil
}

// WithTx 在写事务内执行 fn；事务为 BEGIN IMMEDIATE（_txlock=immediate）以串行化
// 写者（database.md §5）。fn 返回错误或 panic 即回滚。LOCKED 下 fail-closed。
func (s *Store) WithTx(ctx context.Context, fn func(*sql.Tx) error) (err error) {
	db, err := s.conn()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("coorddb: begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("coorddb: commit: %w", err)
	}
	return nil
}

// dsn 以 raw key 模式构造加密连接串：32B Argon2id 派生密钥以 x'<hex>' 直传
// SQLCipher（跳过其内置 KDF，由我们用 Argon2id 派生）；salt 由 SQLCipher 存于
// 库头（非机密）。
func (s *Store) dsn(key []byte) string {
	q := url.Values{}
	q.Set("_pragma_key", fmt.Sprintf("x'%s'", hex.EncodeToString(key)))
	q.Set("_busy_timeout", "5000")
	q.Set("_journal_mode", "WAL")
	q.Set("_txlock", "immediate")
	return "file:" + s.dbPath + "?" + q.Encode()
}

// dsnPlaintext 构造**无 _pragma_key** 的连接串（database.md §7.1 禁用模式）：
// SQLCipher 驱动未提供密钥即等价标准未加密 SQLite,`.db` 明文落盘。仅 dev/test。
func (s *Store) dsnPlaintext() string {
	q := url.Values{}
	q.Set("_busy_timeout", "5000")
	q.Set("_journal_mode", "WAL")
	q.Set("_txlock", "immediate")
	return "file:" + s.dbPath + "?" + q.Encode()
}
