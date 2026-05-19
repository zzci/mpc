// Package contract defines the protocol.md authoritative types
// (envelope SigningRequest / MpcMessage / CapToken / START
// StartSigning) and the S-001 unique canonical serialization: the
// proposerSig preimage and metaHash are both deterministically built
// from logical field values, never from JSON/protobuf wire bytes, so the
// same logical envelope submitted via JSON or delivered via protobuf
// yields byte-identical to-sign / to-hash inputs. Also covers sessionId
// isolation, senderAuth, version negotiation.
// Authoritative baseline (read-only): docs/design/contract/protocol.md, docs/design/contract/api.md,
// docs/spec/envelope-canonical.md.
package contract
