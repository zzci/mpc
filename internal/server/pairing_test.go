package server

import (
	"errors"
	"testing"
	"time"
)

func mkClock(start time.Time) (func() time.Time, *time.Time) {
	t := start
	return func() time.Time { return t }, &t
}

func TestPairingCreateAndGet(t *testing.T) {
	now, _ := mkClock(time.Unix(1_700_000_000, 0))
	s := NewPairingStore(now)
	ticket, err := s.Create("g1", "Alice", 10*time.Minute)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(ticket.Token) != 64 {
		t.Fatalf("token len = %d, want 64", len(ticket.Token))
	}
	if ticket.GroupID != "g1" || ticket.Label != "Alice" {
		t.Fatalf("ticket fields not stored: %+v", ticket)
	}
	if ticket.UsedAt != nil {
		t.Fatalf("new ticket should not be used")
	}
	got, ok := s.Get(ticket.Token)
	if !ok || got.Token != ticket.Token {
		t.Fatalf("Get(%q) failed: ok=%v got=%+v", ticket.Token, ok, got)
	}
}

func TestPairingCreateRejectsBadTTL(t *testing.T) {
	s := NewPairingStore(nil)
	if _, err := s.Create("g", "l", 0); err == nil {
		t.Fatalf("ttl=0 should be rejected")
	}
	if _, err := s.Create("g", "l", -time.Second); err == nil {
		t.Fatalf("negative ttl should be rejected")
	}
}

func TestPairingConsumeHappyPath(t *testing.T) {
	clock, tp := mkClock(time.Unix(1_700_000_000, 0))
	s := NewPairingStore(clock)
	ticket, _ := s.Create("g1", "l", time.Minute)
	*tp = tp.Add(5 * time.Second)
	consumed, err := s.Consume(ticket.Token, "04abc")
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if consumed.UsedAt == nil || consumed.UsedBy != "04abc" {
		t.Fatalf("consumed ticket lost UsedAt/UsedBy: %+v", consumed)
	}
}

func TestPairingConsumeUnknown(t *testing.T) {
	s := NewPairingStore(nil)
	if _, err := s.Consume("deadbeef", "id"); !errors.Is(err, ErrPairingUnknown) {
		t.Fatalf("unknown token err = %v, want ErrPairingUnknown", err)
	}
}

func TestPairingConsumeReplayRefused(t *testing.T) {
	s := NewPairingStore(nil)
	ticket, _ := s.Create("g1", "l", time.Minute)
	if _, err := s.Consume(ticket.Token, "id1"); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	_, err := s.Consume(ticket.Token, "id2")
	if !errors.Is(err, ErrPairingUsed) {
		t.Fatalf("replay err = %v, want ErrPairingUsed", err)
	}
}

func TestPairingConsumeExpired(t *testing.T) {
	clock, tp := mkClock(time.Unix(1_700_000_000, 0))
	s := NewPairingStore(clock)
	ticket, _ := s.Create("g1", "l", time.Minute)
	*tp = tp.Add(2 * time.Minute)
	_, err := s.Consume(ticket.Token, "id")
	if !errors.Is(err, ErrPairingExpired) {
		t.Fatalf("expired err = %v, want ErrPairingExpired", err)
	}
}

func TestPairingListAndDelete(t *testing.T) {
	clock, tp := mkClock(time.Unix(1_700_000_000, 0))
	s := NewPairingStore(clock)
	t1, _ := s.Create("g1", "first", time.Minute)
	*tp = tp.Add(time.Second)
	t2, _ := s.Create("g2", "second", time.Minute)
	got := s.List()
	if len(got) != 2 || got[0].Token != t2.Token { // newest-first
		t.Fatalf("list order/len wrong: %+v", got)
	}
	if !s.Delete(t1.Token) {
		t.Fatalf("delete existing returned false")
	}
	if s.Delete(t1.Token) {
		t.Fatalf("delete already-removed returned true")
	}
	if len(s.List()) != 1 {
		t.Fatalf("post-delete len = %d", len(s.List()))
	}
}

func TestPairingGCExpiredOnly(t *testing.T) {
	clock, tp := mkClock(time.Unix(1_700_000_000, 0))
	s := NewPairingStore(clock)
	expired, _ := s.Create("g1", "expired", time.Minute)
	used, _ := s.Create("g2", "used", time.Hour)
	_, _ = s.Consume(used.Token, "id")
	*tp = tp.Add(2 * time.Minute)
	pending, _ := s.Create("g3", "still pending", time.Hour)

	n := s.GC()
	if n != 1 {
		t.Fatalf("GC removed %d, want 1 (only the expired-unused)", n)
	}
	// Used + still-pending must survive.
	if _, ok := s.Get(expired.Token); ok {
		t.Fatalf("expired ticket should have been GC'd")
	}
	if _, ok := s.Get(used.Token); !ok {
		t.Fatalf("used ticket should survive GC (audit)")
	}
	if _, ok := s.Get(pending.Token); !ok {
		t.Fatalf("still-pending ticket should survive GC")
	}
}
