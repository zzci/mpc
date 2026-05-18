package coorddb

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrInitKDF_CreatesAndReloadsStableSalt(t *testing.T) {
	db := filepath.Join(t.TempDir(), "coord.db")

	p1, err := loadOrInitKDF(db)
	if err != nil {
		t.Fatalf("init kdf: %v", err)
	}
	if p1.Salt == "" || p1.MemoryKiB == 0 || p1.KeyLen != 32 {
		t.Fatalf("unexpected params: %+v", p1)
	}
	if _, err := os.Stat(kdfSidecarPath(db)); err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}

	p2, err := loadOrInitKDF(db)
	if err != nil {
		t.Fatalf("reload kdf: %v", err)
	}
	if p2.Salt != p1.Salt {
		t.Fatalf("salt not stable across reload: %q != %q", p2.Salt, p1.Salt)
	}
}

func TestDeriveKey_DeterministicAndPassphraseSensitive(t *testing.T) {
	p, err := loadOrInitKDF(filepath.Join(t.TempDir(), "coord.db"))
	if err != nil {
		t.Fatalf("init kdf: %v", err)
	}
	k1, err := deriveKey([]byte("correct horse"), p)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	k2, _ := deriveKey([]byte("correct horse"), p)
	k3, _ := deriveKey([]byte("battery staple"), p)

	if len(k1) != 32 {
		t.Fatalf("key len = %d, want 32", len(k1))
	}
	if !bytes.Equal(k1, k2) {
		t.Fatal("same passphrase produced different keys")
	}
	if bytes.Equal(k1, k3) {
		t.Fatal("different passphrase produced same key")
	}
}

func TestZeroize(t *testing.T) {
	b := []byte{1, 2, 3, 4}
	zeroize(b)
	for i, v := range b {
		if v != 0 {
			t.Fatalf("byte %d not zeroed: %d", i, v)
		}
	}
}
