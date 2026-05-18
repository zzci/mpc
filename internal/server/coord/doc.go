// Package coord implements the node's coord role (docs/design/server/server.md Part
// 2, C1-C10): it ingests signed envelopes into the pending list, drives the
// C3 state machine, tracks member liveness, initiates quorum, treats TTL as a
// first-class concern, serves the external (A) and member (B) APIs plus the
// S-002 group/membership provisioning endpoints, and verifies {R,S,V} under
// the group public key before returning a result to the external service.
//
// Hard boundaries (docs/design/server/server.md C1/C8): coord never participates in
// MPC and never holds a share; it only emits START. It does not import
// internal/server/relay — the plaintext-envelope path and the relay
// zero-trust transport path stay logically isolated even in a merged binary.
//
// Authoritative, read-only: docs/design/server/server.md, docs/design/contract/api.md,
// docs/spec/group-provisioning.md (S-002), docs/spec/envelope-canonical.md
// (S-001). It reuses internal/contract (C-001) for envelope canonicalization /
// signature verification and internal/server/coorddb (D-001) for the
// whole-DB-encrypted store, the LOCKED lifecycle, and the in-memory presence
// set.
package coord
