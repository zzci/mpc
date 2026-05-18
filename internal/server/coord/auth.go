package coord

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/royqta/mcp-wallet/internal/contract"
)

// Authentication (docs/design/contract/api.md A1/B1, group-provisioning.md §6).
//
//   - External (A): transport mTLS or API key, plus the envelope proposerSig
//     verified against the proposer public key (api.md:11). api.md leaves
//     proposer-key distribution unspecified and there is no provisioning
//     column for it; coord adopts the self-describing convention "proposer
//     identifier == its secp256k1 public key" (hex, compressed or
//     uncompressed), consistent with the self-sovereign trust model used
//     elsewhere in this design. This is overridable via proposerKeyResolver
//     for a future registry; documented as an api.md implementation closure
//     (docs/design/ unchanged).
//   - Member (B): every request is signed by the member identity key over
//     (memberId, method, key params, ts, nonce); coord verifies with
//     group_members.identity_pubkey, isolates by groupId, and rejects an
//     out-of-window ts or a replayed nonce (api.md D: ts+nonce replay guard).
//
// The member-auth preimage uses the same canonicalization discipline as the
// envelope (S-001 §2.3): a fixed domain prefix then uint32-big-endian
// length-prefixed fields, so the future coord-client (B-002) reproduces it
// byte-for-byte. A distinct domain prevents cross-protocol signature reuse.

var memberAuthDomain = append([]byte("TSS-COORD-MEMBER-AUTH-v1"), 0x00)

// memberAuthWindow bounds acceptable clock skew for the signed ts and is also
// the nonce-retention horizon (a nonce cannot be replayed within it, and after
// it the ts is already rejected).
const memberAuthWindow = 5 * time.Minute

// memberAuthSig is the parsed member-signed authenticator carried by B
// requests (transport detail — JSON body/header — handled in api.go).
type memberAuthSig struct {
	MemberID string
	TS       int64 // unix ms
	Nonce    []byte
	Sig      []byte
}

// memberAuthDigest builds SHA-256 over the canonical (memberId, method,
// params, ts, nonce) preimage. `params` is the caller-chosen stable binding of
// the request's key parameters (e.g. groupId, requestId) — order is fixed by
// the caller and must match the client.
func memberAuthDigest(memberID, method string, params []byte, ts int64, nonce []byte) [32]byte {
	var b []byte
	b = append(b, memberAuthDomain...)
	b = appendLP(b, []byte(memberID))
	b = appendLP(b, []byte(method))
	b = appendLP(b, params)
	var tb [8]byte
	binary.BigEndian.PutUint64(tb[:], uint64(ts))
	b = append(b, tb[:]...)
	b = appendLP(b, nonce)
	return sha256.Sum256(b)
}

func appendLP(b, v []byte) []byte {
	var lp [4]byte
	binary.BigEndian.PutUint32(lp[:], uint32(len(v)))
	b = append(b, lp[:]...)
	return append(b, v...)
}

// verifyMemberAuth checks the ts window, the nonce (single-use within the
// window), then the secp256k1 signature against identityPub. Order matters:
// the ts/nonce gate is cheap and rejects replays before the EC verify.
func (c *Coord) verifyMemberAuth(a memberAuthSig, method string, params, identityPub []byte) error {
	tnow := c.clock.Now()
	now := unixMillis(tnow)
	if d := now - a.TS; d > memberAuthWindow.Milliseconds() || d < -memberAuthWindow.Milliseconds() {
		return errUnauthenticated("stale or future request timestamp")
	}
	if len(a.Nonce) == 0 {
		return errUnauthenticated("missing nonce")
	}
	if !c.nonces.use(a.MemberID, a.Nonce, tnow, tnow.Add(memberAuthWindow)) {
		return errUnauthenticated("nonce replay")
	}
	digest := memberAuthDigest(a.MemberID, method, params, a.TS, a.Nonce)
	if err := contract.VerifyDigest(identityPub, digest, a.Sig); err != nil {
		return errUnauthenticated("bad member signature")
	}
	return nil
}

// checkAPIKey constant-time compares the presented external API key (api.md
// A1, ExternalAuth=="api_key").
func (c *Coord) checkAPIKey(presented string) error {
	if subtle.ConstantTimeCompare([]byte(presented), []byte(c.cfg.APIKey)) != 1 {
		return errUnauthenticated("invalid api key")
	}
	return nil
}

// proposerKeyResolver maps a proposer identifier to its secp256k1 public key.
// The default treats the identifier as a hex-encoded key (see file doc); tests
// and a future registry can substitute another resolver.
type proposerKeyResolver func(proposer string) ([]byte, error)

func defaultProposerKey(proposer string) ([]byte, error) {
	pub, err := hex.DecodeString(proposer)
	if err != nil {
		return nil, fmt.Errorf("coord: proposer is not a hex public key: %w", err)
	}
	// P6 strong validation: only the secp256k1 serialized lengths are accepted
	// (33 compressed / 65 uncompressed). A wrong-length identifier is rejected
	// here as a clean INVALID_ENVELOPE instead of reaching the EC verify.
	if len(pub) != 33 && len(pub) != 65 {
		return nil, fmt.Errorf("coord: proposer key is not a secp256k1 public key length")
	}
	return pub, nil
}

// nonceCache is an in-memory single-use nonce set with per-entry expiry. coord
// is a single logical node (docs/design/server/server.md C9) so an in-process map
// is sufficient; entries self-expire on the auth window.
type nonceCache struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func newNonceCache() *nonceCache { return &nonceCache{seen: map[string]time.Time{}} }

// use records (memberID,nonce) and returns false if it was already present and
// unexpired (a replay). It opportunistically evicts expired entries. `now` is
// the injected-clock reading (clock.go: every now-reading goes through Clock)
// and must be consistent with `expiry`, which the caller derives from the same
// clock; reading the wall clock here would desync the two and silently drop
// live nonces under a deterministic test clock.
func (n *nonceCache) use(memberID string, nonce []byte, now, expiry time.Time) bool {
	key := memberID + ":" + hex.EncodeToString(nonce)
	n.mu.Lock()
	defer n.mu.Unlock()
	for k, exp := range n.seen {
		if now.After(exp) {
			delete(n.seen, k)
		}
	}
	if exp, ok := n.seen[key]; ok && now.Before(exp) {
		return false
	}
	n.seen[key] = expiry
	return true
}
