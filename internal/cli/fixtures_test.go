package cli

import (
	"fmt"
	"os"
	"testing"

	"github.com/bnb-chain/tss-lib/v3/ecdsa/keygen"
)

// TestMain confines the dev/test fast path to the test binary (RA-001 P1-2):
// tss-lib's LoadKeygenTestFixtures is test-utility code and is referenced ONLY
// here, never from a compilable product path, so cmd/cli always generates real
// local pre-params. It also sets the explicit non-production marker so the
// E2E carrier may skip the multi-minute safe-prime search AND tss-lib's
// Paillier proofs under the binding `go test -race ./...` gate — the same
// marker production never sets (security.md invariant #10, fail-closed).
func TestMain(m *testing.M) {
	_ = os.Setenv(allowInsecureMPCEnv, "1")
	fixturePreParamsHook = func(index, n int) (*keygen.LocalPreParams, error) {
		fx, _, err := keygen.LoadKeygenTestFixtures(n)
		if err != nil {
			return nil, fmt.Errorf("cli: load keygen fixtures: %w", err)
		}
		if index >= len(fx) {
			return nil, fmt.Errorf("cli: no fixture for index %d", index)
		}
		pp := fx[index].LocalPreParams
		return &pp, nil
	}
	os.Exit(m.Run())
}
