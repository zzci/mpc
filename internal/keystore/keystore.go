package keystore

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Share is the unit the keystore seals: a named, opaque keygen save-data blob.
// It mirrors mpc.Share (moniker + serialized tss-lib save data) without
// importing the mpc package, keeping keystore a leaf module. SaveData is the
// exact byte string produced by mpc.MarshalSaveData and consumed by
// mpc.UnmarshalSaveData.
type Share struct {
	Moniker  string `json:"moniker"`
	SaveData []byte `json:"saveData"`
}

// Store persists sealed shares under a directory. Each share is encrypted with
// the user passphrase plus the SecureArea device factor; the plaintext shard
// never reaches disk.
type Store struct {
	dir  string
	area SecureArea
}

// NewStore opens (creating if absent) a store rooted at dir. area supplies the
// device factor; pass PassphraseOnly{} for passphrase-only at-rest sealing.
func NewStore(dir string, area SecureArea) (*Store, error) {
	if area == nil {
		return nil, fmt.Errorf("keystore: secure area must not be nil")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("keystore: create dir: %w", err)
	}
	return &Store{dir: dir, area: area}, nil
}

// path maps a moniker to a file name via hex encoding, which is collision-free
// and immune to path traversal regardless of moniker content.
func (s *Store) path(moniker string) string {
	return filepath.Join(s.dir, hex.EncodeToString([]byte(moniker))+".ks")
}

// Save seals the share and writes it atomically (temp file + rename) with
// owner-only permissions.
func (s *Store) Save(ctx context.Context, share Share, passphrase string) error {
	if share.Moniker == "" {
		return fmt.Errorf("keystore: share moniker must not be empty")
	}
	deviceKey, err := s.area.DeviceKey(ctx)
	if err != nil {
		return fmt.Errorf("keystore: device key: %w", err)
	}
	defer wipe(deviceKey)

	plain, err := json.Marshal(share)
	if err != nil {
		return fmt.Errorf("keystore: marshal share: %w", err)
	}
	defer wipe(plain)

	blob, err := seal(plain, passphrase, s.area.ID(), deviceKey)
	if err != nil {
		return err
	}

	dst := s.path(share.Moniker)
	tmp, err := os.CreateTemp(s.dir, ".ks-*")
	if err != nil {
		return fmt.Errorf("keystore: temp file: %w", err)
	}
	tmpName := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("keystore: chmod: %w", err)
	}
	if _, err := tmp.Write(blob); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("keystore: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("keystore: close: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("keystore: rename: %w", err)
	}
	return nil
}

// Load reads and decrypts the share for moniker. A wrong passphrase, a wrong
// device factor, or on-disk corruption surface as ErrDecrypt; an unknown
// format version as ErrVersionMismatch.
func (s *Store) Load(ctx context.Context, moniker, passphrase string) (Share, error) {
	blob, err := os.ReadFile(s.path(moniker))
	if err != nil {
		return Share{}, fmt.Errorf("keystore: read: %w", err)
	}
	deviceKey, err := s.area.DeviceKey(ctx)
	if err != nil {
		return Share{}, fmt.Errorf("keystore: device key: %w", err)
	}
	defer wipe(deviceKey)

	plain, err := open(blob, passphrase, deviceKey)
	if err != nil {
		return Share{}, err
	}
	defer wipe(plain)

	var share Share
	if err := json.Unmarshal(plain, &share); err != nil {
		return Share{}, ErrFormat
	}
	return share, nil
}
