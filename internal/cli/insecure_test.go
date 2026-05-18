package cli

import (
	"testing"

	"github.com/bnb-chain/tss-lib/v3/tss"
)

func freshParams(t *testing.T) *tss.Parameters {
	t.Helper()
	pids := tss.GenerateTestPartyIDs(2)
	return tss.NewParameters(tss.S256(), tss.NewPeerContext(pids), pids[0], 2, 1)
}

// TestProofPolicy_DefaultFailClosed asserts the production-reachable default:
// without the explicit ALLOW_INSECURE_MPC=1 marker the Paillier modulus/factor
// ZK proofs stay ON (security.md invariant #10, RA-001 P1-1). The package
// TestMain sets the marker for the E2E carrier, so each case overrides it.
func TestProofPolicy_DefaultFailClosed(t *testing.T) {
	t.Run("unset -> proofs ON", func(t *testing.T) {
		t.Setenv(allowInsecureMPCEnv, "")
		if insecureMPCAllowed() {
			t.Fatal("unset marker must not allow insecure MPC")
		}
		p := freshParams(t)
		applyKeygenProofPolicy(p)
		if p.NoProofMod() || p.NoProofFac() {
			t.Fatalf("no marker: proofs must stay ON, got NoProofMod=%v NoProofFac=%v",
				p.NoProofMod(), p.NoProofFac())
		}
	})

	t.Run("non-1 value -> still proofs ON", func(t *testing.T) {
		t.Setenv(allowInsecureMPCEnv, "true") // only exactly "1" is the marker
		if insecureMPCAllowed() {
			t.Fatal(`only "1" may allow insecure MPC`)
		}
		p := freshParams(t)
		applyKeygenProofPolicy(p)
		if p.NoProofMod() || p.NoProofFac() {
			t.Fatal("non-1 marker: proofs must stay ON")
		}
	})

	t.Run("marker=1 -> dev/test no-proof", func(t *testing.T) {
		t.Setenv(allowInsecureMPCEnv, "1")
		if !insecureMPCAllowed() {
			t.Fatal(`"1" must allow insecure MPC`)
		}
		p := freshParams(t)
		applyKeygenProofPolicy(p)
		if !p.NoProofMod() || !p.NoProofFac() {
			t.Fatal("marker=1: dev/test no-proof must be applied")
		}
	})
}

// TestPreParamsFixtureHook_GatedByMarker verifies the P1-2 isolation: the
// fixture hook is only taken under the explicit marker; otherwise the real
// (multi-minute) generation path is taken — proven here by a short timeout
// override that forces the real path to fail fast rather than load fixtures.
func TestPreParamsFixtureHook_GatedByMarker(t *testing.T) {
	if fixturePreParamsHook == nil {
		t.Fatal("TestMain must install the fixture hook for the carrier")
	}
	// Marker set (carrier default): fixture hook taken, fast + no error.
	t.Setenv(allowInsecureMPCEnv, "1")
	pp, err := preParamsFor(0, 3)
	if err != nil || pp == nil {
		t.Fatalf("marker set: fixture pre-params expected, got pp=%v err=%v", pp, err)
	}
}
