package cli

import (
	"fmt"
	"os"

	"github.com/bnb-chain/tss-lib/v3/tss"
)

// allowInsecureMPCEnv is the explicit non-production marker that must be set to
// exactly "1" before a NETWORKED keygen/resharing party may skip tss-lib's
// Paillier modulus/factor ZK proofs (security.md invariant #10, RA-001 P1-1;
// same fail-closed discipline as #9 / FIX-003's ALLOW_INSECURE_DB).
//
// The GG18/GG20 modulus/factor proofs are the core defence against a malicious
// member that injects a crafted Paillier public key, so they are NOT optional
// on any production networked path. tss-lib's no-proof mode exists only to let
// the dev/test E2E carrier finish a 3-party keygen inside the relay's
// circuit-v2 Duration cap — it must never be reachable in the shipped binary.
//
// Fail-closed by construction: the proofs are ON unless this env is explicitly
// "1". cmd/cli never sets it, so a production/release/CI networked keygen
// always runs with proofs; there is no "proofs off without an explicit
// non-production marker" reachable state (the marker is the only off-switch).
const allowInsecureMPCEnv = "ALLOW_INSECURE_MPC"

// insecureMPCAllowed reports whether the explicit non-production marker is set.
// Anything other than exactly "1" (including unset) keeps the secure default.
func insecureMPCAllowed() bool {
	return os.Getenv(allowInsecureMPCEnv) == "1"
}

// applyKeygenProofPolicy is the single decision point for the Paillier
// modulus/factor ZK proofs on a networked keygen/resharing party. Production
// default: leave the proofs ON (do NOT call SetNoProof*). Only when the
// explicit non-production marker is set does it drop into tss-lib's no-proof
// test mode, and it announces that loudly on stderr so an insecure run is
// never silent (security.md #10: no-proof must be explicitly gated, prod
// fail-closed).
func applyKeygenProofPolicy(params *tss.Parameters) {
	if !insecureMPCAllowed() {
		return // proofs ON — the only production-reachable path
	}
	fmt.Fprintf(os.Stderr,
		"cli: WARNING %s=1 — Paillier modulus/factor ZK proofs DISABLED "+
			"(tss-lib no-proof test mode); DEV/TEST ONLY, never production "+
			"(security.md invariant #10)\n", allowInsecureMPCEnv)
	params.SetNoProofMod()
	params.SetNoProofFac()
}
