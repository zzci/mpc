package contract

import (
	"crypto/sha256"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
)

// The proposer / member identity signature scheme is fixed by
// docs/spec/envelope-canonical.md §2.4 to secp256k1 ECDSA over the SHA-256 of
// the canonical preimage, reusing the project's existing btcec/v2 stack (same
// curve as internal/addr); no new cryptographic primitive is introduced.
// Signatures are DER-serialized, matching btcec defaults.

// SignDigest produces a DER secp256k1 ECDSA signature over a 32-byte digest.
// It is the shared signing primitive for proposerSig and senderAuth.
func SignDigest(priv *btcec.PrivateKey, digest [32]byte) []byte {
	return ecdsa.Sign(priv, digest[:]).Serialize()
}

// VerifyDigest verifies a DER secp256k1 ECDSA signature over a 32-byte digest
// against a serialized public key (compressed or uncompressed). It returns
// ErrBadSignature on any parse or verification failure.
func VerifyDigest(pub []byte, digest [32]byte, sig []byte) error {
	pk, err := btcec.ParsePubKey(pub)
	if err != nil {
		return fmt.Errorf("%w: bad public key: %w", ErrBadSignature, err)
	}
	s, err := ecdsa.ParseDERSignature(sig)
	if err != nil {
		return fmt.Errorf("%w: bad signature encoding: %w", ErrBadSignature, err)
	}
	if !s.Verify(digest[:], pk) {
		return ErrBadSignature
	}
	return nil
}

// SignEnvelope sets req.ProposerSig to the proposer's signature over the
// canonical envelope digest (docs/spec/envelope-canonical.md §2.4). The
// signature is computed before assignment so ProposerSig itself never enters
// the preimage.
func SignEnvelope(priv *btcec.PrivateKey, req *SigningRequest) error {
	digest, err := EnvelopeDigest(req)
	if err != nil {
		return err
	}
	req.ProposerSig = SignDigest(priv, digest)
	return nil
}

// VerifyProposerSig re-derives the canonical envelope digest and verifies
// req.ProposerSig against proposerPub. This is the device's pre-MPC
// proposerSig check (docs/design/contract/protocol.md:25); coord uses the same
// check with the group-known proposer key (S-001 §2.4).
func VerifyProposerSig(req *SigningRequest, proposerPub []byte) error {
	digest, err := EnvelopeDigest(req)
	if err != nil {
		return err
	}
	return VerifyDigest(proposerPub, digest, req.ProposerSig)
}

// senderAuthDomain separates the member-identity (sessionId,round,payload)
// preimage from the envelope preimage so a signature for one can never be
// replayed as the other.
var senderAuthDomain = append([]byte("TSS-SENDER-AUTH-CANONICAL-v1"), 0x00)

// SenderAuthDigest returns SHA-256 over the canonical preimage of an
// MpcMessage's authenticated triple (sessionId, round, payload), per
// docs/design/contract/protocol.md:54. The same length-prefixing rules as the
// envelope preimage prevent field-boundary ambiguity.
func SenderAuthDigest(sessionID string, round uint32, payload []byte) ([32]byte, error) {
	var b []byte
	b = append(b, senderAuthDomain...)
	sid := nfcBytes(sessionID)
	if uint64(len(sid)) > maxLenPrefix {
		return [32]byte{}, fmt.Errorf("%w: sessionId exceeds uint32 length", ErrInvalidEnvelope)
	}
	b = appendUint32(b, uint32(len(sid)))
	b = append(b, sid...)
	b = appendUint32(b, round)
	if uint64(len(payload)) > maxLenPrefix {
		return [32]byte{}, fmt.Errorf("%w: payload exceeds uint32 length", ErrInvalidEnvelope)
	}
	b = appendUint32(b, uint32(len(payload)))
	b = append(b, payload...)
	return sha256.Sum256(b), nil
}

// SignSenderAuth sets msg.SenderAuth to the member's signature over
// SenderAuthDigest(msg) (docs/design/contract/protocol.md:54), computed before
// assignment so it never feeds back into the digest.
func SignSenderAuth(priv *btcec.PrivateKey, msg *MpcMessage) error {
	digest, err := SenderAuthDigest(msg.SessionID, msg.Round, msg.Payload)
	if err != nil {
		return err
	}
	msg.SenderAuth = SignDigest(priv, digest)
	return nil
}

// VerifySenderAuth verifies msg.SenderAuth against memberPub over the
// (sessionId,round,payload) digest — the recipient's member-identity check
// above the tss-lib layer (docs/design/contract/protocol.md:54).
func VerifySenderAuth(msg *MpcMessage, memberPub []byte) error {
	digest, err := SenderAuthDigest(msg.SessionID, msg.Round, msg.Payload)
	if err != nil {
		return err
	}
	return VerifyDigest(memberPub, digest, msg.SenderAuth)
}
