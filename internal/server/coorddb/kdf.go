package coorddb

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"golang.org/x/crypto/argon2"
)

// kdfParams holds the Argon2id parameters that derive the operator
// passphrase into the whole-DB encryption key (database.md §7).
//
// The salt is non-secret: an Argon2id salt only needs to be unique
// (anti-rainbow-table), not confidential, so it is persisted next to the
// DB in a sidecar <db>.kdf (plaintext JSON) so the same passphrase
// re-derives the same key across restarts. The passphrase itself is
// NEVER written to this file nor read from config/env — losing it makes
// the DB unrecoverable (by design).
type kdfParams struct {
	Version   int    `json:"version"`
	Salt      string `json:"salt"` // base64(raw 16B), non-secret
	TimeCost  uint32 `json:"time"`
	MemoryKiB uint32 `json:"memory_kib"`
	Threads   uint8  `json:"threads"`
	KeyLen    uint32 `json:"key_len"`
}

// defaultKDFParams picks high-memory Argon2id params (64 MiB / t=3 /
// p=4 / 32B key).
func defaultKDFParams() kdfParams {
	return kdfParams{
		Version:   1,
		TimeCost:  3,
		MemoryKiB: 64 * 1024,
		Threads:   4,
		KeyLen:    32,
	}
}

func kdfSidecarPath(dbPath string) string { return dbPath + ".kdf" }

// loadOrInitKDF reads the DB-side KDF sidecar; if absent it initializes
// it with default params + a fresh random salt and writes it (0600).
// The salt is non-secret so persisting it is safe; the passphrase is
// not part of this file.
func loadOrInitKDF(dbPath string) (kdfParams, error) {
	path := kdfSidecarPath(dbPath)
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		var p kdfParams
		if err := json.Unmarshal(data, &p); err != nil {
			return kdfParams{}, fmt.Errorf("coorddb: parse kdf sidecar %s: %w", path, err)
		}
		if _, err := base64.StdEncoding.DecodeString(p.Salt); err != nil {
			return kdfParams{}, fmt.Errorf("coorddb: invalid kdf salt: %w", err)
		}
		return p, nil
	case os.IsNotExist(err):
		p := defaultKDFParams()
		salt := make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			return kdfParams{}, fmt.Errorf("coorddb: gen kdf salt: %w", err)
		}
		p.Salt = base64.StdEncoding.EncodeToString(salt)
		buf, err := json.Marshal(p)
		if err != nil {
			return kdfParams{}, fmt.Errorf("coorddb: marshal kdf sidecar: %w", err)
		}
		if err := os.WriteFile(path, buf, 0o600); err != nil {
			return kdfParams{}, fmt.Errorf("coorddb: write kdf sidecar %s: %w", path, err)
		}
		return p, nil
	default:
		return kdfParams{}, fmt.Errorf("coorddb: read kdf sidecar %s: %w", path, err)
	}
}

// deriveKey derives a raw encryption key from the passphrase via
// Argon2id; the returned key is for in-memory use only and the caller
// must zeroize it when done.
func deriveKey(passphrase []byte, p kdfParams) ([]byte, error) {
	salt, err := base64.StdEncoding.DecodeString(p.Salt)
	if err != nil {
		return nil, fmt.Errorf("coorddb: decode kdf salt: %w", err)
	}
	return argon2.IDKey(passphrase, salt, p.TimeCost, p.MemoryKiB, p.Threads, p.KeyLen), nil
}

// zeroize wipes a sensitive byte slice (passphrase copy / derived key)
// to shorten the in-memory residency window.
func zeroize(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
