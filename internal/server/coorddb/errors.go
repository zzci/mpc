package coorddb

import "errors"

// ErrLocked means the store is LOCKED (not mounted, no in-memory key).
// Every data access returns this under LOCKED (fail-closed); callers
// (X-001/admin) map it to 503 LOCKED (server.md C9b, database.md §7,
// docs/spec/group-provisioning.md §9).
var ErrLocked = errors.New("coorddb: store is LOCKED")

// ErrUnlocked means Unlock was called on an already-UNLOCKED store.
var ErrUnlocked = errors.New("coorddb: store already UNLOCKED")

// ErrEmptyPassphrase means the unlock passphrase was empty. The
// passphrase is only ever passed in at runtime; this module never
// persists it nor reads it from config/env (database.md §7: doing so
// would defeat the file-leak protection).
var ErrEmptyPassphrase = errors.New("coorddb: empty passphrase")

// ErrPlaintextMode means Unlock was called on a dev/test
// encryption-disabled store (NewPlaintextStore, database.md §7.1):
// that mode derives no key and has no passphrase unlock.
var ErrPlaintextMode = errors.New("coorddb: store is in plaintext (encryption-disabled) mode; Unlock not applicable")

// ErrNotPlaintext means OpenInsecure was called on an encrypted store
// (NewStore): plaintext mount is only for a NewPlaintextStore store.
var ErrNotPlaintext = errors.New("coorddb: OpenInsecure requires a plaintext (encryption-disabled) store")
