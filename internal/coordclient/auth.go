package coordclient

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/btcsuite/btcd/btcec/v2"

	"github.com/royqta/mcp-wallet/internal/contract"
)

// Member-side reproduction of the coord member-auth scheme
// (docs/design/contract/api.md B1, mirrored from internal/server/coord/auth.go
// which states "the future coord-client (B-002) reproduces it byte-for-byte").
// Every B request is signed by the member identity key over the canonical
// (memberId, method, params, ts, nonce) preimage and carried in headers; the
// coord verifies with group_members.identity_pubkey, isolates by groupId, and
// rejects an out-of-window ts or a replayed nonce (api.md D).
//
// Byte-for-byte contract with the server:
//   - domain = "TSS-COORD-MEMBER-AUTH-v1" ‖ 0x00
//   - preimage = domain ‖ LP(memberId) ‖ LP(method) ‖ LP(boundHash)
//     ‖ BE64(ts) ‖ LP(nonce)
//   - LP(x) = uint32-big-endian(len(x)) ‖ x
//   - boundHash = SHA-256( method ‖ "|" ‖ groupId ‖ "|" ‖ params )
//     (the server computes bound=method+"|"+groupId+"|"+params then signs
//     hash(bound); api.go memberGate)
//   - signature = secp256k1 ECDSA(SHA-256(preimage)), DER-serialized
//     (reuses contract.SignDigest, the project's shared primitive)
//   - params = exact request-body bytes for POST endpoints, exact raw query
//     string for GET endpoints (server uses io.ReadAll body / r.URL.RawQuery)
//
// A distinct domain prevents cross-protocol signature reuse against the
// envelope / senderAuth preimages.

const (
	headerMemberID    = "X-Member-Id"
	headerMemberTS    = "X-Member-Ts"
	headerMemberNonce = "X-Member-Nonce"
	headerMemberSig   = "X-Member-Sig"

	// memberAuthWindow mirrors the server's accepted ts skew
	// (internal/server/coord/auth.go memberAuthWindow); ts is emitted as the
	// current time so a request stays inside this window.
	nonceLen = 16
)

var memberAuthDomain = append([]byte("TSS-COORD-MEMBER-AUTH-v1"), 0x00)

// appendLP appends a uint32-big-endian length prefix then the bytes, matching
// the server's appendLP exactly so the preimage is byte-identical.
func appendLP(b, v []byte) []byte {
	var lp [4]byte
	binary.BigEndian.PutUint32(lp[:], uint32(len(v)))
	b = append(b, lp[:]...)
	return append(b, v...)
}

// boundHash reproduces the server's hash(method+"|"+groupId+"|"+params).
func boundHash(method, groupID string, params []byte) []byte {
	bound := append([]byte(method+"|"+groupID+"|"), params...)
	h := sha256.Sum256(bound)
	return h[:]
}

// memberAuthDigest builds SHA-256 over the canonical
// (memberId, method, boundHash, ts, nonce) preimage. It is identical to the
// server's memberAuthDigest with params = boundHash.
func memberAuthDigest(memberID, method string, bound []byte, ts int64, nonce []byte) [32]byte {
	var b []byte
	b = append(b, memberAuthDomain...)
	b = appendLP(b, []byte(memberID))
	b = appendLP(b, []byte(method))
	b = appendLP(b, bound)
	var tb [8]byte
	binary.BigEndian.PutUint64(tb[:], uint64(ts))
	b = append(b, tb[:]...)
	b = appendLP(b, nonce)
	return sha256.Sum256(b)
}

// signRequest sets the four X-Member-* headers on req for the given coord
// method and key params. A fresh nonce and the current ts are used on every
// call so a retried request is never a replay (api.md D nonce single-use).
func (c *Client) signRequest(req *http.Request, method string, params []byte) error {
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(c.rng, nonce); err != nil {
		return fmt.Errorf("coordclient: nonce: %w", err)
	}
	ts := c.now().UnixMilli()
	digest := memberAuthDigest(c.memberID, method, boundHash(method, c.groupID, params), ts, nonce)
	sig := contract.SignDigest(c.priv, digest)

	req.Header.Set(headerMemberID, c.memberID)
	req.Header.Set(headerMemberTS, strconv.FormatInt(ts, 10))
	req.Header.Set(headerMemberNonce, base64.StdEncoding.EncodeToString(nonce))
	req.Header.Set(headerMemberSig, base64.StdEncoding.EncodeToString(sig))
	return nil
}

// MemberPublicKey returns the member identity public key in the serialized
// (compressed secp256k1) form coord stores as group_members.identity_pubkey.
func (c *Client) MemberPublicKey() []byte {
	return c.priv.PubKey().SerializeCompressed()
}

// loadIdentityKey parses a raw 32-byte secp256k1 scalar into a member identity
// key. It is exposed for callers holding the key as bytes (keystore export).
func loadIdentityKey(raw []byte) (*btcec.PrivateKey, error) {
	if len(raw) != 32 {
		return nil, fmt.Errorf("coordclient: identity key must be 32 bytes, got %d", len(raw))
	}
	priv, _ := btcec.PrivKeyFromBytes(raw)
	return priv, nil
}
