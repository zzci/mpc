package addr

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcutil/base58"
)

// pubFromScalar returns the uncompressed secp256k1 public key for the private
// scalar n (a small positive integer), used to reach well-known test vectors.
func pubFromScalar(n byte) []byte {
	priv := make([]byte, 32)
	priv[31] = n
	_, pub := btcec.PrivKeyFromBytes(priv)
	return pub.SerializeUncompressed()
}

func TestETHAddress_KnownVectors(t *testing.T) {
	// Canonical secp256k1 scalar -> Ethereum address vectors (widely
	// published "well-known private key" addresses, EIP-55 checksummed).
	cases := []struct {
		scalar byte
		want   string
	}{
		{1, "0x7E5F4552091A69125d5DfCb7b8C2659029395Bdf"},
		{2, "0x2B5AD5c4795c026514f8317c7a215E218DcCD6cF"},
		{3, "0x6813Eb9362372EEF6200f3b1dbC3f819671cBA69"},
	}
	for _, c := range cases {
		got, err := ETHAddress(pubFromScalar(c.scalar))
		if err != nil {
			t.Fatalf("scalar %d: unexpected error: %v", c.scalar, err)
		}
		if got != c.want {
			t.Errorf("scalar %d: ETHAddress = %s, want %s", c.scalar, got, c.want)
		}
	}
}

// TestBSCAddress_SameAsETH pins the design invariant that BSC shares
// Ethereum's exact address scheme: BSCAddress must equal ETHAddress for any
// key. Reuses the canonical ETH scalars without introducing new vectors.
func TestBSCAddress_SameAsETH(t *testing.T) {
	for _, n := range []byte{1, 2, 3} {
		pub := pubFromScalar(n)
		eth, err := ETHAddress(pub)
		if err != nil {
			t.Fatalf("scalar %d: ETHAddress error: %v", n, err)
		}
		bsc, err := BSCAddress(pub)
		if err != nil {
			t.Fatalf("scalar %d: BSCAddress error: %v", n, err)
		}
		if bsc != eth {
			t.Errorf("scalar %d: BSCAddress = %s, want == ETHAddress %s", n, bsc, eth)
		}
	}
}

func TestTronAddress_KnownVector(t *testing.T) {
	// Scalar 1 maps to the same 20-byte key hash as the ETH vector above;
	// Base58Check(0x41 || hash) yields this TRON mainnet address.
	const want = "TMVQGm1qAQYVdetCeGRRkTWYYrLXuHK2HC"
	got, err := TronAddress(pubFromScalar(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("TronAddress = %s, want %s", got, want)
	}
}

// TestTronAddress_RoundTrip checks that, for several keys, the produced
// address decodes back to version 0x41 plus the keccak256[12:] of the key
// body, tying TRON output to the proven ETH derivation path.
func TestTronAddress_RoundTrip(t *testing.T) {
	for _, n := range []byte{1, 2, 3} {
		pub := pubFromScalar(n)
		tron, err := TronAddress(pub)
		if err != nil {
			t.Fatalf("scalar %d: TronAddress error: %v", n, err)
		}
		payload, version, err := base58.CheckDecode(tron)
		if err != nil {
			t.Fatalf("scalar %d: CheckDecode error: %v", n, err)
		}
		if version != tronPrefix {
			t.Errorf("scalar %d: version = 0x%02x, want 0x41", n, version)
		}
		wantHash := keccak256(pub[1:])[12:]
		if !bytes.Equal(payload, wantHash) {
			t.Errorf("scalar %d: payload %x, want %x", n, payload, wantHash)
		}
	}
}

// TestBase58CheckDocVector pins the encoding against the TRON documentation
// address-format example (hex 41E5...32CD0 <-> Base58Check).
func TestBase58CheckDocVector(t *testing.T) {
	raw, _ := hex.DecodeString("E552F6487585C2B58BC2C9BB4492BC1F17132CD0")
	got := base58.CheckEncode(raw, tronPrefix)
	const want = "TWsm8HtU2A5eEzoT8ev8yaoFjHsXLLrckb"
	if got != want {
		t.Errorf("CheckEncode = %s, want %s", got, want)
	}
}

func TestInvalidPubKey(t *testing.T) {
	onCurve := pubFromScalar(1)

	badPrefix := make([]byte, uncompressedPubKeyLen)
	copy(badPrefix, onCurve)
	badPrefix[0] = 0x03 // valid length, wrong prefix

	notOnCurve := make([]byte, uncompressedPubKeyLen)
	notOnCurve[0] = 0x04 // 0x04 || 64 zero bytes is not a curve point

	cases := []struct {
		name string
		pub  []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"short", onCurve[:64]},
		{"long", append(append([]byte{}, onCurve...), 0x00)},
		{"bad prefix", badPrefix},
		{"not on curve", notOnCurve},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ETHAddress(c.pub); !errors.Is(err, ErrInvalidPubKey) {
				t.Errorf("ETHAddress(%s) error = %v, want ErrInvalidPubKey", c.name, err)
			}
			if _, err := TronAddress(c.pub); !errors.Is(err, ErrInvalidPubKey) {
				t.Errorf("TronAddress(%s) error = %v, want ErrInvalidPubKey", c.name, err)
			}
		})
	}
}
