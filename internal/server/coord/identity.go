package coord

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
)

// distributed-mpc.md §2.1 / §3.1 / api.md B9-B11: identity is the operator-
// pre-declared per-group strict allowlist (Coord.Config.ExpectedMembers).
// All keygen / reshare / attestation gating boils down to lookups against
// that map; this file is the single place those lookups live so the same
// rule is applied identically everywhere.
//
// Comparison discipline: secp256k1 pubkeys are accepted in both 33B
// (compressed) and 65B (uncompressed) wire forms. Operators may store
// either form in config, and clients may submit either form; we compare
// canonicalized bytes so a compressed-vs-uncompressed mix-up still resolves
// to the right identity. We do NOT decompress here (that would import a
// secp256k1 curve dependency for a config-time check) — instead, we accept
// the configured form as-is and require the client to submit a form
// already present in the strict set. This is consistent with R3
// (operator-declared exact set) and avoids leaking key-format normalization
// concerns into the lookup path.

// expectedSet returns the configured identity allowlist for groupID
// (raw bytes). Returns nil when ExpectedMembers has no entry for the
// group (configuration absence → fail-close upstream).
func (c *Coord) expectedSet(groupID string) [][]byte {
	if c.cfg.ExpectedMembers == nil {
		return nil
	}
	return c.cfg.ExpectedMembers[groupID]
}

// expectedHas reports whether the given identity pubkey is present in the
// strict set for groupID. An empty set always returns false (R3: missing
// configuration = nobody is keygen-eligible for that group).
func (c *Coord) expectedHas(groupID string, idPub []byte) bool {
	if len(idPub) == 0 {
		return false
	}
	for _, k := range c.expectedSet(groupID) {
		if bytes.Equal(k, idPub) {
			return true
		}
	}
	return false
}

// expectedSubset returns nil iff every key in candidates is in the strict
// set for groupID. The returned error carries an operator-safe message
// (no key contents) suitable for an apiError.
func (c *Coord) expectedSubset(groupID string, candidates [][]byte) error {
	set := c.expectedSet(groupID)
	if len(set) == 0 {
		return fmt.Errorf("group has no expected_members configured")
	}
	for i, cand := range candidates {
		if len(cand) == 0 {
			return fmt.Errorf("candidate[%d] is empty", i)
		}
		found := false
		for _, k := range set {
			if bytes.Equal(k, cand) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("candidate[%d] not in expected_members", i)
		}
	}
	return nil
}

// decodeIdentityHex parses a hex-encoded secp256k1 identity pubkey from
// the wire and rejects malformed / wrong-length values. The 33B / 65B
// constraint mirrors the config-time validateExpectedMembers check so the
// wire and config use the same forms.
func decodeIdentityHex(s string) ([]byte, error) {
	b, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("identity hex: %w", err)
	}
	if len(b) != 33 && len(b) != 65 {
		return nil, fmt.Errorf("identity pubkey length %d not 33/65", len(b))
	}
	return b, nil
}

// decodeIdentityHexSet parses a slice of hex identity pubkeys (the
// memberSet on api.md B9/B10 requests). Duplicates are rejected so a
// memberSet cannot list one party twice to forge a quorum count.
func decodeIdentityHexSet(in []string) ([][]byte, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("memberSet is empty")
	}
	out := make([][]byte, 0, len(in))
	for i, hk := range in {
		b, err := decodeIdentityHex(hk)
		if err != nil {
			return nil, fmt.Errorf("memberSet[%d]: %w", i, err)
		}
		for j := range out {
			if bytes.Equal(out[j], b) {
				return nil, fmt.Errorf("memberSet[%d] duplicates memberSet[%d]", i, j)
			}
		}
		out = append(out, b)
	}
	return out, nil
}
