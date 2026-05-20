package hd

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"

	"github.com/bnb-chain/tss-lib/v3/crypto/ckd"
	"github.com/bnb-chain/tss-lib/v3/tss"
)

// MaxIndex is the exclusive upper bound on a non-hardened HD child index
// (address-derivation.md §1 F2: index ∈ [0, 2^31)). uint32 ≥ MaxIndex would
// step into BIP-32's hardened range, which this design explicitly excludes
// (Q1 lock: non-hardened only).
const MaxIndex uint32 = 1 << 31

// ChaincodeLen is the byte length of the HD chaincode the post-DKG
// commit-reveal protocol emits (address-derivation.md §2 / §3 step 4: HKDF L=32).
const ChaincodeLen = 32

// ErrIndexOutOfRange is returned by Derive when index ≥ MaxIndex.
var ErrIndexOutOfRange = errors.New("hd: child index >= 2^31 (non-hardened upper bound)")

// ErrSkipIndex surfaces BIP-32's skip-and-retry rule: when the HMAC's left
// half is zero or ≥ secp256k1 order N the derived key is invalid and the
// caller must try the next index (address-derivation.md §2). The probability
// is astronomical, but the error is exposed so a caller never silently signs
// against a malformed delta.
var ErrSkipIndex = errors.New("hd: derived IL out of range; skip this child index")

// Derive computes (IL, Q_child) for the non-hardened child `index` against the
// master xpub (masterPub, chaincode), per address-derivation.md §2:
//
//	IL = HMAC-SHA512(key=chaincode, data = compressed(masterPub) ‖ index_be32)[0:32]
//	if IL == 0 or IL ≥ N(secp256k1)  → ErrSkipIndex
//	Q_child = masterPub + IL·G
//
// The result is single-machine offline: no MPC round, no network, no private-key
// material. IL is public (anyone holding the xpub recomputes it); only the
// chaincode is a shared secret — and the xpub itself is owning-member-only
// (F1). The 32-byte IL is the keyDerivationDelta the tss-lib KDD signing path
// (address-derivation.md §6) consumes; Q_child is what the signature must
// recover to.
func Derive(masterPub *ecdsa.PublicKey, chaincode []byte, index uint32) (*big.Int, *ecdsa.PublicKey, error) {
	if masterPub == nil || masterPub.X == nil || masterPub.Y == nil {
		return nil, nil, errors.New("hd: master public key is nil or missing coordinates")
	}
	if len(chaincode) != ChaincodeLen {
		return nil, nil, fmt.Errorf("hd: chaincode must be %d bytes, got %d", ChaincodeLen, len(chaincode))
	}
	if index >= MaxIndex {
		return nil, nil, ErrIndexOutOfRange
	}
	curve := tss.S256()
	// A fresh copy of the chaincode keeps the caller's slice immune from any
	// future internal mutation by tss-lib's ckd (it stores the slice by
	// reference on the returned child key).
	cc := make([]byte, ChaincodeLen)
	copy(cc, chaincode)
	parent := &ckd.ExtendedKey{
		PublicKey: ecdsa.PublicKey{Curve: curve, X: masterPub.X, Y: masterPub.Y},
		ChainCode: cc,
	}
	il, child, err := ckd.DeriveChildKey(index, parent, curve)
	if err != nil {
		// tss-lib's ckd rejects both IL==0/IL≥N AND a delta·G that lands on
		// infinity; both are BIP-32's skip-this-index condition. We collapse
		// them into ErrSkipIndex (still wrapping the inner cause via Go's
		// multi-%w for diagnostics) so a caller can errors.Is the skip
		// condition generically.
		return nil, nil, fmt.Errorf("%w: %w", ErrSkipIndex, err)
	}
	childPub := &ecdsa.PublicKey{Curve: curve, X: child.X, Y: child.Y}
	return il, childPub, nil
}

// ChildPubBytes returns childPub in the 65-byte uncompressed (0x04 ‖ X32 ‖ Y32)
// form the internal/addr.{ETHAddress,BSCAddress,TronAddress} entry points
// consume. Returns nil if childPub is nil or has no coordinates so callers can
// fail-fast at the address-derivation boundary rather than at addr's input
// validation.
func ChildPubBytes(childPub *ecdsa.PublicKey) []byte {
	if childPub == nil || childPub.X == nil || childPub.Y == nil {
		return nil
	}
	out := make([]byte, 65)
	out[0] = 0x04
	childPub.X.FillBytes(out[1:33])
	childPub.Y.FillBytes(out[33:65])
	return out
}
