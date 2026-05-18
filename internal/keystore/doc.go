// Package keystore seals keygen save-data shards at rest and provides an
// encrypted backup/restore path.
//
// Threat model: a shard (one party's tss-lib keygen output) must never touch
// disk in plaintext. At-rest encryption binds two factors — a user passphrase
// (Argon2id-derived, subject to a weak-passphrase policy) and a device
// secure-area secret. In production the device factor is DeviceSecureArea,
// backed by the iOS Keychain / Secure Enclave or the Android Keystore; it
// fails closed when secure hardware is unavailable, with no software
// fallback. SoftSecureArea is test-only key material. Backups are
// passphrase-only so they remain portable across devices, matching
// docs/design/mcp/sdk.md §6.
//
// AEAD (AES-256-GCM) makes a wrong passphrase or any corruption fail closed as
// an explicit, non-leaking error; an envelope version guards format changes.
// Argon2 parameters are bounded on read so an untrusted backup cannot force a
// memory-amplification DoS.
package keystore
