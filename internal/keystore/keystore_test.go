package keystore_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bnb-chain/tss-lib/v3/ecdsa/keygen"

	"github.com/zzci/mpc/internal/keystore"
	"github.com/zzci/mpc/internal/mpc"
)

// plaintextMarker is embedded in the shard so tests can assert it never
// appears in any persisted/exported byte stream.
const plaintextMarker = "SHARD-PLAINTEXT-CANARY-7f3a"

// goodPass is a policy-compliant passphrase. The K-001 round-trip cases use
// it on the seal path now that a weak-passphrase policy gates Store.Save and
// ExportShare; this only strengthens the inputs and leaves the round-trip
// semantics each case asserts unchanged.
const goodPass = "correct horse battery staple"

func sampleShare(t *testing.T) keystore.Share {
	t.Helper()
	return keystore.Share{
		Moniker:  "party-1/" + plaintextMarker, // also exercises path-unsafe monikers
		SaveData: []byte(`{"secret":"` + plaintextMarker + `","k":42}`),
	}
}

func newStore(t *testing.T) (*keystore.Store, *keystore.SoftSecureArea, string) {
	t.Helper()
	dir := t.TempDir()
	area, err := keystore.NewSoftSecureArea()
	if err != nil {
		t.Fatalf("NewSoftSecureArea: %v", err)
	}
	st, err := keystore.NewStore(dir, area)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return st, area, dir
}

func TestStoreSaveLoadRoundTrip(t *testing.T) {
	ctx := context.Background()
	st, _, _ := newStore(t)
	want := sampleShare(t)

	if err := st.Save(ctx, want, "correct horse battery staple"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := st.Load(ctx, want.Moniker, "correct horse battery staple")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Moniker != want.Moniker || !bytes.Equal(got.SaveData, want.SaveData) {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, want)
	}
}

func TestNeverPlaintextOnDisk(t *testing.T) {
	ctx := context.Background()
	st, _, dir := newStore(t)
	share := sampleShare(t)

	if err := st.Save(ctx, share, goodPass); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one file on disk, got %d", len(entries))
	}
	raw, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if bytes.Contains(raw, []byte(plaintextMarker)) {
		t.Fatal("plaintext shard marker found on disk: shard persisted unencrypted")
	}
	if bytes.Contains(raw, share.SaveData) {
		t.Fatal("raw SaveData found on disk: shard persisted unencrypted")
	}
	// Sanity: the file must be a valid envelope (ciphertext, not the share).
	var env map[string]any
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("on-disk blob is not a JSON envelope: %v", err)
	}
	if _, ok := env["ct"]; !ok {
		t.Fatal("envelope missing ciphertext field")
	}
}

func TestWrongPassphraseRejected(t *testing.T) {
	ctx := context.Background()
	st, _, _ := newStore(t)
	share := sampleShare(t)

	if err := st.Save(ctx, share, goodPass); err != nil {
		t.Fatalf("Save: %v", err)
	}
	_, err := st.Load(ctx, share.Moniker, "wrong-pass")
	if !errors.Is(err, keystore.ErrDecrypt) {
		t.Fatalf("expected ErrDecrypt for wrong passphrase, got %v", err)
	}
}

func TestWrongDeviceFactorRejected(t *testing.T) {
	ctx := context.Background()
	st, _, dir := newStore(t)
	share := sampleShare(t)
	if err := st.Save(ctx, share, goodPass); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// A different device key (e.g. secure element not restored) must not open
	// the blob even with the correct passphrase.
	otherArea, err := keystore.NewSoftSecureArea()
	if err != nil {
		t.Fatalf("NewSoftSecureArea: %v", err)
	}
	st2, err := keystore.NewStore(dir, otherArea)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	_, err = st2.Load(ctx, share.Moniker, goodPass)
	if !errors.Is(err, keystore.ErrDecrypt) {
		t.Fatalf("expected ErrDecrypt for wrong device factor, got %v", err)
	}
}

func TestRestoredDeviceKeyOpens(t *testing.T) {
	ctx := context.Background()
	st, area, dir := newStore(t)
	share := sampleShare(t)
	if err := st.Save(ctx, share, goodPass); err != nil {
		t.Fatalf("Save: %v", err)
	}
	devKey, err := area.DeviceKey(ctx)
	if err != nil {
		t.Fatalf("DeviceKey: %v", err)
	}
	restored, err := keystore.NewSoftSecureAreaWithKey(devKey)
	if err != nil {
		t.Fatalf("NewSoftSecureAreaWithKey: %v", err)
	}
	st2, err := keystore.NewStore(dir, restored)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	got, err := st2.Load(ctx, share.Moniker, goodPass)
	if err != nil {
		t.Fatalf("Load after device restore: %v", err)
	}
	if !bytes.Equal(got.SaveData, share.SaveData) {
		t.Fatal("round trip mismatch after device key restore")
	}
}

func TestCorruptionRejected(t *testing.T) {
	ctx := context.Background()
	st, _, dir := newStore(t)
	share := sampleShare(t)
	if err := st.Save(ctx, share, goodPass); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	p := filepath.Join(dir, entries[0].Name())
	raw, _ := os.ReadFile(p)

	var env map[string]any
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	ctB64, ok := env["ct"].(string)
	if !ok {
		t.Fatalf("ct field is not a base64 string: %T", env["ct"])
	}
	ct, err := base64.StdEncoding.DecodeString(ctB64)
	if err != nil {
		t.Fatalf("decode ct: %v", err)
	}
	ct[len(ct)/2] ^= 0xFF // flip one ciphertext byte; GCM auth must fail
	env["ct"] = base64.StdEncoding.EncodeToString(ct)
	tampered, _ := json.Marshal(env)
	if err := os.WriteFile(p, tampered, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err = st.Load(ctx, share.Moniker, goodPass)
	if !errors.Is(err, keystore.ErrDecrypt) {
		t.Fatalf("expected ErrDecrypt for corrupted ciphertext, got %v", err)
	}
}

func TestVersionMismatchRejected(t *testing.T) {
	ctx := context.Background()
	st, _, dir := newStore(t)
	share := sampleShare(t)
	if err := st.Save(ctx, share, goodPass); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	p := filepath.Join(dir, entries[0].Name())
	raw, _ := os.ReadFile(p)

	var env map[string]any
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	env["v"] = 9999
	bumped, _ := json.Marshal(env)
	if err := os.WriteFile(p, bumped, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := st.Load(ctx, share.Moniker, goodPass)
	if !errors.Is(err, keystore.ErrVersionMismatch) {
		t.Fatalf("expected ErrVersionMismatch, got %v", err)
	}
}

func TestMalformedEnvelopeRejected(t *testing.T) {
	_, err := keystore.ImportShare([]byte("not json at all"), goodPass)
	if !errors.Is(err, keystore.ErrFormat) {
		t.Fatalf("expected ErrFormat, got %v", err)
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	share := sampleShare(t)
	blob, err := keystore.ExportShare(share, goodPass)
	if err != nil {
		t.Fatalf("ExportShare: %v", err)
	}
	if bytes.Contains(blob, []byte(plaintextMarker)) || bytes.Contains(blob, share.SaveData) {
		t.Fatal("plaintext shard found in backup blob")
	}
	got, err := keystore.ImportShare(blob, goodPass)
	if err != nil {
		t.Fatalf("ImportShare: %v", err)
	}
	if got.Moniker != share.Moniker || !bytes.Equal(got.SaveData, share.SaveData) {
		t.Fatal("export/import round trip mismatch")
	}

	if _, err := keystore.ImportShare(blob, "wrong-pass"); !errors.Is(err, keystore.ErrDecrypt) {
		t.Fatalf("expected ErrDecrypt importing with wrong passphrase, got %v", err)
	}
}

// TestMPCSaveDataInterop proves the keystore round-trips the exact byte string
// produced/consumed by internal/mpc, without running a full (slow) keygen.
func TestMPCSaveDataInterop(t *testing.T) {
	ctx := context.Background()
	sd := keygen.NewLocalPartySaveData(3)
	raw, err := mpc.MarshalSaveData(&sd)
	if err != nil {
		t.Fatalf("mpc.MarshalSaveData: %v", err)
	}
	share := keystore.Share{Moniker: "interop-party", SaveData: raw}

	st, _, _ := newStore(t)
	if err := st.Save(ctx, share, goodPass); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := st.Load(ctx, share.Moniker, goodPass)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(got.SaveData, raw) {
		t.Fatal("save data bytes mutated by keystore round trip")
	}
	if _, err := mpc.UnmarshalSaveData(got.SaveData); err != nil {
		t.Fatalf("mpc.UnmarshalSaveData on restored bytes: %v", err)
	}

	// Same guarantee through the backup path.
	blob, err := keystore.ExportShare(share, goodPass)
	if err != nil {
		t.Fatalf("ExportShare: %v", err)
	}
	imported, err := keystore.ImportShare(blob, goodPass)
	if err != nil {
		t.Fatalf("ImportShare: %v", err)
	}
	if _, err := mpc.UnmarshalSaveData(imported.SaveData); err != nil {
		t.Fatalf("mpc.UnmarshalSaveData after backup round trip: %v", err)
	}
}
