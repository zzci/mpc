package keystore_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/royqta/mcp-wallet/internal/keystore"
)

// craftEnvelope seals a valid backup, then rewrites its kdf parameters so a
// test can feed ImportShare a structurally-valid-but-hostile envelope without
// hand-building ciphertext.
func craftEnvelope(t *testing.T, mutate func(kdf map[string]any)) []byte {
	t.Helper()
	blob, err := keystore.ExportShare(keystore.Share{Moniker: "m", SaveData: []byte("x")}, goodPass)
	if err != nil {
		t.Fatalf("ExportShare: %v", err)
	}
	var env map[string]any
	if err := json.Unmarshal(blob, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	kdf, ok := env["kdf"].(map[string]any)
	if !ok {
		t.Fatalf("kdf field missing or wrong type: %T", env["kdf"])
	}
	mutate(kdf)
	env["kdf"] = kdf
	out, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal tampered envelope: %v", err)
	}
	return out
}

func TestWeakPassphraseRejectedOnSeal(t *testing.T) {
	ctx := context.Background()
	st, _, _ := newStore(t)
	share := sampleShare(t)

	weak := []string{
		"pw",           // too short
		"password1234", // common base
		"aaaaaaaaaaaa", // too few distinct runes
		"abcdefghijkl", // single class, < 20 chars
	}
	for _, w := range weak {
		if err := st.Save(ctx, share, w); !errors.Is(err, keystore.ErrWeakPassphrase) {
			t.Fatalf("Save(%q): want ErrWeakPassphrase, got %v", w, err)
		}
		if _, err := keystore.ExportShare(share, w); !errors.Is(err, keystore.ErrWeakPassphrase) {
			t.Fatalf("ExportShare(%q): want ErrWeakPassphrase, got %v", w, err)
		}
	}
	// A compliant passphrase still seals (round-trip unbroken).
	if err := st.Save(ctx, share, goodPass); err != nil {
		t.Fatalf("Save(goodPass): %v", err)
	}
	if _, err := st.Load(ctx, share.Moniker, goodPass); err != nil {
		t.Fatalf("Load(goodPass): %v", err)
	}
}

// TestImportShareRejectsHugeArgonMemory closes the K-001 residual MEDIUM: a
// crafted backup must not be able to force a multi-hundred-MiB / GiB Argon2
// allocation. The out-of-range parameter is rejected as ErrFormat before any
// Argon2 work, so there is no memory-amplification DoS.
func TestImportShareRejectsHugeArgonMemory(t *testing.T) {
	blob := craftEnvelope(t, func(kdf map[string]any) {
		kdf["m"] = 1 << 20 // 1 GiB in KiB — the former ceiling
	})
	if _, err := keystore.ImportShare(blob, goodPass); !errors.Is(err, keystore.ErrFormat) {
		t.Fatalf("ImportShare with 1 GiB Argon2 memory: want ErrFormat, got %v", err)
	}
}

func TestImportShareRejectsAbnormalKDFParams(t *testing.T) {
	cases := map[string]func(map[string]any){
		"memory below floor": func(k map[string]any) { k["m"] = 1024 }, // 1 MiB < 8 MiB
		"time zero":          func(k map[string]any) { k["t"] = 0 },
		"time absurd":        func(k map[string]any) { k["t"] = 1 << 20 },
		"threads zero":       func(k map[string]any) { k["p"] = 0 },
		"threads absurd":     func(k map[string]any) { k["p"] = 255 },
		"alg unknown":        func(k map[string]any) { k["alg"] = "scrypt" },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			blob := craftEnvelope(t, mut)
			if _, err := keystore.ImportShare(blob, goodPass); !errors.Is(err, keystore.ErrFormat) {
				t.Fatalf("%s: want ErrFormat, got %v", name, err)
			}
		})
	}
}

// fakeProvider is a DeviceKeyProvider stand-in for the production path: it
// either yields a fixed hardware key or simulates an unavailable secure area.
type fakeProvider struct {
	key []byte
	err error
}

func (f fakeProvider) ProvideDeviceKey(_ context.Context) ([]byte, error) {
	return f.key, f.err
}

func TestDeviceSecureAreaFailsClosed(t *testing.T) {
	ctx := context.Background()

	if _, err := keystore.NewDeviceSecureArea(nil); err == nil {
		t.Fatal("NewDeviceSecureArea(nil): want error, got nil (would be a software fallback)")
	}

	// Provider reports the secure hardware is unavailable: no fallback.
	unavail, err := keystore.NewDeviceSecureArea(fakeProvider{err: errors.New("secure enclave locked")})
	if err != nil {
		t.Fatalf("NewDeviceSecureArea: %v", err)
	}
	if _, err := unavail.DeviceKey(ctx); err == nil {
		t.Fatal("DeviceKey with failing provider: want error, got nil")
	}

	// Provider returns an empty key with no error: still a failure.
	empty, err := keystore.NewDeviceSecureArea(fakeProvider{key: []byte{}})
	if err != nil {
		t.Fatalf("NewDeviceSecureArea: %v", err)
	}
	if _, err := empty.DeviceKey(ctx); err == nil {
		t.Fatal("DeviceKey with empty key: want error, got nil")
	}
}

// TestDeviceSecureAreaTwoFactor proves the hardware-backed area is a real
// second factor: a blob sealed with the device key cannot be opened once the
// device factor changes, even with the correct passphrase.
func TestDeviceSecureAreaTwoFactor(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	devKey := make([]byte, 32)
	for i := range devKey {
		devKey[i] = byte(i + 1)
	}
	area, err := keystore.NewDeviceSecureArea(fakeProvider{key: devKey})
	if err != nil {
		t.Fatalf("NewDeviceSecureArea: %v", err)
	}
	st, err := keystore.NewStore(dir, area)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	share := sampleShare(t)
	if err := st.Save(ctx, share, goodPass); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := st.Load(ctx, share.Moniker, goodPass)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Moniker != share.Moniker {
		t.Fatal("round trip mismatch under device factor")
	}

	// Different device key + correct passphrase must not open it.
	other, err := keystore.NewDeviceSecureArea(fakeProvider{key: []byte("a-completely-different-device-key")})
	if err != nil {
		t.Fatalf("NewDeviceSecureArea: %v", err)
	}
	st2, err := keystore.NewStore(dir, other)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if _, err := st2.Load(ctx, share.Moniker, goodPass); !errors.Is(err, keystore.ErrDecrypt) {
		t.Fatalf("want ErrDecrypt with wrong device factor, got %v", err)
	}
}
