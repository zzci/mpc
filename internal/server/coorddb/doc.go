// Package coorddb provides coord persistence: a single SQLite file +
// versioned migrations (no hand-edited schema) + whole-DB encryption +
// LOCKED lifecycle (Argon2id passphrase in memory only, fail-closed) +
// an in-memory presence set. Authority: docs/design/server/database.md.
package coorddb
