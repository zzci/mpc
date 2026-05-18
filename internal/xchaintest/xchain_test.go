package xchaintest

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	btcecdsa "github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/umbracle/fastrlp"
	"golang.org/x/crypto/sha3"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/bnb-chain/tss-lib/v3/ecdsa/keygen"

	"github.com/royqta/mcp-wallet/internal/addr"
	"github.com/royqta/mcp-wallet/internal/contract"
	"github.com/royqta/mcp-wallet/internal/mpc"
	"github.com/royqta/mcp-wallet/internal/txdecode"
)

// eip1559TxType is the EIP-1559 typed-transaction envelope prefix; mirrors the
// package-private constant in internal/txdecode (not modified here).
const eip1559TxType = 0x02

// EIP-155 spec example (the same external anchor internal/txdecode pins): a
// real, spec-fixed signing RLP and its keccak256. Anchors absolute correctness
// of the ETH legacy digest the threshold signer consumes.
const (
	eip155RLP    = "ec098504a817c800825208943535353535353535353535353535353535353535880de0b6b3a764000080018080"
	eip155Digest = "daf5a779ae972f972197303d7b574746c7ef83eadac0f2791ad23db92e4c8e53"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	return b
}

func keccak256(b []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(b)
	return h.Sum(nil)
}

// groupKey runs one in-process 2-of-3 ECDSA keygen over the real public API
// (mpc.Keygen, full Paillier proofs) using tss-lib's bundled pre-params so the
// safe-prime search is skipped. It returns the shares and the group master
// public key in uncompressed secp256k1 form (validated on-curve).
func groupKey(t *testing.T) (shares []mpc.Share, masterPub []byte, pubX, pubY *big.Int) {
	t.Helper()
	fixtures, _, err := keygen.LoadKeygenTestFixtures(3)
	if err != nil {
		t.Fatalf("load tss-lib keygen fixtures (run tss-lib keygen tests to generate them): %v", err)
	}
	pre := make([]keygen.LocalPreParams, 3)
	for i := range pre {
		pre[i] = fixtures[i].LocalPreParams
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	shares, err = mpc.Keygen(ctx, mpc.KeygenConfig{Threshold: 1, Parties: 3, PreParams: pre})
	if err != nil {
		t.Fatalf("mpc.Keygen: %v", err)
	}

	sd, err := mpc.UnmarshalSaveData(shares[0].SaveData)
	if err != nil {
		t.Fatalf("UnmarshalSaveData: %v", err)
	}
	pubX, pubY = sd.ECDSAPub.X(), sd.ECDSAPub.Y()

	raw := make([]byte, 65)
	raw[0] = 0x04
	pubX.FillBytes(raw[1:33])
	pubY.FillBytes(raw[33:65])
	pk, err := btcec.ParsePubKey(raw)
	if err != nil {
		t.Fatalf("master pubkey not on curve: %v", err)
	}
	return shares, pk.SerializeUncompressed(), pubX, pubY
}

// build1559 constructs a real EIP-1559 signing preimage (chainId, nonce,
// maxPrio, maxFee, gas, to, value, data, empty accessList) and returns
// (unsignedTx, keccak256 digest) — the exact bytes/hash a BSC wallet signs.
func build1559(chainID int64, to []byte, value *big.Int) ([]byte, []byte) {
	a := &fastrlp.Arena{}
	arr := a.NewArray()
	arr.Set(a.NewBigInt(big.NewInt(chainID)))
	arr.Set(a.NewUint(7))
	arr.Set(a.NewBigInt(big.NewInt(1_000_000_000)))
	arr.Set(a.NewBigInt(big.NewInt(30_000_000_000)))
	arr.Set(a.NewUint(21000))
	arr.Set(a.NewCopyBytes(to))
	arr.Set(a.NewBigInt(value))
	arr.Set(a.NewBytes(nil))
	arr.Set(a.NewArray()) // empty accessList
	full := append([]byte{eip1559TxType}, arr.MarshalTo(nil)...)
	return full, keccak256(full)
}

// tronTransferRaw builds a real TRON Transaction.raw protobuf carrying one
// native TransferContract, and returns (raw_data, sha256(raw_data)) — TRON's
// signing hash is sha256(raw_data) itself (docs/design/mcp/sdk.md §4).
func tronTransferRaw() ([]byte, []byte) {
	addr21 := func(b byte) []byte {
		a := make([]byte, 21)
		a[0] = 0x41
		for i := 1; i < 21; i++ {
			a[i] = b
		}
		return a
	}
	pb := func(num protowire.Number, v []byte) []byte {
		return protowire.AppendBytes(protowire.AppendTag(nil, num, protowire.BytesType), v)
	}
	pv := func(num protowire.Number, v uint64) []byte {
		return protowire.AppendVarint(protowire.AppendTag(nil, num, protowire.VarintType), v)
	}
	msg := pb(1, addr21(0x11))                // owner_address
	msg = append(msg, pb(2, addr21(0x22))...) // to_address
	msg = append(msg, pv(3, 1_000_000)...)    // amount: 1 TRX
	any := pb(1, []byte("type.googleapis.com/protocol.TransferContract"))
	any = append(any, pb(2, msg)...)
	c := pv(1, 1) // Contract.type = TransferContract
	c = append(c, pb(2, any)...)
	raw := pb(11, c) // Transaction.raw.contract
	d := sha256.Sum256(raw)
	return raw, d[:]
}

// assertLowS asserts S is in the lower half of the secp256k1 order (BIP-0062
// canonical low-S), which tss-lib's finalization round enforces.
func assertLowS(t *testing.T, sig mpc.Signature) {
	t.Helper()
	n := btcec.S256().Params().N
	halfN := new(big.Int).Rsh(n, 1)
	s := new(big.Int).SetBytes(sig.S[:])
	if s.Sign() == 0 {
		t.Fatal("S is zero")
	}
	if s.Cmp(halfN) > 0 {
		t.Fatal("S is not low-S normalized: S > N/2")
	}
}

// TestCrossChainRealDigestRSV is the E-001 end-to-end cross check: one group
// key signs ETH/BSC/TRON real transaction digests; each {R,S,V} is verified by
// secp256k1 ecrecover + stdlib verify, and the recovered signer address is
// cross-checked against internal/addr derivation from the group master key.
func TestCrossChainRealDigestRSV(t *testing.T) {
	shares, masterPub, pubX, pubY := groupKey(t)

	bsc1559Tx, bsc1559Dg := build1559(56, mustHex(t, "00112233445566778899aabbccddeeff00112233"), big.NewInt(12345))
	tronTx, tronDg := tronTransferRaw()

	cases := []struct {
		name   string
		chain  string
		tx     []byte
		digest []byte
		// addrFn derives the chain address from an uncompressed pubkey.
		addrFn func([]byte) (string, error)
	}{
		{"eth-legacy-eip155", "eth", mustHex(t, eip155RLP), mustHex(t, eip155Digest), addr.ETHAddress},
		{"bsc-eip1559", "bsc", bsc1559Tx, bsc1559Dg, addr.BSCAddress},
		{"tron-native-transfer", "tron", tronTx, tronDg, addr.TronAddress},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// 1. End-to-end: txdecode independently recomputes the chain digest
			// from the real unsignedTx and asserts it == digest32. A verified
			// result proves c.digest IS the real chain signing digest.
			res, err := txdecode.New().Decode(&contract.SigningRequest{
				Chain: c.chain, UnsignedTx: c.tx, Digest32: c.digest,
			})
			if err != nil {
				t.Fatalf("txdecode.Decode: %v", err)
			}
			if !res.DigestVerified {
				t.Fatal("expected DigestVerified (digest32 not bound to unsignedTx)")
			}

			// 2. Threshold-sign the digest txdecode just bound.
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			defer cancel()
			sig, err := mpc.Sign(ctx, mpc.SignConfig{
				SessionID: "e001-" + c.name,
				Threshold: 1,
				Shares:    shares[:2], // 2-of-3
				Digest:    c.digest,
			})
			if err != nil {
				t.Fatalf("mpc.Sign: %v", err)
			}

			// 3. low-S normalization.
			assertLowS(t, sig)

			// 4. recovery id V correctness: realistic range, the exact V
			// recovers the master key, and a flipped V does NOT (so V is
			// meaningfully correct, not coincidentally matching).
			if sig.V != 0 && sig.V != 1 {
				t.Fatalf("recovery id V = %d, want 0 or 1", sig.V)
			}
			rec, wasCompressed, err := btcecdsa.RecoverCompact(sig.Compact(), c.digest)
			if err != nil {
				t.Fatalf("ecrecover: %v", err)
			}
			if wasCompressed {
				t.Fatal("ecrecover produced a compressed-key flag; expected uncompressed")
			}
			flipped := sig.Compact()
			flipped[0] ^= 0x01 // flip V parity
			if bad, _, ferr := btcecdsa.RecoverCompact(flipped, c.digest); ferr == nil {
				if string(bad.SerializeUncompressed()) == string(masterPub) {
					t.Fatal("flipped V still recovered the master key; V is not bound")
				}
			}

			// 5. ecrecover restores exactly the group master public key.
			if string(rec.SerializeUncompressed()) != string(masterPub) {
				t.Fatal("recovered pubkey != group master pubkey")
			}

			// 6. address cross-consistency: address derived from the recovered
			// signer key == address derived from the group master key, via the
			// real internal/addr derivation (EVM keccak/EIP-55, TRON base58).
			fromMaster, err := c.addrFn(masterPub)
			if err != nil {
				t.Fatalf("addr from master: %v", err)
			}
			fromRecovered, err := c.addrFn(rec.SerializeUncompressed())
			if err != nil {
				t.Fatalf("addr from recovered: %v", err)
			}
			if fromMaster != fromRecovered {
				t.Fatalf("address mismatch: master=%s recovered=%s", fromMaster, fromRecovered)
			}
			if fromMaster == "" {
				t.Fatal("derived address is empty")
			}

			// 7. stdlib ECDSA verify of {R,S} against the master key, an
			// independent path from ecrecover.
			pk := ecdsa.PublicKey{Curve: btcec.S256(), X: pubX, Y: pubY}
			r := new(big.Int).SetBytes(sig.R[:])
			s := new(big.Int).SetBytes(sig.S[:])
			if !ecdsa.Verify(&pk, c.digest, r, s) {
				t.Fatal("stdlib ecdsa.Verify failed for {R,S} against master pubkey")
			}
		})
	}
}
