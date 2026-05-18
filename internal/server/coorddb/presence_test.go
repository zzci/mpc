package coorddb

import (
	"context"
	"testing"
	"time"
)

func TestPresence_HeartbeatAndOnline(t *testing.T) {
	ctx := context.Background()
	p, err := NewPresence(time.Hour, time.Hour)
	if err != nil {
		t.Fatalf("new presence: %v", err)
	}
	defer func() { _ = p.Close() }()

	if err := p.Heartbeat(ctx, "g1", "m1", "peerA"); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if err := p.Heartbeat(ctx, "g1", "m1", "peerA2"); err != nil { // upsert
		t.Fatalf("heartbeat upsert: %v", err)
	}
	if err := p.Heartbeat(ctx, "g2", "m9", "peerZ"); err != nil {
		t.Fatalf("heartbeat g2: %v", err)
	}

	on, err := p.Online(ctx, "g1")
	if err != nil {
		t.Fatalf("online: %v", err)
	}
	if len(on) != 1 || on[0].MemberID != "m1" || on[0].RelayPeerID != "peerA2" {
		t.Fatalf("unexpected online set: %+v", on)
	}
}

func TestPresence_TTLExpiryAndCleanup(t *testing.T) {
	ctx := context.Background()
	p, err := NewPresence(50*time.Millisecond, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("new presence: %v", err)
	}
	defer func() { _ = p.Close() }()

	if err := p.Heartbeat(ctx, "g1", "m1", "peer"); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	on, _ := p.Online(ctx, "g1")
	if len(on) != 1 {
		t.Fatalf("want 1 online immediately, got %d", len(on))
	}

	time.Sleep(80 * time.Millisecond) // past TTL
	// even if cleanup has not run, Online does not return expired entries (expires_at filter)
	if on, _ := p.Online(ctx, "g1"); len(on) != 0 {
		t.Fatalf("expired member still online: %+v", on)
	}

	// periodic cleanup should physically delete expired rows
	deadline := time.Now().Add(2 * time.Second)
	for {
		var n int
		if err := p.db.QueryRowContext(ctx, `SELECT count(*) FROM presence`).Scan(&n); err != nil {
			t.Fatalf("count presence: %v", err)
		}
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cleanup did not purge expired rows (still %d)", n)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
