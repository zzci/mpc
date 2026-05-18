// Package admin implements the coord-side operations management API
// (docs/design/server/admin.md). A single global operations administrator can:
//
//   - read-only: inspect transaction records, historical signing sessions
//     (request_events timeline + approvals + result), the admin_audit log and
//     relay counts, filtered by group/time/status/proposer
//     (admin.md §1, database.md §6 indexes);
//   - abuse controls: ban a peerID, revoke a circuit-relay reservation, rotate
//     the pnet PSK, set quota/rate-limit params — every control is appended to
//     admin_audit (append-only; the admin cannot mutate or delete it);
//   - DB unlock/relock: strong-auth + passphrase → coorddb derives the
//     whole-DB-encryption key (Argon2id, in memory only, never on disk);
//     relock zeroizes (server.md C9b, database.md §7). Unlock attempts are
//     rate-limited with exponential backoff.
//
// Trust boundary (admin.md §6/§21/§70): the admin is an operations kill-switch,
// not a trust anchor. It cannot issue member admission, see shares or MPC
// payloads (those are never persisted — database.md §7), bypass the
// self-sovereign access control, forge approvals or move funds: the API
// exposes no such surface. Read and control privileges use separate tokens
// (admin.md §4) and the API is not public (admin.md §5: bound to a private
// address; mTLS/OIDC/VPN/IP-allowlist are deployment concerns, as with the
// coord external auth).
//
// The package depends only on the public APIs of internal/server/coorddb
// (D-001) — Store.WithTx for read-only SQL, Store.AppendAdminAudit,
// Store.Unlock/Relock/IsUnlocked — and never modifies that package. Relay-side
// enforcement and metrics are reached through small injected interfaces
// (RelayController/RelayMetrics): the relay role is stateless and
// coord-independent (server.md R5), so a deployment may run it as a separate
// instance; admin-api always records the operator directive in admin_audit and
// enforces it when a controller is wired.
package admin
