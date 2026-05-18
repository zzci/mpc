package txdecode

import (
	"encoding/hex"
	"strings"

	"github.com/btcsuite/btcutil/base58"
	"golang.org/x/crypto/sha3"
)

// tronAddrPrefix is the TRON mainnet address version byte (0x41) that prefixes
// the 20-byte key hash in protobuf addresses and Base58Check encoding; matches
// internal/addr.tronPrefix.
const tronAddrPrefix = 0x41

// normalizeChain maps an opaque envelope chain label to a Chain. The label is
// proposer-signed (in the canonical preimage, C-001) but not chain-binding;
// the authoritative chain for EVM is the signed embedded chainId, which the
// EVM decoder cross-checks against this label. ok=false => unsupported chain.
func normalizeChain(label string) (Chain, bool) {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "eth", "ethereum", "evm", "eip155":
		return ChainETH, true
	case "bsc", "bnb", "bnbchain", "bnb-smart-chain", "binance-smart-chain":
		return ChainBSC, true
	case "tron", "trx":
		return ChainTRON, true
	default:
		return "", false
	}
}

// keccak256 returns the Keccak-256 (legacy, not SHA3-256) digest. Uses the
// existing golang.org/x/crypto/sha3 dependency (same primitive internal/addr
// uses); no hash primitive is hand-implemented (task constraint).
func keccak256(b []byte) [32]byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(b)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// toChecksumHex renders a 20-byte EVM address as an EIP-55 mixed-case
// checksummed 0x-hex string. Mirrors internal/addr's scheme so A-zone
// addresses are comparable with derived wallet addresses.
func toChecksumHex(addr20 []byte) string {
	lower := hex.EncodeToString(addr20)
	hash := keccak256([]byte(lower))
	out := make([]byte, len(lower))
	for i := 0; i < len(lower); i++ {
		c := lower[i]
		if c >= 'a' && c <= 'f' {
			// nibble i of the hash: high nibble for even i, low for odd.
			var nibble byte
			if i%2 == 0 {
				nibble = hash[i/2] >> 4
			} else {
				nibble = hash[i/2] & 0x0f
			}
			if nibble >= 8 {
				c -= 32 // to uppercase
			}
		}
		out[i] = c
	}
	return "0x" + string(out)
}

// tronBase58 renders a TRON address. raw is either the 21-byte protobuf form
// (0x41 ‖ 20) or a bare 20-byte EVM-style hash (as found inside TRC20
// calldata). A non-0x41 21-byte prefix is reported via ok=false so the caller
// can raise a caution rather than silently mis-display.
func tronBase58(raw []byte) (addr string, ok bool) {
	switch len(raw) {
	case 21:
		if raw[0] != tronAddrPrefix {
			return "0x" + hex.EncodeToString(raw), false
		}
		return base58.CheckEncode(raw[1:], tronAddrPrefix), true
	case 20:
		return base58.CheckEncode(raw, tronAddrPrefix), true
	default:
		return "0x" + hex.EncodeToString(raw), false
	}
}

// selectorHex renders a 4-byte selector as 0x-hex.
func selectorHex(sel []byte) string { return "0x" + hex.EncodeToString(sel) }
