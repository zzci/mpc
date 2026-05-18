package mpc

import (
	"context"
	"fmt"
	"time"

	"github.com/bnb-chain/tss-lib/v3/ecdsa/keygen"
)

// RecoverConfig parameterizes a lost-member recovery resharing
// (docs/design/mcp/sdk.md §7). When a device or share is lost, the surviving
// members re-run resharing to a fresh committee that restores the original
// redundancy: the same (t, n), the same master public key, and therefore every
// derived chain address unchanged. The lost share is not reconstructed in
// place — resharing draws a fresh polynomial, so all old shares (including any
// copies an attacker may hold of the lost one) are invalidated.
type RecoverConfig struct {
	// SurvivingShares are the shares still held after the loss. They must
	// number at least Threshold+1 — fewer than t+1 cannot reconstruct the
	// key, so the wallet is unrecoverable by design (no backend escrow). Order
	// is irrelevant (the committee is reconstructed from embedded ShareIDs).
	SurvivingShares []Share

	// Threshold is the original t (t-of-n: any t+1 shares can sign). It is
	// preserved across recovery so the wallet's security parameter does not
	// silently change.
	Threshold int

	// Parties is the committee size to restore — normally the original n, so
	// the pre-loss redundancy (n−t spare shares) is rebuilt.
	Parties int

	// PreParams optionally supplies pre-computed Paillier/safe-prime
	// parameters for the rebuilt committee (len must equal Parties). See
	// ReshareConfig.PreParams — the same RED LINE custody invariant applies:
	// in production these MUST be generated locally on each participant's own
	// device, never pre-generated or pushed by a backend.
	PreParams []keygen.LocalPreParams

	// PreParamTimeout bounds internal generation when PreParams is nil.
	// Zero means defaultPreParamTimeout.
	PreParamTimeout time.Duration
}

// RecoverLostMember restores a wallet whose committee lost one or more members.
// It is an intent-revealing wrapper over Reshare: the surviving committee
// (≥ Threshold+1 shares) reshares onto a fresh committee of Parties shares at
// the same Threshold. The master public key — and every ETH/BSC/TRON address
// derived from it — is invariant; Reshare asserts this internally and fails
// closed if it is ever violated (docs/design/mcp/sdk.md §7, resharing.go).
//
// It adds the recovery-specific precondition check so the unrecoverable case
// (fewer than t+1 surviving shares) fails with a clear, custody-accurate
// message rather than a generic resharing parameter error.
func RecoverLostMember(ctx context.Context, cfg RecoverConfig) ([]Share, error) {
	if cfg.Threshold < 1 {
		return nil, fmt.Errorf("recover: threshold must be >= 1, got %d", cfg.Threshold)
	}
	if cfg.Parties <= cfg.Threshold {
		return nil, fmt.Errorf(
			"recover: parties must exceed threshold (t=%d n=%d); a degenerate committee defeats redundancy",
			cfg.Threshold, cfg.Parties)
	}
	if len(cfg.SurvivingShares) < cfg.Threshold+1 {
		return nil, fmt.Errorf(
			"recover: %d surviving shares < threshold+1 (%d); the key is unrecoverable (no backend escrow by design)",
			len(cfg.SurvivingShares), cfg.Threshold+1)
	}
	return Reshare(ctx, ReshareConfig{
		OldThreshold:    cfg.Threshold,
		OldShares:       cfg.SurvivingShares,
		NewThreshold:    cfg.Threshold,
		NewParties:      cfg.Parties,
		PreParams:       cfg.PreParams,
		PreParamTimeout: cfg.PreParamTimeout,
	})
}
