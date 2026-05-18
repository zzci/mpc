package keystore

import (
	"context"
	"crypto/rand"
	"fmt"
)

// SecureArea models a device-bound secret folded into the at-rest encryption
// key alongside the user passphrase (the second of the two factors).
//
// Implementations:
//   - DeviceSecureArea is the production area. It binds to the iOS Keychain /
//     Secure Enclave or the Android Keystore through a native
//     DeviceKeyProvider and fails closed when that hardware is unavailable —
//     there is no software fallback (docs/design/security.md §5,6).
//   - SoftSecureArea holds a random key in process memory. It is test-only
//     key material, never a production fallback for an absent secure area.
//   - PassphraseOnly contributes no factor; it is the portable-backup path
//     (ExportShare/ImportShare), which must restore on a different device.
type SecureArea interface {
	// DeviceKey returns the device-bound secret to mix with the passphrase
	// key. A nil/empty result means "no device factor" (passphrase only).
	DeviceKey(ctx context.Context) ([]byte, error)
	// ID names the area. It is non-sensitive and recorded in the envelope so
	// a blob states which device factor it was sealed with.
	ID() string
}

// SoftSecureArea is an in-memory SecureArea holding a random key. It exists
// only for tests and never persists its key, so a shard on disk cannot be
// opened without separately restoring that key. It is deliberately NOT a
// production fallback: production must use DeviceSecureArea, which fails
// closed rather than degrading to a software-held factor.
type SoftSecureArea struct {
	key []byte
}

// NewSoftSecureArea generates a fresh 32-byte device key.
func NewSoftSecureArea() (*SoftSecureArea, error) {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, fmt.Errorf("keystore: secure area key: %w", err)
	}
	return &SoftSecureArea{key: k}, nil
}

// NewSoftSecureAreaWithKey rebinds an existing device key, e.g. to reopen a
// store after the secure element has been restored. The key is copied.
func NewSoftSecureAreaWithKey(key []byte) (*SoftSecureArea, error) {
	if len(key) == 0 {
		return nil, fmt.Errorf("keystore: secure area key must not be empty")
	}
	k := make([]byte, len(key))
	copy(k, key)
	return &SoftSecureArea{key: k}, nil
}

// DeviceKey returns a copy of the held key.
func (s *SoftSecureArea) DeviceKey(_ context.Context) ([]byte, error) {
	k := make([]byte, len(s.key))
	copy(k, s.key)
	return k, nil
}

// ID identifies the software secure area.
func (s *SoftSecureArea) ID() string { return "soft" }

// PassphraseOnly contributes no device factor. It is the fallback used for
// portable backups (ExportShare/ImportShare), which must restore on a device
// other than the one that created them.
type PassphraseOnly struct{}

// DeviceKey returns no key.
func (PassphraseOnly) DeviceKey(_ context.Context) ([]byte, error) { return nil, nil }

// ID identifies the passphrase-only (no device) factor.
func (PassphraseOnly) ID() string { return "none" }

// DeviceKeyProvider yields the device-bound secret from platform secure
// hardware: the iOS Keychain / Secure Enclave or the Android Keystore,
// implemented in the native rn-bridge layer and injected here. The secret
// originates in secure hardware; this package never derives, stores, nor
// substitutes a software value for it — that is the "no software fallback"
// rule (docs/design/security.md §5,6, docs/design/mcp/sdk.md §6).
type DeviceKeyProvider interface {
	// ProvideDeviceKey returns the device-bound secret. It must return a
	// non-empty key on success and an error whenever secure hardware is
	// unavailable. An empty key with a nil error is treated as a failure,
	// so a missing factor can never silently weaken sealing.
	ProvideDeviceKey(ctx context.Context) ([]byte, error)
}

// DeviceSecureArea is the production SecureArea: it binds the at-rest key to
// platform secure hardware through a DeviceKeyProvider and fails closed when
// that hardware is unavailable. Unlike SoftSecureArea it never holds or
// invents key material itself, so it cannot degrade to a software factor.
type DeviceSecureArea struct {
	provider DeviceKeyProvider
}

// NewDeviceSecureArea binds a DeviceSecureArea to a native key provider. A
// nil provider is rejected rather than tolerated, so production code cannot
// accidentally seal a shard without the device factor.
func NewDeviceSecureArea(provider DeviceKeyProvider) (*DeviceSecureArea, error) {
	if provider == nil {
		return nil, fmt.Errorf("keystore: device key provider must not be nil (no software fallback)")
	}
	return &DeviceSecureArea{provider: provider}, nil
}

// DeviceKey returns the hardware-held device secret. A provider error, or an
// empty key, is propagated as a failure: with no software fallback an
// unavailable secure area fails closed instead of weakening encryption. A
// fresh copy is returned so the caller's defer-wipe of the working key cannot
// zero a buffer the native provider may still own or reuse.
func (d *DeviceSecureArea) DeviceKey(ctx context.Context) ([]byte, error) {
	k, err := d.provider.ProvideDeviceKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("keystore: device secure area unavailable: %w", err)
	}
	if len(k) == 0 {
		return nil, fmt.Errorf("keystore: device secure area returned empty key (no software fallback)")
	}
	out := make([]byte, len(k))
	copy(out, k)
	return out, nil
}

// ID identifies the hardware-backed device factor.
func (d *DeviceSecureArea) ID() string { return "device" }
