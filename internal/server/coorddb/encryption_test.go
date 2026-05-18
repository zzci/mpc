package coorddb

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Standalone encryption test suite (database.md §7.2, decoupled from
// E2E-001). Acceptance matrix (a)-(e):
//   (a) with encryption on, the .db on disk is ciphertext
//                                              -> store_test.go TestStore_LeakedFileIsCiphertext
//   (b) passphrase unlock -> UNLOCKED normal r/w
//                                              -> store_test.go TestStore_UnlockRelockLifecycle
//   (c) wrong passphrase rejected              -> store_test.go TestStore_WrongPassphraseRejected
//   (d) relock zeroizes back to LOCKED, unreadable
//                                              -> store_test.go TestStore_UnlockRelockLifecycle
//   (e) production guardrail: the disable switch fail-closes under a
//       simulated production marker
//                                              -> internal/node TestEncryptionDisableProductionGuardrail
// This file adds the §7.1 "dev/test encryption-disabled mode" tests and
// contrasts with (a): disabled mode writes the .db in plaintext (hence
// non-production only), enabled mode writes ciphertext.

// TestPlaintextStore_DisabledModeImmediatelyUnlocked verifies disabled
// mode: after OpenInsecure there is no key derivation, no passphrase,
// and it is immediately UNLOCKED-equivalent and read/writable
// (database.md §7.1).
func TestPlaintextStore_DisabledModeImmediatelyUnlocked(t *testing.T) {
	dir := t.TempDir()
	s := NewPlaintextStore(filepath.Join(dir, "coord.db"))
	t.Cleanup(func() { _ = s.Close() })

	if s.IsUnlocked() {
		t.Fatal("before OpenInsecure: should not be mounted yet")
	}
	if err := s.OpenInsecure(context.Background()); err != nil {
		t.Fatalf("OpenInsecure: %v", err)
	}
	if !s.IsUnlocked() {
		t.Fatal("after OpenInsecure: should be UNLOCKED-equivalent (no key, no LOCKED)")
	}
	// Normal r/w: persists and reads back even without a passphrase.
	seedGroup(t, s)
	if got, err := s.GroupEpoch(context.Background(), encMarker); err != nil || got != 0 {
		t.Fatalf("plaintext read/write: got=%d err=%v", got, err)
	}
}

// TestPlaintextStore_FileIsPlaintextAtRest contrasts with
// TestStore_LeakedFileIsCiphertext: disabled mode writes the .db in
// plaintext (standard SQLite header + recognizable payload) — precisely
// why disabling is non-production only and the production iron-law
// guardrail (node Validate) must fail-closed on misuse.
func TestPlaintextStore_FileIsPlaintextAtRest(t *testing.T) {
	dir := t.TempDir()
	s := NewPlaintextStore(filepath.Join(dir, "coord.db"))
	if err := s.OpenInsecure(context.Background()); err != nil {
		t.Fatalf("OpenInsecure: %v", err)
	}
	seedGroup(t, s)
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "coord.db"))
	if err != nil {
		t.Fatalf("read db file: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("SQLite format 3")) {
		t.Fatal("disabled mode must be standard plaintext SQLite (header missing)")
	}
	if !bytes.Contains(data, []byte(encMarker)) {
		t.Fatal("disabled mode: payload expected in plaintext on disk")
	}
	// Disabled mode derives no key -> writes no KDF sidecar.
	if _, err := os.Stat(filepath.Join(dir, "coord.db.kdf")); !os.IsNotExist(err) {
		t.Fatalf("disabled mode must not write KDF sidecar: stat err=%v", err)
	}
}

// TestPlaintextStore_UnlockRejected verifies passphrase unlock is not
// applicable in disabled mode (fail-closed misuse protection): Unlock
// returns ErrPlaintextMode and does not accidentally switch to the
// encrypted path.
func TestPlaintextStore_UnlockRejected(t *testing.T) {
	s := NewPlaintextStore(filepath.Join(t.TempDir(), "coord.db"))
	if err := s.Unlock(context.Background(), []byte(testPass)); !errors.Is(err, ErrPlaintextMode) {
		t.Fatalf("Unlock on plaintext store: got %v, want ErrPlaintextMode", err)
	}
}

// TestEncryptedStore_OpenInsecureRejected verifies an encrypted Store
// (NewStore) cannot be plaintext-mounted via OpenInsecure — the disabled
// path is reachable only via NewPlaintextStore, preventing an encrypted
// store from being mis-downgraded to plaintext.
func TestEncryptedStore_OpenInsecureRejected(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "coord.db"))
	if err := s.OpenInsecure(context.Background()); !errors.Is(err, ErrNotPlaintext) {
		t.Fatalf("OpenInsecure on encrypted store: got %v, want ErrNotPlaintext", err)
	}
	if s.IsUnlocked() {
		t.Fatal("encrypted store must stay LOCKED after rejected OpenInsecure")
	}
}
