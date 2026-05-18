package coorddb

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// 独立加密专测(database.md §7.2,与 E2E-001 解耦)。验收矩阵 (a)-(e):
//   (a) 启用加密时 .db 落盘实为密文            -> store_test.go TestStore_LeakedFileIsCiphertext
//   (b) 口令解锁 -> UNLOCKED 正常读写          -> store_test.go TestStore_UnlockRelockLifecycle
//   (c) 错误口令拒绝                           -> store_test.go TestStore_WrongPassphraseRejected
//   (d) relock 清零回 LOCKED 不可读            -> store_test.go TestStore_UnlockRelockLifecycle
//   (e) 生产护栏:禁用开关在模拟生产标记下 fail-closed 拒启动
//                                              -> internal/node TestEncryptionDisableProductionGuardrail
// 本文件补 §7.1 新增的「dev/test 整库加密禁用模式」专测,并与 (a) 形成对照:
// 禁用模式 .db 明文落盘(故仅限非生产),启用模式落盘密文。

// TestPlaintextStore_DisabledModeImmediatelyUnlocked 验证禁用模式:OpenInsecure
// 后不派生密钥、不经口令、立即 UNLOCKED-等价并可正常读写(database.md §7.1)。
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
	// 正常读写:无口令也能持久化与回读。
	seedGroup(t, s)
	if got, err := s.GroupEpoch(context.Background(), encMarker); err != nil || got != 0 {
		t.Fatalf("plaintext read/write: got=%d err=%v", got, err)
	}
}

// TestPlaintextStore_FileIsPlaintextAtRest 与 TestStore_LeakedFileIsCiphertext
// 形成对照:禁用模式 .db 明文落盘(标准 SQLite 头 + 可识别载荷)——正因如此,
// 禁用仅限非生产,生产铁律护栏(node Validate)必须 fail-closed 拦截误用。
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
	// 禁用模式不派生密钥 -> 不写 KDF 旁文件。
	if _, err := os.Stat(filepath.Join(dir, "coord.db.kdf")); !os.IsNotExist(err) {
		t.Fatalf("disabled mode must not write KDF sidecar: stat err=%v", err)
	}
}

// TestPlaintextStore_UnlockRejected 验证禁用模式下口令解锁不适用(fail-closed
// 误用防护):Unlock 返回 ErrPlaintextMode,不会意外切到加密路径。
func TestPlaintextStore_UnlockRejected(t *testing.T) {
	s := NewPlaintextStore(filepath.Join(t.TempDir(), "coord.db"))
	if err := s.Unlock(context.Background(), []byte(testPass)); !errors.Is(err, ErrPlaintextMode) {
		t.Fatalf("Unlock on plaintext store: got %v, want ErrPlaintextMode", err)
	}
}

// TestEncryptedStore_OpenInsecureRejected 验证加密 Store(NewStore)不能经
// OpenInsecure 明文挂载——禁用路径仅 NewPlaintextStore 可达,杜绝加密 Store
// 被误降级为明文。
func TestEncryptedStore_OpenInsecureRejected(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "coord.db"))
	if err := s.OpenInsecure(context.Background()); !errors.Is(err, ErrNotPlaintext) {
		t.Fatalf("OpenInsecure on encrypted store: got %v, want ErrNotPlaintext", err)
	}
	if s.IsUnlocked() {
		t.Fatal("encrypted store must stay LOCKED after rejected OpenInsecure")
	}
}
