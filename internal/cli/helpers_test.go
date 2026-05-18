package cli

import (
	"encoding/hex"
	"math/big"
	"os"
	"testing"

	btcec "github.com/btcsuite/btcd/btcec/v2"
)

func mkdir(p string) error { return os.MkdirAll(p, 0o750) }

// assertLowS verifies S lies in the lower half of the curve order — the
// canonical low-S form tss-lib's finalize round enforces (and the V recovery
// id is adjusted to match), which Ethereum/BSC require to reject malleability.
func assertLowS(t *testing.T, sHex string) {
	t.Helper()
	sb, err := hex.DecodeString(sHex)
	if err != nil {
		t.Fatalf("bad S hex: %v", err)
	}
	s := new(big.Int).SetBytes(sb)
	halfN := new(big.Int).Rsh(btcec.S256().Params().N, 1)
	if s.Cmp(halfN) > 0 {
		t.Fatalf("signature S is not low-S (malleable): S > N/2")
	}
}
