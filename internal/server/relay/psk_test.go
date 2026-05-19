package relay

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePSK(t *testing.T) {
	key := make([]byte, pskLen)
	for i := range key {
		key[i] = byte(i)
	}

	t.Run("env hex", func(t *testing.T) {
		t.Setenv("RELAY_PSK_HEX", hex.EncodeToString(key))
		got, err := resolvePSK("env:RELAY_PSK_HEX")
		if err != nil || len(got) != pskLen {
			t.Fatalf("hex env: got %d bytes, err %v", len(got), err)
		}
	})

	t.Run("file base64", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "psk")
		if err := os.WriteFile(f, []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := resolvePSK("file:" + f)
		if err != nil || len(got) != pskLen {
			t.Fatalf("base64 file: got %d bytes, err %v", len(got), err)
		}
	})

	t.Run("missing env", func(t *testing.T) {
		if _, err := resolvePSK("env:RELAY_PSK_ABSENT"); !errors.Is(err, errSecretMissing) {
			t.Fatalf("missing must be errSecretMissing, got %v", err)
		}
	})

	t.Run("literal accepted", func(t *testing.T) {
		// Config framework v2 (user ruling 2026-05-19): a literal value is
		// allowed alongside env:/file: references.
		got, err := resolvePSK(hex.EncodeToString(key))
		if err != nil || len(got) != pskLen {
			t.Fatalf("literal psk: got %d bytes, err %v", len(got), err)
		}
	})

	t.Run("bad length", func(t *testing.T) {
		t.Setenv("RELAY_PSK_SHORT", hex.EncodeToString(key[:16]))
		if _, err := resolvePSK("env:RELAY_PSK_SHORT"); err == nil {
			t.Fatal("non-32-byte key must be rejected")
		}
	})
}
