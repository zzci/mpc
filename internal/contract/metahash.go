package contract

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/gowebpki/jcs"
)

// businessInfo canonical bytes use RFC 8785 JCS via the mature
// github.com/gowebpki/jcs library (latest stable v1.0.1, registry-verified),
// per docs/spec/envelope-canonical.md §4.1/§4.3. JCS is an encoding rule, not
// a cryptographic primitive (S-001 §4.3); the hash is SHA-256, already in the
// design primitive set (no new primitive introduced).

// EmptyMetaHash is SHA-256 of the zero-length byte string — the metaHash when
// businessInfo is absent (missing field or explicit null), per
// docs/spec/envelope-canonical.md §4.2. It is the well-known constant
// e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 and is
// neither H("{}") nor H(`""`).
var EmptyMetaHash = sha256.Sum256(nil)

// MetaHash returns metaHash for a structured businessInfo
// (docs/spec/envelope-canonical.md §4.2): SHA-256 over the RFC 8785 JCS bytes
// of the object, or EmptyMetaHash when bi is nil. bi is marshaled to JSON then
// JCS-canonicalized, so Go struct field order is irrelevant.
func MetaHash(bi *BusinessInfo) ([32]byte, error) {
	if bi == nil {
		return EmptyMetaHash, nil
	}
	raw, err := json.Marshal(bi)
	if err != nil {
		return [32]byte{}, fmt.Errorf("%w: marshal businessInfo: %w", ErrInvalidEnvelope, err)
	}
	return MetaHashFromJSON(raw)
}

// MetaHashFromJSON returns metaHash for a businessInfo carried as a raw JSON
// object — the coord A2 ingestion path (docs/spec/envelope-canonical.md §4.1:
// coord JCS-normalizes the submitted object once). A nil/empty input or the
// JSON literal null is treated as absent and yields EmptyMetaHash.
func MetaHashFromJSON(raw []byte) ([32]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return EmptyMetaHash, nil
	}
	canon, err := jcs.Transform(raw)
	if err != nil {
		return [32]byte{}, fmt.Errorf("%w: businessInfo not canonicalizable JSON: %w", ErrInvalidEnvelope, err)
	}
	return sha256.Sum256(canon), nil
}

// VerifyMetaHash checks req.MetaHash equals MetaHash(req.BusinessInfo), the
// device pre-MPC check of docs/design/contract/protocol.md:25
// ("metaHash==H(businessInfo)"). It returns ErrInvalidEnvelope on mismatch.
func VerifyMetaHash(req *SigningRequest) error {
	want, err := MetaHash(req.BusinessInfo)
	if err != nil {
		return err
	}
	if !bytes.Equal(req.MetaHash, want[:]) {
		return fmt.Errorf("%w: metaHash does not match businessInfo", ErrInvalidEnvelope)
	}
	return nil
}
