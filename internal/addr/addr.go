package addr

import (
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcutil/base58"
	"golang.org/x/crypto/sha3"
)

// ErrInvalidPubKey is returned when the input is not a valid uncompressed
// secp256k1 public key (wrong length, wrong prefix, or not on the curve).
var ErrInvalidPubKey = errors.New("addr: invalid secp256k1 public key")

// uncompressedPubKeyLen is the byte length of an uncompressed secp256k1
// public key: a 0x04 prefix followed by the 32-byte X and 32-byte Y coords.
const uncompressedPubKeyLen = 65

// tronPrefix is the TRON mainnet address version byte (0x41) prepended to the
// 20-byte key hash before Base58Check encoding.
const tronPrefix = 0x41

// ETHAddress derives the EIP-55 checksummed hex address (with 0x prefix) from
// an uncompressed secp256k1 public key. The address is the last 20 bytes of
// keccak256 over the 64-byte public key body.
//
// BSC uses the identical secp256k1 + keccak256 + EIP-55 scheme as Ethereum, so
// a BSC address is the same string; see BSCAddress for an explicitly named
// entry point.
func ETHAddress(pub []byte) (string, error) {
	body, err := pubKeyBody(pub)
	if err != nil {
		return "", err
	}
	hash := keccak256(body)
	return toChecksumHex(hash[12:]), nil
}

// BSCAddress derives the BNB Smart Chain address from an uncompressed
// secp256k1 public key. BSC is EVM-compatible and shares Ethereum's exact
// address scheme (secp256k1 + keccak256 + EIP-55), so this delegates to
// ETHAddress; it exists to make the ETH/BSC/TRON triad explicit at the API.
func BSCAddress(pub []byte) (string, error) {
	return ETHAddress(pub)
}

// TronAddress derives the Base58Check TRON address from an uncompressed
// secp256k1 public key. The payload is 0x41 followed by the last 20 bytes of
// keccak256 over the 64-byte public key body, with a double-SHA-256 checksum.
func TronAddress(pub []byte) (string, error) {
	body, err := pubKeyBody(pub)
	if err != nil {
		return "", err
	}
	hash := keccak256(body)
	return base58.CheckEncode(hash[12:], tronPrefix), nil
}

// pubKeyBody validates that pub is an uncompressed secp256k1 public key on the
// curve and returns its 64-byte body (X || Y, without the 0x04 prefix). The
// input is never mutated; a fresh slice is returned.
func pubKeyBody(pub []byte) ([]byte, error) {
	if len(pub) != uncompressedPubKeyLen {
		return nil, fmt.Errorf("%w: got %d bytes, want %d", ErrInvalidPubKey, len(pub), uncompressedPubKeyLen)
	}
	if pub[0] != 0x04 {
		return nil, fmt.Errorf("%w: prefix 0x%02x, want 0x04 (uncompressed)", ErrInvalidPubKey, pub[0])
	}
	if _, err := btcec.ParsePubKey(pub); err != nil {
		return nil, fmt.Errorf("%w: not on curve: %w", ErrInvalidPubKey, err)
	}
	body := make([]byte, uncompressedPubKeyLen-1)
	copy(body, pub[1:])
	return body, nil
}

// keccak256 returns the Keccak-256 (legacy, not NIST SHA3) digest of data.
func keccak256(data []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	return h.Sum(nil)
}

// toChecksumHex encodes a 20-byte address as an EIP-55 mixed-case hex string
// with a 0x prefix.
func toChecksumHex(addr []byte) string {
	unchecksummed := hex.EncodeToString(addr)
	hash := keccak256([]byte(unchecksummed))
	result := []byte(unchecksummed)
	for i := range result {
		hashByte := hash[i/2]
		if i%2 == 0 {
			hashByte >>= 4
		} else {
			hashByte &= 0x0f
		}
		if result[i] > '9' && hashByte > 7 {
			result[i] -= 32 // ASCII lower -> upper
		}
	}
	return "0x" + string(result)
}
