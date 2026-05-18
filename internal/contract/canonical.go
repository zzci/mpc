package contract

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"

	"golang.org/x/text/unicode/norm"
)

// canonicalDomain is the fixed domain-separation prefix of the envelope
// preimage (docs/spec/envelope-canonical.md §2.2):
//
//	DOMAIN = ASCII("TSS-ENVELOPE-CANONICAL-v1") ‖ 0x00
//
// The trailing 0x00 terminates the constant; the "v1" tag versions this
// canonicalization scheme and is orthogonal to SigningRequest.Version
// (docs/design/contract/protocol.md:83-87).
var canonicalDomain = append([]byte("TSS-ENVELOPE-CANONICAL-v1"), 0x00)

// fixedHashLen is the mandatory byte length of digest32 and metaHash
// (docs/spec/envelope-canonical.md §2.3): SHA-256 output, structurally fixed,
// length-prefix-free but strictly length-validated.
const fixedHashLen = 32

// maxLenPrefix is the largest value a uint32 big-endian length prefix can hold
// (docs/spec/envelope-canonical.md §2.3 rule 2).
const maxLenPrefix = math.MaxUint32

// appendUint32 appends v as 4-byte big-endian, the shared length-prefix
// encoding used by the envelope and senderAuth preimages.
func appendUint32(b []byte, v uint32) []byte {
	return binary.BigEndian.AppendUint32(b, v)
}

// CanonicalBytes builds the canonical preimage P of an envelope
// (docs/spec/envelope-canonical.md §2.1):
//
//	P = DOMAIN ‖ F(version) ‖ F(requestId) ‖ F(groupId) ‖ F(chain)
//	      ‖ F(unsignedTx) ‖ F(digest32) ‖ F(proposer)
//	      ‖ F(createdAt) ‖ F(expiry) ‖ F(metaHash)
//
// businessInfo never enters P; its integrity is carried by metaHash (which is
// in P). proposerSig never enters P; it is the signature over P. The bytes are
// derived purely from logical field values, never from JSON or protobuf wire
// bytes, so JSON-submitted and protobuf-delivered copies of the same logical
// envelope yield identical preimages (S-001 §8). Any field that violates the
// fixed-length / range contract yields ErrInvalidEnvelope, which the verifier
// treats as a rejection (docs/design/contract/protocol.md:25).
func CanonicalBytes(req *SigningRequest) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: nil request", ErrInvalidEnvelope)
	}
	var buf bytes.Buffer
	buf.Write(canonicalDomain)

	writeUint64(&buf, req.Version)

	rid, err := uuidBytes(req.RequestID)
	if err != nil {
		return nil, err
	}
	buf.Write(rid)

	if err := writeLenPrefixed(&buf, "groupId", nfcBytes(req.GroupID)); err != nil {
		return nil, err
	}
	if err := writeLenPrefixed(&buf, "chain", nfcBytes(req.Chain)); err != nil {
		return nil, err
	}
	if err := writeLenPrefixed(&buf, "unsignedTx", req.UnsignedTx); err != nil {
		return nil, err
	}

	if len(req.Digest32) != fixedHashLen {
		return nil, fmt.Errorf("%w: digest32 is %d bytes, want %d", ErrInvalidEnvelope, len(req.Digest32), fixedHashLen)
	}
	buf.Write(req.Digest32)

	if err := writeLenPrefixed(&buf, "proposer", nfcBytes(req.Proposer)); err != nil {
		return nil, err
	}

	writeInt64(&buf, req.CreatedAt)
	writeInt64(&buf, req.Expiry)

	if len(req.MetaHash) != fixedHashLen {
		return nil, fmt.Errorf("%w: metaHash is %d bytes, want %d", ErrInvalidEnvelope, len(req.MetaHash), fixedHashLen)
	}
	buf.Write(req.MetaHash)

	return buf.Bytes(), nil
}

// EnvelopeDigest returns SHA-256(CanonicalBytes(req)) — the 32-byte value the
// proposer signs and verifiers re-derive (docs/spec/envelope-canonical.md
// §2.4).
func EnvelopeDigest(req *SigningRequest) ([32]byte, error) {
	p, err := CanonicalBytes(req)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(p), nil
}

// writeUint64 appends an integer as fixed 8-byte big-endian
// (docs/spec/envelope-canonical.md §2.3: integers are fixed-width big-endian).
func writeUint64(buf *bytes.Buffer, v uint64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	buf.Write(b[:])
}

// writeInt64 appends a signed integer as fixed 8-byte big-endian (two's
// complement). createdAt/expiry are unix milliseconds
// (docs/spec/envelope-canonical.md §2.3 rule 1).
func writeInt64(buf *bytes.Buffer, v int64) {
	writeUint64(buf, uint64(v))
}

// writeLenPrefixed appends a uint32 big-endian length prefix followed by the
// raw bytes (docs/spec/envelope-canonical.md §2.3 rule 2/3). The prefix
// prevents concatenation ambiguity between adjacent variable-length fields.
func writeLenPrefixed(buf *bytes.Buffer, field string, b []byte) error {
	if uint64(len(b)) > maxLenPrefix {
		return fmt.Errorf("%w: %s exceeds uint32 length", ErrInvalidEnvelope, field)
	}
	var lp [4]byte
	binary.BigEndian.PutUint32(lp[:], uint32(len(b)))
	buf.Write(lp[:])
	buf.Write(b)
	return nil
}

// nfcBytes returns the UTF-8 bytes of s after Unicode NFC normalization
// (docs/spec/envelope-canonical.md §2.3 rule 2), so visually identical strings
// produced by different input methods canonicalize identically across parties.
func nfcBytes(s string) []byte {
	return norm.NFC.Bytes([]byte(s))
}

// uuidBytes parses an RFC 4122 UUID string into its 16 raw bytes
// (docs/spec/envelope-canonical.md §2.3: requestId is the binary 16 bytes,
// hex case-insensitive). It accepts the hyphenated 8-4-4-4-12 form and the
// 32-hex unhyphenated form; anything else is an invalid envelope.
func uuidBytes(s string) ([]byte, error) {
	var hexStr string
	switch len(s) {
	case 36:
		if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
			return nil, fmt.Errorf("%w: malformed requestId UUID", ErrInvalidEnvelope)
		}
		hexStr = s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:]
	case 32:
		hexStr = s
	default:
		return nil, fmt.Errorf("%w: requestId is not a UUID (len %d)", ErrInvalidEnvelope, len(s))
	}
	out := make([]byte, 16)
	if _, err := hex.Decode(out, []byte(hexStr)); err != nil {
		return nil, fmt.Errorf("%w: requestId not hex: %w", ErrInvalidEnvelope, err)
	}
	return out, nil
}
