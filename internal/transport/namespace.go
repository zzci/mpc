package transport

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
)

// rendezvousLabel is the fixed HMAC message of the rendezvous namespace
// derivation (docs/design/contract/protocol.md:35). The group secret is the HMAC
// key so the namespace is unguessable without group membership yet
// deterministic across members.
const rendezvousLabel = "tss-group"

// rendezvousEncoding is unpadded lowercase RFC 4648 base32 — a stable,
// case-insensitive, DNS/topic-safe encoding for the namespace string.
var rendezvousEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// RendezvousNamespace returns base32(HMAC-SHA256(groupSecret,"tss-group")),
// the rendezvous namespace every group member registers and discovers under
// (docs/design/contract/protocol.md:35). It is a pure deterministic function: the
// same groupSecret always yields the same namespace, and the namespace leaks
// nothing about the secret.
func RendezvousNamespace(groupSecret []byte) string {
	mac := hmac.New(sha256.New, groupSecret)
	mac.Write([]byte(rendezvousLabel))
	return rendezvousEncoding.EncodeToString(mac.Sum(nil))
}
