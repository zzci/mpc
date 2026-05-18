package cli

import (
	"fmt"

	btcec "github.com/btcsuite/btcd/btcec/v2"

	"github.com/royqta/mcp-wallet/internal/mpc"
)

// groupPubUncompressed extracts the wallet master public key (uncompressed
// secp256k1, 65 bytes) from a serialized share, validated on-curve. Every
// device's keygen/reshare share must yield the identical key — that equality,
// plus ecrecover over the signature, is the carrier's correctness anchor.
func groupPubUncompressed(sh mpc.Share) ([]byte, error) {
	sd, err := mpc.UnmarshalSaveData(sh.SaveData)
	if err != nil {
		return nil, fmt.Errorf("cli: load share: %w", err)
	}
	if sd.ECDSAPub == nil {
		return nil, fmt.Errorf("cli: share has no ECDSAPub")
	}
	raw := make([]byte, 65)
	raw[0] = 0x04
	sd.ECDSAPub.X().FillBytes(raw[1:33])
	sd.ECDSAPub.Y().FillBytes(raw[33:65])
	pk, err := btcec.ParsePubKey(raw)
	if err != nil {
		return nil, fmt.Errorf("cli: master pubkey not on curve: %w", err)
	}
	return pk.SerializeUncompressed(), nil
}
