package walletcli

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"

	"github.com/zzci/mpc/internal/addr"
	"github.com/zzci/mpc/internal/hd"
	"github.com/zzci/mpc/sdk"
)

// xpubOp queries the coord B8 endpoint via the shared SDK (1:1 delegation,
// same shell-agnostic discipline as fetchOp). The reqJSON shape is
// `{coordBaseURL, groupId, memberId, memberKeyHex}` — exactly the inputs the
// FetchTransactions reqJSON uses, so a wallet that already pulls the pending
// list reuses the same identity/transport setup to fetch the xpub. The
// returned JSON is `{ecdsaPubkeyHex, chaincodeHex}` per api.md B8; the
// caller persists this verbatim and reuses it for every offline derive.
func xpubOp(s *sdk.SDK, reqJSON string) (string, error) {
	return s.FetchXpub(reqJSON)
}

// addrView is the offline-derive result for `wallet address <i>`: one row of
// the address book for child index `i`, derived purely from the cached xpub.
// Every field is reproducible from (ecdsaPubkey, chaincode, index) by anyone
// with the same xpub — IL is public, the chaincode is the only secret and it
// stays only in the caller's memory.
type addrView struct {
	Index          uint32 `json:"index"`
	ECDSAPubkeyHex string `json:"ecdsaPubkeyHex"`
	ChaincodeHex   string `json:"chaincodeHex"`
	ChildPubkeyHex string `json:"childPubkeyHex"`
	ILHex          string `json:"ilHex"`
	EVMAddress     string `json:"evmAddress"`
	BSCAddress     string `json:"bscAddress"`
	TronAddress    string `json:"tronAddress"`
}

// addressOp performs the single-machine offline HD derive (docs/design/mcp/
// address-derivation.md §2 / §6): no MPC, no network, no private-key material
// — pure public-key arithmetic against the cached xpub. xpubJSON is the
// verbatim B8 response stored by the caller; index ∈ [0, 2^31) (F2,
// non-hardened upper bound, enforced by internal/hd).
//
// The hd.ErrSkipIndex BIP-32 edge case (IL == 0 or IL ≥ N — astronomically
// rare) is surfaced verbatim so a caller never silently uses a malformed
// delta; an off-range index is rejected the same way (hd.ErrIndexOutOfRange).
// Output is one JSON line a script can pipe; addresses are EIP-55 / Base58Check
// from the shared internal/addr (no separate code path, no parallel impl).
func addressOp(xpubJSON string, index uint32) (string, error) {
	var body xpubFetchResponseJSON
	if err := json.Unmarshal([]byte(xpubJSON), &body); err != nil {
		return "", fmt.Errorf("invalid xpub JSON: %w", err)
	}
	pubBytes, err := hex.DecodeString(body.ECDSAPubkeyHex)
	if err != nil {
		return "", fmt.Errorf("ecdsaPubkeyHex not hex: %w", err)
	}
	chaincode, err := hex.DecodeString(body.ChaincodeHex)
	if err != nil {
		return "", fmt.Errorf("chaincodeHex not hex: %w", err)
	}
	// btcec.ParsePubKey accepts both compressed (33B) and uncompressed (65B)
	// forms; the server returns whatever S-002 provisioned, so be liberal here
	// while staying strict downstream (addr.* needs uncompressed bytes).
	pk, err := btcec.ParsePubKey(pubBytes)
	if err != nil {
		return "", fmt.Errorf("ecdsaPubkey not a secp256k1 point: %w", err)
	}
	masterPub := pk.ToECDSA()

	il, childPub, err := hd.Derive(masterPub, chaincode, index)
	if err != nil {
		return "", fmt.Errorf("derive m/%d: %w", index, err)
	}
	childUncompressed := hd.ChildPubBytes(childPub)
	if childUncompressed == nil {
		return "", fmt.Errorf("derive m/%d: empty child public key", index)
	}
	eth, err := addr.ETHAddress(childUncompressed)
	if err != nil {
		return "", fmt.Errorf("ETHAddress: %w", err)
	}
	bsc, err := addr.BSCAddress(childUncompressed)
	if err != nil {
		return "", fmt.Errorf("BSCAddress: %w", err)
	}
	tron, err := addr.TronAddress(childUncompressed)
	if err != nil {
		return "", fmt.Errorf("TronAddress: %w", err)
	}
	out, err := json.Marshal(addrView{
		Index:          index,
		ECDSAPubkeyHex: body.ECDSAPubkeyHex,
		ChaincodeHex:   body.ChaincodeHex,
		ChildPubkeyHex: hex.EncodeToString(childUncompressed),
		ILHex:          hex.EncodeToString(il.Bytes()),
		EVMAddress:     eth,
		BSCAddress:     bsc,
		TronAddress:    tron,
	})
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	return string(out), nil
}

// xpubFetchResponseJSON mirrors the api.md B8 wire body (hex-encoded fields,
// owning-member-only). The shape is the shared contract — walletcli decodes
// it locally rather than re-exporting an mobileapi type, so the package's
// dependency surface is the SDK + the offline crypto helpers (hd / addr).
type xpubFetchResponseJSON struct {
	ECDSAPubkeyHex string `json:"ecdsaPubkeyHex"`
	ChaincodeHex   string `json:"chaincodeHex"`
}
