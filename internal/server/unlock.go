package server

// Interface seam for DB unlock-passphrase handling (server.md C9b,
// server/database.md §7).
//
// The coord store is whole-DB encrypted; the operator passphrase that
// derives the key NEVER enters config files / environment variables /
// KMS (doing so would defeat the file-leak protection). So this config
// package exposes no passphrase field and Config does not reference this
// interface; the passphrase is provided in memory only, at coord
// runtime, via the admin-api unlock interaction, and zeroized on relock.
//
// N-001 only declares the seam; it does not implement unlock —
// unlock/relock/rate-limit-backoff are X-001 / admin territory
// (server/admin.md). The relay role has no DB and no unlock.

// UnlockProvider supplies the in-memory unlock passphrase for the coord
// whole-DB encryption. The implementation is injected by the admin-api
// during the unlock interaction; callers should zeroize the returned
// bytes promptly when done.
type UnlockProvider interface {
	// Passphrase returns the current in-memory unlock passphrase; it
	// should return an error while LOCKED.
	Passphrase() ([]byte, error)
}
