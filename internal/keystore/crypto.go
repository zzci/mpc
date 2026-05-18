package keystore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/hkdf"
)

// formatVersion is the on-disk / backup envelope version. Bump only on a
// breaking layout or crypto-suite change; old blobs then fail closed with
// ErrVersionMismatch instead of silently misdecoding.
const formatVersion = 1

// Argon2id defaults follow the OWASP minimum profile (m=19 MiB, t=2, p=1),
// strong enough for a wallet passphrase yet fast enough for unit tests. The
// chosen values are persisted per blob so a future tightening does not strand
// existing data.
const (
	argonTimeDefault    uint32 = 2
	argonMemoryDefault  uint32 = 19 * 1024 // KiB (19 MiB, OWASP minimum)
	argonThreadsDefault uint8  = 1
	argonKeyLen         uint32 = 32 // AES-256
	saltLen                    = 16

	// Argon2 parameter bounds, enforced on every envelope including an
	// untrusted backup handed to ImportShare. The memory ceiling caps the
	// transient allocation a crafted blob can force: it closes the K-001
	// residual MEDIUM where the former 1 GiB ceiling let a malicious blob
	// OOM a memory-constrained device (memory-amplification DoS). The floor
	// plus the time/threads/salt caps reject downgraded or garbage
	// parameters before any Argon2 work runs. The window stays wide enough
	// that a future tightening of the seal defaults never strands data
	// sealed under the current ones.
	minArgonMemoryKiB uint32 = 8 * 1024  // 8 MiB
	maxArgonMemoryKiB uint32 = 64 * 1024 // 64 MiB (was 1 GiB)
	maxArgonTime      uint32 = 16
	maxArgonThreads   uint8  = 16
	maxArgonSaltLen          = 1024
)

// hkdfInfo domain-separates the final AEAD key derivation.
var hkdfInfo = []byte("mcp-wallet/keystore/v1")

// Sentinel errors. They never embed passphrase, key, or plaintext material so
// that a caller surfacing them to a UI cannot leak secrets.
var (
	// ErrFormat means the envelope bytes are not well-formed.
	ErrFormat = errors.New("keystore: malformed envelope")
	// ErrVersionMismatch means the envelope version is not supported.
	ErrVersionMismatch = errors.New("keystore: unsupported envelope version")
	// ErrDecrypt means authentication failed: wrong passphrase, wrong device
	// factor, or corrupted ciphertext. The three are cryptographically
	// indistinguishable under AES-GCM and are deliberately not split, to avoid
	// acting as an oracle.
	ErrDecrypt = errors.New("keystore: decryption failed (wrong passphrase or corrupted data)")
)

// kdfParams records how the passphrase key was stretched, so an envelope is
// self-describing and survives parameter changes.
type kdfParams struct {
	Alg     string `json:"alg"`
	Salt    []byte `json:"salt"`
	Time    uint32 `json:"t"`
	Memory  uint32 `json:"m"`
	Threads uint8  `json:"p"`
}

// envelope is the sealed unit written to disk or handed out as a backup.
// []byte fields marshal to base64 in JSON, so no plaintext shard material can
// appear in the persisted form.
type envelope struct {
	Version  int       `json:"v"`
	DeviceID string    `json:"device"`
	KDF      kdfParams `json:"kdf"`
	Nonce    []byte    `json:"nonce"`
	Cipher   []byte    `json:"ct"`
}

// deriveKey stretches the passphrase with Argon2id, then folds in the optional
// device-bound secret via HKDF. With no device factor (deviceKey == nil) the
// result reduces to the passphrase-only key, which is the backup path.
func deriveKey(passphrase string, deviceKey []byte, p kdfParams) ([]byte, error) {
	if p.Alg != "argon2id" {
		return nil, ErrFormat
	}
	if p.Time == 0 || p.Time > maxArgonTime ||
		p.Threads == 0 || p.Threads > maxArgonThreads ||
		p.Memory < minArgonMemoryKiB || p.Memory > maxArgonMemoryKiB ||
		len(p.Salt) == 0 || len(p.Salt) > maxArgonSaltLen {
		return nil, ErrFormat
	}
	pwKey := argon2.IDKey([]byte(passphrase), p.Salt, p.Time, p.Memory, p.Threads, argonKeyLen)
	if len(deviceKey) == 0 {
		return pwKey, nil
	}
	r := hkdf.New(sha256.New, pwKey, deviceKey, hkdfInfo)
	final := make([]byte, argonKeyLen)
	if _, err := io.ReadFull(r, final); err != nil {
		wipe(pwKey)
		return nil, fmt.Errorf("keystore: derive: %w", err)
	}
	wipe(pwKey)
	return final, nil
}

// seal encrypts plaintext under a key derived from the passphrase and the
// optional device factor, returning a self-describing envelope as JSON. The
// weak-passphrase policy is enforced here, so it covers every write path
// (Store.Save and ExportShare) without applying to the read path, where it
// must not turn a wrong passphrase into an oracle.
func seal(plaintext []byte, passphrase, deviceID string, deviceKey []byte) ([]byte, error) {
	if err := validatePassphrase(passphrase); err != nil {
		return nil, err
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("keystore: salt: %w", err)
	}
	kp := kdfParams{
		Alg:     "argon2id",
		Salt:    salt,
		Time:    argonTimeDefault,
		Memory:  argonMemoryDefault,
		Threads: argonThreadsDefault,
	}
	key, err := deriveKey(passphrase, deviceKey, kp)
	if err != nil {
		return nil, err
	}
	defer wipe(key)

	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("keystore: nonce: %w", err)
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)

	bz, err := json.Marshal(envelope{
		Version:  formatVersion,
		DeviceID: deviceID,
		KDF:      kp,
		Nonce:    nonce,
		Cipher:   ct,
	})
	if err != nil {
		return nil, fmt.Errorf("keystore: marshal envelope: %w", err)
	}
	return bz, nil
}

// open reverses seal. Version is checked before any crypto so a format change
// reports ErrVersionMismatch rather than a generic auth failure.
func open(blob []byte, passphrase string, deviceKey []byte) ([]byte, error) {
	var env envelope
	if err := json.Unmarshal(blob, &env); err != nil {
		return nil, ErrFormat
	}
	if env.Version != formatVersion {
		return nil, ErrVersionMismatch
	}
	if len(env.Nonce) == 0 || len(env.Cipher) == 0 {
		return nil, ErrFormat
	}
	key, err := deriveKey(passphrase, deviceKey, env.KDF)
	if err != nil {
		return nil, err
	}
	defer wipe(key)

	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(env.Nonce) != gcm.NonceSize() {
		return nil, ErrFormat
	}
	pt, err := gcm.Open(nil, env.Nonce, env.Cipher, nil)
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("keystore: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("keystore: gcm: %w", err)
	}
	return gcm, nil
}

// wipe zeroes a derived-key buffer to keep secret material residency minimal.
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
