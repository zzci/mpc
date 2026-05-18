package coorddb

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/mutecomm/go-sqlcipher/v4" // same driver; presence passes no key -> plain (unencrypted) sqlite
)

// Presence is the heartbeat online-set (database.md §11/§27, §143): an
// in-memory SQLite (:memory:), not persisted, not encrypted, no
// sensitive data. SQLite has no native TTL, so expiry is an expires_at
// column + periodic cleanup. Orthogonal to the Store LOCKED lifecycle:
// presence is always available (empty on restart, rebuilt by heartbeats
// once members reconnect).
type Presence struct {
	db     *sql.DB
	ttl    time.Duration
	cancel context.CancelFunc
	wg     sync.WaitGroup
	closed atomic.Bool
}

// OnlineMember is a snapshot of one online member.
type OnlineMember struct {
	GroupID     string
	MemberID    string
	RelayPeerID string
	TS          int64 // unix ns
}

var presenceSeq atomic.Int64

// NewPresence builds an in-memory online-set and starts periodic
// cleanup. ttl is the heartbeat validity; cleanupInterval is the
// expiry-sweep period.
func NewPresence(ttl, cleanupInterval time.Duration) (*Presence, error) {
	if ttl <= 0 || cleanupInterval <= 0 {
		return nil, fmt.Errorf("coorddb: presence ttl/interval must be > 0")
	}
	// unique name + shared cache: all connections share one in-memory DB, not reclaimed for its lifetime.
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

// Heartbeat records/refreshes a member as online (upsert). expires_at = now + ttl.
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

// Online returns a group's currently non-expired online members (expired entries are excluded even if not yet cleaned).
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

// Close stops the cleanup goroutine and unmounts the in-memory DB.
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
				// in-memory cleanup failure is non-fatal (retried next round); not persisted, no sensitive data.
				continue
			}
		}
	}
}
