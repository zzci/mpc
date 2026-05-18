// Package coordclient is the member-side coord API client: pull pending
// signing requests, approve/reject, heartbeat, register push, receive START,
// and report {R,S,V}, with member-identity signature auth and TTL awareness
// (docs/design/contract/api.md B, docs/design/mcp/sdk.md §1 coord-client module).
//
// Authoritative read-only baselines: docs/design/contract/api.md (B1-B7 endpoints,
// C error table, D replay/cursor rules), docs/design/contract/protocol.md (§1
// envelope, §5 START, §7 versioning), docs/design/mcp/sdk.md §1. The client reuses
// internal/contract (C-001) for envelope canonicalization, metaHash,
// proposerSig verification, START parsing and version checks — it never
// re-derives a preimage contract already owns.
//
// Auth (B1): every B request is signed by the member identity key over the
// canonical (memberId, method, params, ts, nonce) preimage, reproduced
// byte-for-byte from internal/server/coord/auth.go (which names B-002 as the
// reproducing peer). A fresh nonce and ts are generated on every attempt,
// including retries, so a retried request is never a replay (api.md D).
//
// Resilience: 503 LOCKED is treated as a backoff-retry signal, never a
// terminal outcome (api.md:84); 5xx INTERNAL and 429 are likewise retried with
// full-jittered exponential backoff bounded by RetryPolicy and the caller's
// context. Terminal 4xx (INVALID_ENVELOPE / UNAUTHENTICATED / FORBIDDEN /
// NOT_FOUND / STATE_CONFLICT / EXPIRED) return immediately as a typed
// *APIError; errors.Is bridges to the package sentinels.
//
// TTL is first-class (PLAN.md §7): expired items never surface in B3, and the
// device re-checks remaining TTL / envelope expiry before approving and before
// signing; an expired decision/result yields 410 → ErrExpired (terminal).
//
// This module performs no tx-decode or chain-digest re-derivation and no
// libp2p/MPC: those belong to the tx-decode and transport/mpc-core modules
// (docs/design/mcp/sdk.md §3-§4). Implemented under task B-002.
package coordclient
