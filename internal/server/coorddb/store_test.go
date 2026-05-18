package coorddb

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const (
	testPass   = "operator-passphrase-correct"
	encMarker  = "ENCMARKER-GROUP-deadbeefcafe"
	memberMark = "ENCMARKER-MEMBER-feedface"
)

func mustUnlocked(t *testing.T, dir string) *Store {
	t.Helper()
	s := NewStore(filepath.Join(dir, "coord.db"))
	if err := s.Unlock(context.Background(), []byte(testPass)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedGroup(t *testing.T, s *Store) {
	t.Helper()
	err := s.ProvisionGroup(context.Background(),
		GroupRecord{
			GroupID: encMarker, ECDSAPubkey: []byte{1, 2, 3}, GroupPubkey: []byte{4, 5, 6},
			ThresholdT: 2, PartiesN: 3, Epoch: 0, CreatedAt: "2026-05-18T00:00:00Z",
		},
		[]MemberRecord{{MemberID: memberMark, IdentityPubkey: []byte{7, 8, 9}}})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
}

func TestStore_StartsLockedFailClosed(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "coord.db"))
	if s.IsUnlocked() {
		t.Fatal("store should start LOCKED")
	}
	if _, err := s.RequestStatus(context.Background(), "x"); !errors.Is(err, ErrLocked) {
		t.Fatalf("read under LOCKED: got %v, want ErrLocked", err)
	}
	err := s.WithTx(context.Background(), func(*sql.Tx) error { return nil })
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("WithTx under LOCKED: got %v, want ErrLocked", err)
	}
}

func TestStore_EmptyPassphraseRejected(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "coord.db"))
	if err := s.Unlock(context.Background(), nil); !errors.Is(err, ErrEmptyPassphrase) {
		t.Fatalf("got %v, want ErrEmptyPassphrase", err)
	}
}

func TestStore_UnlockRelockLifecycle(t *testing.T) {
	dir := t.TempDir()
	s := mustUnlocked(t, dir)
	if !s.IsUnlocked() {
		t.Fatal("should be UNLOCKED")
	}
	seedGroup(t, s)

	if err := s.Relock(); err != nil {
		t.Fatalf("relock: %v", err)
	}
	if s.IsUnlocked() {
		t.Fatal("should be LOCKED after relock")
	}
	// fail-closed after relock
	if _, err := s.RequestStatus(context.Background(), "x"); !errors.Is(err, ErrLocked) {
		t.Fatalf("post-relock read: got %v, want ErrLocked", err)
	}
	// idempotent relock
	if err := s.Relock(); err != nil {
		t.Fatalf("idempotent relock: %v", err)
	}

	// correct passphrase re-unlock recovers persisted data
	if err := s.Unlock(context.Background(), []byte(testPass)); err != nil {
		t.Fatalf("re-unlock: %v", err)
	}
	if got, err := s.GroupEpoch(context.Background(), encMarker); err != nil || got != 0 {
		t.Fatalf("persisted group epoch: got=%d err=%v", got, err)
	}
}

func TestStore_WrongPassphraseRejected(t *testing.T) {
	dir := t.TempDir()
	s := mustUnlocked(t, dir)
	seedGroup(t, s)
	if err := s.Relock(); err != nil {
		t.Fatalf("relock: %v", err)
	}
	err := s.Unlock(context.Background(), []byte("WRONG-passphrase"))
	if err == nil {
		t.Fatal("wrong passphrase must fail (encrypted db unreadable)")
	}
	if s.IsUnlocked() {
		t.Fatal("must remain LOCKED after failed unlock")
	}
}

func TestStore_LeakedFileIsCiphertext(t *testing.T) {
	dir := t.TempDir()
	s := mustUnlocked(t, dir)
	seedGroup(t, s)
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "coord.db"))
	if err != nil {
		t.Fatalf("read db file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("db file empty")
	}
	if bytes.HasPrefix(data, []byte("SQLite format 3")) {
		t.Fatal("db header is plaintext SQLite (not encrypted)")
	}
	if bytes.Contains(data, []byte(encMarker)) || bytes.Contains(data, []byte(memberMark)) {
		t.Fatal("plaintext payload leaked into db file")
	}
}

func TestStore_TransitionSameTxAtomic(t *testing.T) {
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())
	seedGroup(t, s)
	if err := s.CreateSigningRequest(ctx, SigningRequestSeed{
		RequestID: "req-1", GroupID: encMarker, Chain: "eth",
		UnsignedTx: []byte("tx"), Digest32: bytes.Repeat([]byte{0xab}, 32),
		Proposer: "p", MetaHash: []byte("mh"), ProposerSig: []byte("ps"),
		CreatedAt: "2026-05-18T00:00:00Z", Expiry: "2026-05-18T01:00:00Z",
	}); err != nil {
		t.Fatalf("create request: %v", err)
	}

	// success: status changes AND one event written, same tx
	if err := s.RecordTransition(ctx, "req-1", "PENDING", "DISPATCHED", "coord", "quorum"); err != nil {
		t.Fatalf("transition: %v", err)
	}
	if st, _ := s.RequestStatus(ctx, "req-1"); st != "DISPATCHED" {
		t.Fatalf("status = %s, want DISPATCHED", st)
	}
	if n := eventCount(t, s, "req-1"); n != 1 {
		t.Fatalf("event count = %d, want 1", n)
	}

	// guard miss: wrong `from` -> ErrConflict, status unchanged, NO new event (rollback)
	err := s.RecordTransition(ctx, "req-1", "PENDING", "SIGNED", "coord", "bad")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
	}
	if st, _ := s.RequestStatus(ctx, "req-1"); st != "DISPATCHED" {
		t.Fatalf("status mutated on failed transition: %s", st)
	}
	if n := eventCount(t, s, "req-1"); n != 1 {
		t.Fatalf("event count = %d after failed transition, want 1 (atomic rollback)", n)
	}
}

func eventCount(t *testing.T, s *Store, reqID string) int {
	t.Helper()
	db, err := s.conn()
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	var n int
	if err := db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM request_events WHERE request_id = ?`, reqID).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}
