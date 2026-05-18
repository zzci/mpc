package relay

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/zzci/mpc/internal/contract"
)

// CapToken access control implements docs/design/server/server.md R4 layer 2
// (能力令牌) and docs/design/contract/protocol.md §6. The token type and the
// secp256k1-ECDSA-over-SHA256 signature primitive are owned by C-001
// (internal/contract); protocol.md §6 says only that groupSig covers "the
// preimage" without fixing its bytes, and S-001 (envelope-canonical) scopes
// itself to SigningRequest / senderAuth, not CapToken. N-002 is constrained to
// internal/server/relay, so the CapToken canonical preimage is defined here,
// domain-separated from every contract preimage and built with the same
// deterministic rules (NFC + uint32 big-endian length prefixes, fixed-width
// big-endian integers, signature excluded) so it can never collide with or be
// replayed as an envelope / senderAuth signature. Verification reuses the
// project's existing primitive contract.VerifyDigest (no new cryptography),
// matching docs/spec/group-provisioning.md §6.1
// (groupSig = sign(groupKey, H(payload)); verify(groupPubkey, H, groupSig)).
//
// Trust anchor = the self-sovereign wallet-group public key set
// (server.md:130,133). N-002 implements token_verify.source=config only; the
// coord-sync source is rejected at startup because relay MUST NOT depend on
// coord (server.md R5 / N-002 acceptance).

// capTokenDomain separates the CapToken preimage from contract's envelope
// ("TSS-ENVELOPE-CANONICAL-v1") and senderAuth ("TSS-SENDER-AUTH-CANONICAL-v1")
// domains; the trailing 0x00 terminates the constant.
var capTokenDomain = append([]byte("TSS-CAPTOKEN-CANONICAL-v1"), 0x00)

// ErrUntrustedToken is returned when a CapToken's groupSig does not verify
// against any configured trusted group public key, or its scope/validity
// window is unacceptable. Per server.md R4 the relay refuses the reservation /
// registration on any such failure.
var ErrUntrustedToken = errors.New("relay: capability token not trusted")

// errCoordSyncUnsupported is returned at startup when token_verify.source is
// coord-sync: relay must remain independent of coord (server.md R5), so N-002
// supports only the self-sovereign config source.
var errCoordSyncUnsupported = errors.New(
	"relay: token_verify.source=coord-sync not supported (N-002 is config-only; relay must not depend on coord)")

// trustAnchors holds the parsed self-sovereign wallet-group public keys the
// relay verifies CapToken.groupSig against (server.md:130, R4 layer 2). Keys
// are immutable after construction.
type trustAnchors struct {
	pubkeys [][]byte // serialized secp256k1 public keys (compressed/uncompressed)
}

// newTrustAnchors parses the configured base64 group public key set. source
// must be "config"; "coord-sync" is rejected to keep relay coord-independent.
func newTrustAnchors(source string, b64Pubkeys []string) (*trustAnchors, error) {
	switch source {
	case "config":
	case "coord-sync":
		return nil, errCoordSyncUnsupported
	default:
		return nil, fmt.Errorf("relay: unknown token_verify.source %q", source)
	}
	if len(b64Pubkeys) == 0 {
		return nil, errors.New("relay: token_verify.group_pubkeys is empty (no trust anchor)")
	}
	ta := &trustAnchors{pubkeys: make([][]byte, 0, len(b64Pubkeys))}
	for i, s := range b64Pubkeys {
		raw, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("relay: group_pubkeys[%d] not base64: %w", i, err)
		}
		ta.pubkeys = append(ta.pubkeys, raw)
	}
	return ta, nil
}

// capTokenDigest builds SHA-256 over the CapToken canonical preimage:
//
//	P = DOMAIN ‖ F(groupId) ‖ F(memberId) ‖ F(scope)
//	      ‖ I(notBefore) ‖ I(notAfter) ‖ F(nonce)
//
// where F = uint32 big-endian length prefix ‖ bytes (NFC-normalized for the
// string fields), I = 8-byte big-endian. groupSig is excluded (it is the
// signature over this digest).
func capTokenDigest(t *contract.CapToken) ([32]byte, error) {
	var b []byte
	b = append(b, capTokenDomain...)
	var err error
	if b, err = appendLP(b, "groupId", norm.NFC.Bytes([]byte(t.GroupID))); err != nil {
		return [32]byte{}, err
	}
	if b, err = appendLP(b, "memberId", norm.NFC.Bytes([]byte(t.MemberID))); err != nil {
		return [32]byte{}, err
	}
	if b, err = appendLP(b, "scope", norm.NFC.Bytes([]byte(t.Scope))); err != nil {
		return [32]byte{}, err
	}
	b = binary.BigEndian.AppendUint64(b, uint64(t.NotBefore))
	b = binary.BigEndian.AppendUint64(b, uint64(t.NotAfter))
	if b, err = appendLP(b, "nonce", t.Nonce); err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(b), nil
}

// appendLP appends a uint32 big-endian length prefix then the raw bytes; the
// prefix removes concatenation ambiguity between adjacent variable-length
// fields (same rule as contract's envelope preimage).
func appendLP(b []byte, field string, v []byte) ([]byte, error) {
	if uint64(len(v)) > 0xffffffff {
		return nil, fmt.Errorf("%w: %s exceeds uint32 length", ErrUntrustedToken, field)
	}
	b = binary.BigEndian.AppendUint32(b, uint32(len(v)))
	return append(b, v...), nil
}

// verifyCapToken enforces, in order: scope match, validity window (with skew
// tolerance), then groupSig against any trusted anchor. It returns the token
// on success so the caller can key quota by token.Nonce / token.GroupID.
func (ta *trustAnchors) verifyCapToken(
	t *contract.CapToken, want contract.CapScope, now time.Time, skew time.Duration,
) error {
	if t == nil {
		return fmt.Errorf("%w: nil token", ErrUntrustedToken)
	}
	if t.Scope != want {
		return fmt.Errorf("%w: scope %q, want %q", ErrUntrustedToken, t.Scope, want)
	}
	nowMS := now.UnixMilli()
	skewMS := skew.Milliseconds()
	if nowMS+skewMS < t.NotBefore {
		return fmt.Errorf("%w: token not yet valid", ErrUntrustedToken)
	}
	if nowMS-skewMS > t.NotAfter {
		return fmt.Errorf("%w: token expired", ErrUntrustedToken)
	}
	digest, err := capTokenDigest(t)
	if err != nil {
		return err
	}
	for _, pub := range ta.pubkeys {
		if contract.VerifyDigest(pub, digest, t.GroupSig) == nil {
			return nil
		}
	}
	return fmt.Errorf("%w: groupSig verifies against no trusted group key", ErrUntrustedToken)
}
