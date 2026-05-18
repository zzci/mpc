// Command gen emits the authoritative cross-language golden fixture for
// mock-extsvc. It uses the SAME Go packages a real proposer/coord use
// (internal/contract, internal/addr) so the committed JSON is byte-exact
// truth, not a hand-written guess. The Bun test double asserts its
// TypeScript canonical-serialization / metaHash / proposerSig / RSV-verify
// implementations reproduce these bytes exactly (MEXT-001 "逐字节一致").
//
// Regenerate (must run from repo root, after any change to the contract
// canonicalization or addr derivation):
//
//	go run ./mock-extsvc/testdata/gen > mock-extsvc/testdata/golden.json
//
// Determinism: btcec ECDSA signing is RFC6979 (deterministic), so a fixed
// private key + digest yields a stable signature; the file is reproducible.
package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/btcsuite/btcd/btcec/v2"
	btcecdsa "github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/zzci/mpc/internal/addr"
	"github.com/zzci/mpc/internal/contract"
)

type fixture struct {
	DomainHex        string       `json:"domainHex"`
	EmptyMetaHashHex string       `json:"emptyMetaHashHex"`
	WithBusiness     envelopeCase `json:"withBusiness"`
	NoBusiness       envelopeCase `json:"noBusiness"`
	Proposer         keyVec       `json:"proposer"`
	Group            groupVec     `json:"group"`
}

type envelopeCase struct {
	Envelope          envJSON `json:"envelope"`
	MetaHashHex       string  `json:"metaHashHex"`
	CanonicalHex      string  `json:"canonicalHex"`
	EnvelopeDigestHex string  `json:"envelopeDigestHex"`
	ProposerSigHex    string  `json:"proposerSigHex"`
}

// envJSON is the logical envelope as the external service holds it before
// canonicalization: bytes as base64, times as unix-ms, exactly the shape the
// TypeScript side reconstructs from.
type envJSON struct {
	Version       uint64                 `json:"version"`
	RequestID     string                 `json:"requestId"`
	GroupID       string                 `json:"groupId"`
	Chain         string                 `json:"chain"`
	UnsignedTxB64 string                 `json:"unsignedTxB64"`
	Digest32Hex   string                 `json:"digest32Hex"`
	Proposer      string                 `json:"proposer"`
	CreatedAt     int64                  `json:"createdAt"`
	Expiry        int64                  `json:"expiry"`
	BusinessInfo  *contract.BusinessInfo `json:"businessInfo"`
}

type keyVec struct {
	PrivHex            string `json:"privHex"`
	PubCompressedHex   string `json:"pubCompressedHex"`
	PubUncompressedHex string `json:"pubUncompressedHex"`
}

type groupVec struct {
	keyVec
	EVMAddress  string `json:"evmAddress"`
	TronAddress string `json:"tronAddress"`
	RSV         rsvVec `json:"rsv"`
}

// rsvVec is exactly what coord.verifyRSV / api.md A4 deliver: a 65-byte
// [V+27 || R(32) || S(32)] compact signature over digest32, base64-encoded.
type rsvVec struct {
	Digest32Hex string `json:"digest32Hex"`
	RSVB64      string `json:"rsvB64"`
	RSVHex      string `json:"rsvHex"`
}

func mustPriv(h string) *btcec.PrivateKey {
	raw, err := hex.DecodeString(h)
	if err != nil {
		panic(err)
	}
	pk, _ := btcec.PrivKeyFromBytes(raw)
	return pk
}

func buildCase(reqID string, bi *contract.BusinessInfo, proposerPriv *btcec.PrivateKey) envelopeCase {
	d := make([]byte, 32)
	for i := range d {
		d[i] = 0xAB
	}
	mh, err := contract.MetaHash(bi)
	if err != nil {
		panic(err)
	}
	req := contract.SigningRequest{
		Version:      contract.EnvelopeVersionV1,
		RequestID:    reqID,
		GroupID:      "grp-1",
		Chain:        "ethereum",
		UnsignedTx:   []byte("\x01\x02\x03raw-tx"),
		Digest32:     d,
		Proposer:     "proposer-A",
		CreatedAt:    1_700_000_000_000,
		Expiry:       1_700_000_900_000,
		BusinessInfo: bi,
		MetaHash:     mh[:],
	}
	canon, err := contract.CanonicalBytes(&req)
	if err != nil {
		panic(err)
	}
	dig, err := contract.EnvelopeDigest(&req)
	if err != nil {
		panic(err)
	}
	if err := contract.SignEnvelope(proposerPriv, &req); err != nil {
		panic(err)
	}
	return envelopeCase{
		Envelope: envJSON{
			Version:       req.Version,
			RequestID:     req.RequestID,
			GroupID:       req.GroupID,
			Chain:         req.Chain,
			UnsignedTxB64: base64.StdEncoding.EncodeToString(req.UnsignedTx),
			Digest32Hex:   hex.EncodeToString(req.Digest32),
			Proposer:      req.Proposer,
			CreatedAt:     req.CreatedAt,
			Expiry:        req.Expiry,
			BusinessInfo:  bi,
		},
		MetaHashHex:       hex.EncodeToString(mh[:]),
		CanonicalHex:      hex.EncodeToString(canon),
		EnvelopeDigestHex: hex.EncodeToString(dig[:]),
		ProposerSigHex:    hex.EncodeToString(req.ProposerSig),
	}
}

func keyVecOf(pk *btcec.PrivateKey) keyVec {
	return keyVec{
		PrivHex:            hex.EncodeToString(pk.Serialize()),
		PubCompressedHex:   hex.EncodeToString(pk.PubKey().SerializeCompressed()),
		PubUncompressedHex: hex.EncodeToString(pk.PubKey().SerializeUncompressed()),
	}
}

func main() {
	// Fixed test keys (deterministic, non-secret, test-only).
	proposerPriv := mustPriv("1111111111111111111111111111111111111111111111111111111111111111")
	groupPriv := mustPriv("2222222222222222222222222222222222222222222222222222222222222222")

	bi := &contract.BusinessInfo{
		Title:     "Payout #42",
		Summary:   "Vendor settlement",
		Items:     []string{"invoice-42", "po-7"},
		Refs:      map[string]string{"invoice": "INV-42", "po": "PO-7"},
		Requester: "finance-bot",
		Memo:      "monthly",
	}

	emptyMH := contract.EmptyMetaHash

	// RSV vector: sign an arbitrary digest with the group key in the exact
	// btcec compact layout coord.verifyRSV consumes ([V+27 || R || S]).
	rsvDigest := make([]byte, 32)
	for i := range rsvDigest {
		rsvDigest[i] = byte(i + 1)
	}
	compact, err := btcecdsa.SignCompact(groupPriv, rsvDigest, false)
	if err != nil {
		panic(err)
	}

	groupPub := groupPriv.PubKey().SerializeUncompressed()
	evm, err := addr.ETHAddress(groupPub)
	if err != nil {
		panic(err)
	}
	tron, err := addr.TronAddress(groupPub)
	if err != nil {
		panic(err)
	}

	fx := fixture{
		DomainHex:        hex.EncodeToString(append([]byte("TSS-ENVELOPE-CANONICAL-v1"), 0x00)),
		EmptyMetaHashHex: hex.EncodeToString(emptyMH[:]),
		WithBusiness:     buildCase("123e4567-e89b-12d3-a456-426614174000", bi, proposerPriv),
		NoBusiness:       buildCase("00000000-0000-0000-0000-000000000001", nil, proposerPriv),
		Proposer:         keyVecOf(proposerPriv),
		Group: groupVec{
			keyVec:      keyVecOf(groupPriv),
			EVMAddress:  evm,
			TronAddress: tron,
			RSV: rsvVec{
				Digest32Hex: hex.EncodeToString(rsvDigest),
				RSVB64:      base64.StdEncoding.EncodeToString(compact),
				RSVHex:      hex.EncodeToString(compact),
			},
		},
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&fx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
