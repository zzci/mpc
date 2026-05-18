package coorddb

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/mutecomm/go-sqlcipher/v4" // 同一驱动；presence 不传 key → 普通明文 sqlite
)

// Presence 是心跳在线集（database.md §11/§27、§143）：内存 SQLite（:memory:）、
// 不持久、不加密、无敏感数据。SQLite 无原生 TTL → 以 expires_at 列 + 周期清理
// 实现过期语义。与 Store 的 LOCKED 生命周期正交：presence 始终可用
// （重启即空，成员重连后由心跳重建）。
type Presence struct {
	db     *sql.DB
	ttl    time.Duration
	cancel context.CancelFunc
	wg     sync.WaitGroup
	closed atomic.Bool
}

// OnlineMember 是一名在线成员的快照。
type OnlineMember struct {
	GroupID     string
	MemberID    string
	RelayPeerID string
	TS          int64 // unix ns
}

var presenceSeq atomic.Int64

// NewPresence 建一个内存在线集并启动周期清理。ttl 为心跳有效期，
// cleanupInterval 为过期清理周期。
func NewPresence(ttl, cleanupInterval time.Duration) (*Presence, error) {
	if ttl <= 0 || cleanupInterval <= 0 {
		return nil, fmt.Errorf("coorddb: presence ttl/interval must be > 0")
	}
	// 唯一名 + shared cache：保证多连接共享同一内存库，且生命周期内不被回收。
	name := fmt.Sprintf("coorddb_presence_%d", presenceSeq.Add(1))
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_busy_timeout=5000", name)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("coorddb: open presence: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxIdleTime(0)

	if _, err := db.ExecContext(context.Background(),
		`CREATE TABLE presence (
			group_id      TEXT    NOT NULL,
			member_id     TEXT    NOT NULL,
			relay_peer_id TEXT    NOT NULL,
			ts            INTEGER NOT NULL,
			expires_at    INTEGER NOT NULL,
			PRIMARY KEY (group_id, member_id)
		)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("coorddb: create presence table: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &Presence{db: db, ttl: ttl, cancel: cancel}
	p.wg.Add(1)
	go p.cleanupLoop(ctx, cleanupInterval)
	return p, nil
}

// Heartbeat 记录/刷新一名成员在线（upsert）。expires_at = now + ttl。
func (p *Presence) Heartbeat(ctx context.Context, groupID, memberID, relayPeerID string) error {
	now := time.Now().UnixNano()
	exp := now + p.ttl.Nanoseconds()
	if _, err := p.db.ExecContext(ctx,
		`INSERT INTO presence (group_id, member_id, relay_peer_id, ts, expires_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(group_id, member_id)
		 DO UPDATE SET relay_peer_id = excluded.relay_peer_id,
		               ts = excluded.ts,
		               expires_at = excluded.expires_at`,
		groupID, memberID, relayPeerID, now, exp); err != nil {
		return fmt.Errorf("coorddb: presence heartbeat: %w", err)
	}
	return nil
}

// Online 返回某组当前未过期的在线成员（过期项即便尚未被清理也不返回）。
func (p *Presence) Online(ctx context.Context, groupID string) ([]OnlineMember, error) {
	now := time.Now().UnixNano()
	rows, err := p.db.QueryContext(ctx,
		`SELECT group_id, member_id, relay_peer_id, ts
		 FROM presence WHERE group_id = ? AND expires_at > ?`,
		groupID, now)
	if err != nil {
		return nil, fmt.Errorf("coorddb: presence online: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []OnlineMember
	for rows.Next() {
		var m OnlineMember
		if err := rows.Scan(&m.GroupID, &m.MemberID, &m.RelayPeerID, &m.TS); err != nil {
			return nil, fmt.Errorf("coorddb: presence scan: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("coorddb: presence rows: %w", err)
	}
	return out, nil
}

// Close 停止清理协程并卸载内存库。
func (p *Presence) Close() error {
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}
	p.cancel()
	p.wg.Wait()
	if err := p.db.Close(); err != nil {
		return fmt.Errorf("coorddb: close presence: %w", err)
	}
	return nil
}

func (p *Presence) cleanupLoop(ctx context.Context, interval time.Duration) {
	defer p.wg.Done()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := p.db.ExecContext(ctx,
				`DELETE FROM presence WHERE expires_at <= ?`, time.Now().UnixNano()); err != nil {
				// 内存库清理失败不致命（下轮重试）；不持久、无敏感数据。
				continue
			}
		}
	}
}
