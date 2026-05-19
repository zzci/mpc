package coorddb

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"sync"

	_ "github.com/mutecomm/go-sqlcipher/v4" // registers the encrypted sqlite3 driver (cgo / SQLCipher)
)

// Store is the coord whole-DB-encrypted persistence + LOCKED lifecycle
// (server.md C9b, server/database.md §7).
//
//	start → LOCKED (db not mounted, no in-memory key)
//	  ──Unlock(passphrase→Argon2id→raw key→mount+migrate)──▶ UNLOCKED (serving)
//	  ──Relock(zeroize in-memory key + unmount)──▶ LOCKED
//
// Under LOCKED every data access fail-closes with ErrLocked (callers map
// it to 503 LOCKED). The passphrase is only passed in via Unlock; this
// module never persists it nor reads config/env; the key lives in memory
// only and is zeroized on relock.
type Store struct {
	dbPath string

	// plaintext=true is the dev/test encryption-disabled mode
	// (database.md §7.1): OpenInsecure mounts an unencrypted DB, derives
	// no key, has no LOCKED state; Unlock is rejected. The production
	// iron-law guardrail blocks misuse at node startup
	// (internal/server Validate); this field only selects the mount mode.
	plaintext bool

	mu  sync.RWMutex
	db  *sql.DB
	key []byte // Argon2id-derived raw key; memory only, zeroized on Relock
}

// NewStore builds an encrypted Store in LOCKED state (no connection, no
// key derivation). dbPath is the encrypted single-file path, injected by
// the caller (X-001) after resolving config.
func NewStore(dbPath string) *Store {
	return &Store{dbPath: dbPath}
}

// NewPlaintextStore builds a dev/test encryption-disabled Store
// (database.md §7.1). Still unmounted — the caller must OpenInsecure —
// but it mounts an UNENCRYPTED DB, derives no key, and has no LOCKED
// lifecycle. Non-production only; the production iron-law guardrail
// fail-closes at node startup (internal/server Validate), so this type
// does not re-check it.
func NewPlaintextStore(dbPath string) *Store {
	return &Store{dbPath: dbPath, plaintext: true}
}

// Unlock derives the key from the passphrase, mounts the encrypted DB
// and runs migrations forward, transitioning to UNLOCKED. The passphrase
// is supplied by the caller (admin/X-001 via the N-001
// server.UnlockProvider); this method does not copy or persist it — the
// caller should zeroize its own passphrase buffer after return.
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
	// coord is a single-node single-writer (database.md §1/§5): one
	// connection + BEGIN IMMEDIATE (_txlock=immediate) serializes write
	// transactions and avoids SQLITE_BUSY.
	db.SetMaxOpenConns(1)

	// Passphrase check: with a wrong key SQLCipher cannot decrypt pages,
	// so reading sqlite_master fails — the access-layer expression of
	// "a leaked .db file is unreadable without the passphrase".
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

// OpenInsecure is the mount entry for dev/test encryption-disabled mode
// (database.md §7.1): it opens an UNENCRYPTED SQLite (no _pragma_key, no
// key derivation), runs migrations and becomes UNLOCKED-equivalent so
// coord serves immediately and data endpoints are ready at once — this
// is what lets E2E run the full ring without an unlock-timing harness
// hack. Only a plaintext Store may call it; an encrypted Store returns
// ErrNotPlaintext.
//
// The production iron-law guardrail blocks misuse at node startup
// (missing ALLOW_INSECURE_DB=1 = fail-closed refuse), so by the time
// this method is reached it is a confirmed non-production path; a
// disabled-mode DB must never be used for a real deployment.
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

// Relock zeroizes the in-memory key and unmounts the DB, returning to
// LOCKED. Idempotent: a no-op if already LOCKED.
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

// Close is equivalent to Relock, for process exit / test cleanup.
func (s *Store) Close() error { return s.Relock() }

// IsUnlocked reports whether the store is currently unlocked.
func (s *Store) IsUnlocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db != nil
}

// conn returns the mounted connection; fail-closes with ErrLocked under
// LOCKED.
func (s *Store) conn() (*sql.DB, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil, ErrLocked
	}
	return s.db, nil
}

// WithTx runs fn inside a write transaction; the transaction is BEGIN
// IMMEDIATE (_txlock=immediate) to serialize writers (database.md §5).
// fn returning an error or panicking rolls back. Fail-closed under LOCKED.
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

// dsn builds the encrypted connection string in raw-key mode: the 32B
// Argon2id-derived key is passed to SQLCipher as x'<hex>' (skipping its
// built-in KDF — we derive with Argon2id); SQLCipher stores the salt in
// the DB header (non-secret).
func (s *Store) dsn(key []byte) string {
	q := url.Values{}
	q.Set("_pragma_key", fmt.Sprintf("x'%s'", hex.EncodeToString(key)))
	q.Set("_busy_timeout", "5000")
	q.Set("_journal_mode", "WAL")
	q.Set("_txlock", "immediate")
	return "file:" + s.dbPath + "?" + q.Encode()
}

// dsnPlaintext builds a connection string WITHOUT _pragma_key
// (database.md §7.1 disabled mode): with no key the SQLCipher driver is
// equivalent to standard unencrypted SQLite and the .db is plaintext on
// disk. dev/test only.
func (s *Store) dsnPlaintext() string {
	q := url.Values{}
	q.Set("_busy_timeout", "5000")
	q.Set("_journal_mode", "WAL")
	q.Set("_txlock", "immediate")
	return "file:" + s.dbPath + "?" + q.Encode()
}
