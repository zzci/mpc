package txdecode

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/umbracle/fastrlp"

	"github.com/royqta/mcp-wallet/internal/contract"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	return b
}

func req(chain string, tx, digest []byte) *contract.SigningRequest {
	return &contract.SigningRequest{Chain: chain, UnsignedTx: tx, Digest32: digest}
}

// EIP-155 canonical external anchor (EIP-155 spec example): nonce=9,
// gasPrice=20e9, gas=21000, to=0x3535..35, value=1e18, data="", chainId=1.
// Signing RLP and its keccak256 are fixed by the spec — this anchors absolute
// RLP+keccak correctness, not a self-consistent round-trip.
const (
	eip155RLP    = "ec098504a817c800825208943535353535353535353535353535353535353535880de0b6b3a764000080018080"
	eip155Digest = "daf5a779ae972f972197303d7b574746c7ef83eadac0f2791ad23db92e4c8e53"
)

func TestEIP155ExternalAnchor(t *testing.T) {
	tx := mustHex(t, eip155RLP)
	dg := mustHex(t, eip155Digest)
	res, err := New().Decode(req("eth", tx, dg))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !res.DigestVerified {
		t.Fatal("expected digest verified")
	}
	f := res.Facts
	if f.TxType != TxEVMLegacy {
		t.Errorf("TxType=%s", f.TxType)
	}
	if f.To != "0x3535353535353535353535353535353535353535" {
		t.Errorf("To=%s", f.To)
	}
	if f.Value.Cmp(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)) != 0 {
		t.Errorf("Value=%s", f.Value)
	}
	if f.ChainID == nil || f.ChainID.Int64() != 1 {
		t.Errorf("ChainID=%v", f.ChainID)
	}
	if f.Nonce == nil || *f.Nonce != 9 {
		t.Errorf("Nonce=%v", f.Nonce)
	}
	if len(f.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", f.Warnings)
	}
}

func TestEIP155TamperRejects(t *testing.T) {
	tx := mustHex(t, eip155RLP)
	dg := mustHex(t, eip155Digest)

	// (a) tamper digest32 -> mismatch.
	bad := append([]byte(nil), dg...)
	bad[0] ^= 0xff
	if _, err := New().Decode(req("eth", tx, bad)); err == nil {
		t.Fatal("tampered digest32 must reject")
	}

	// (b) tamper unsignedTx (value byte) -> recompute differs -> reject, no
	// facts (decode bug degrades to rejection, never mis-sign).
	bt := append([]byte(nil), tx...)
	bt[len(bt)-12] ^= 0x01
	res, err := New().Decode(req("eth", bt, dg))
	if err == nil || res != nil {
		t.Fatalf("tampered unsignedTx must reject with no facts: res=%v err=%v", res, err)
	}
}

// build1559 constructs an EIP-1559 signing preimage independently and returns
// (unsignedTx, digest). Self-consistent (round-trip) coverage of the 1559
// path; absolute keccak/RLP correctness is anchored by TestEIP155.
func build1559(t *testing.T, chainID int64, to, data []byte, value *big.Int) ([]byte, []byte) {
	t.Helper()
	a := &fastrlp.Arena{}
	arr := a.NewArray()
	arr.Set(a.NewBigInt(big.NewInt(chainID)))
	arr.Set(a.NewUint(7)) // nonce
	arr.Set(a.NewBigInt(big.NewInt(1_000_000_000)))
	arr.Set(a.NewBigInt(big.NewInt(30_000_000_000)))
	arr.Set(a.NewUint(21000))
	arr.Set(a.NewCopyBytes(to))
	arr.Set(a.NewBigInt(value))
	arr.Set(a.NewCopyBytes(data))
	arr.Set(a.NewArray()) // empty accessList
	raw := arr.MarshalTo(nil)
	full := append([]byte{eip1559TxType}, raw...)
	d := keccak256(full)
	return full, d[:]
}

func TestEIP1559RoundTrip(t *testing.T) {
	to := mustHex(t, "00112233445566778899aabbccddeeff00112233")
	tx, dg := build1559(t, 56, to, nil, big.NewInt(12345))
	res, err := New().Decode(req("bsc", tx, dg))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	f := res.Facts
	if f.TxType != TxEVM1559 {
		t.Errorf("TxType=%s", f.TxType)
	}
	if f.ChainID.Int64() != 56 {
		t.Errorf("ChainID=%v", f.ChainID)
	}
	if f.Value.Int64() != 12345 {
		t.Errorf("Value=%v", f.Value)
	}
	if f.MaxFeePerGas == nil || f.MaxFeePerGas.Int64() != 30_000_000_000 {
		t.Errorf("MaxFeePerGas=%v", f.MaxFeePerGas)
	}
}

func TestEVMChainIDLabelMismatchWarns(t *testing.T) {
	to := mustHex(t, "00112233445566778899aabbccddeeff00112233")
	tx, dg := build1559(t, 1, to, nil, big.NewInt(1)) // chainId 1 but label bsc
	res, err := New().Decode(req("bsc", tx, dg))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !hasWarn(res.Facts, "does not match envelope chain label") {
		t.Errorf("expected chainId/label mismatch warning, got %v", res.Facts.Warnings)
	}
}

func TestERC20TransferRecognized(t *testing.T) {
	token := mustHex(t, "dac17f958d2ee523a2206206994597c13d831ec7")
	recip := mustHex(t, "00112233445566778899aabbccddeeff00112233")
	data := append([]byte(nil), selTransfer...)
	data = append(data, make([]byte, 12)...)
	data = append(data, recip...)
	amt := make([]byte, 32)
	big.NewInt(250000).FillBytes(amt)
	data = append(data, amt...)

	tx, dg := build1559(t, 1, token, data, big.NewInt(0))
	res, err := New().Decode(req("eth", tx, dg))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	c := res.Facts.Call
	if c == nil || c.Kind != CallERC20Transfer {
		t.Fatalf("call=%+v", c)
	}
	if !addrEqual(c.Recipient, "0x00112233445566778899aabbccddeeff00112233") {
		t.Errorf("recipient=%s", c.Recipient)
	}
	if c.Amount.Int64() != 250000 {
		t.Errorf("amount=%v", c.Amount)
	}
	if !addrEqual(c.TokenContract, "0xdac17f958d2ee523a2206206994597c13d831ec7") {
		t.Errorf("token=%s", c.TokenContract)
	}
}

func TestUnrecognizedCallNotFabricated(t *testing.T) {
	token := mustHex(t, "dac17f958d2ee523a2206206994597c13d831ec7")
	data := mustHex(t, "deadbeef0011223344") // unknown selector
	tx, dg := build1559(t, 1, token, data, big.NewInt(0))
	res, err := New().Decode(req("eth", tx, dg))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	c := res.Facts.Call
	if c == nil || c.Kind != CallUnrecognized {
		t.Fatalf("expected unrecognized, got %+v", c)
	}
	if c.Selector != "0xdeadbeef" {
		t.Errorf("selector=%s", c.Selector)
	}
	if !bytes.Equal(c.RawCallData, data) {
		t.Errorf("raw calldata not retained")
	}
	if !hasWarn(res.Facts, "unrecognized contract call selector") {
		t.Errorf("expected caution warning, got %v", res.Facts.Warnings)
	}
}

func TestMalformedEVMRejected(t *testing.T) {
	cases := map[string][]byte{
		"empty":      {},
		"not-a-list": {0x82, 0x01, 0x02},
		"garbage":    {0xff, 0xff, 0xff},
		"short-1559": {eip1559TxType, 0xc0},
	}
	for name, tx := range cases {
		t.Run(name, func(t *testing.T) {
			d := make([]byte, 32)
			if _, err := New().Decode(req("eth", tx, d)); err == nil {
				t.Fatal("malformed EVM tx must be rejected")
			}
		})
	}
}

func hasWarn(f *Facts, sub string) bool {
	for _, w := range f.Warnings {
		if bytes.Contains([]byte(w), []byte(sub)) {
			return true
		}
	}
	return false
}
