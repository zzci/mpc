package server

import (
	"errors"
	"testing"
)

// Standalone encryption test (e) — production iron-law guardrail
// (database.md §7.1/§7.2, decoupled from E2E-001). When coord is enabled
// and whole-DB encryption is disabled: without an explicit
// ALLOW_INSECURE_DB=1 non-production confirmation, Validate fail-closes
// returning errInsecureDBNotConfirmed (node refuses to start). The
// default enable=true does not trigger this guardrail (covered by
// TestValidateFullValid etc.).
func TestEncryptionDisableProductionGuardrail(t *testing.T) {
	// Each case sets ALLOW_INSECURE_DB independently (t.Setenv auto-
	// restores). DSN is injected via env so a "confirmed" case reaches
	// the allow path rather than stalling on a missing secret.
	coordCfg := func(encEnable bool) Config {
		return Config{Coord: CoordConfig{
			Enable:   true,
			DB:       CoordDBConfig{DSN: "env:REF_GUARD_DSN", Encryption: CoordDBEncryptionConfig{Enable: encEnable}},
			External: CoordExternalConfig{APIKey: "env:REF_GUARD_KEY", ResultWebhook: "env:REF_GUARD_RW"},
			Notify:   CoordNotifyConfig{Webhook: "env:REF_GUARD_RW"},
			Quorum:   CoordQuorumConfig{SignerSelect: "liveness"},
		}}
	}

	t.Run("disabled without confirmation -> fail-closed", func(t *testing.T) {
		t.Setenv("REF_GUARD_DSN", "postgres://x")
		// ALLOW_INSECURE_DB not set (simulates the production/release path).
		err := coordCfg(false).Validate()
		if !errors.Is(err, errInsecureDBNotConfirmed) {
			t.Fatalf("unconfirmed disable in prod: want errInsecureDBNotConfirmed, got %v", err)
		}
	})

	t.Run("disabled with wrong confirmation value -> still fail-closed", func(t *testing.T) {
		t.Setenv("REF_GUARD_DSN", "postgres://x")
		t.Setenv(allowInsecureDBEnv, "true") // only exact "1" counts as confirmation
		err := coordCfg(false).Validate()
		if !errors.Is(err, errInsecureDBNotConfirmed) {
			t.Fatalf("non-\"1\" confirmation must not pass guardrail: got %v", err)
		}
	})

	t.Run("disabled with explicit non-prod confirmation -> allowed", func(t *testing.T) {
		t.Setenv("REF_GUARD_DSN", "postgres://x")
		t.Setenv("REF_GUARD_KEY", "k")
		t.Setenv("REF_GUARD_RW", "https://rw")
		t.Setenv(allowInsecureDBEnv, "1")
		if err := coordCfg(false).Validate(); err != nil {
			t.Fatalf("confirmed non-prod disable: want nil, got %v", err)
		}
	})

	t.Run("encryption enabled -> guardrail never triggers", func(t *testing.T) {
		t.Setenv("REF_GUARD_DSN", "postgres://x")
		t.Setenv("REF_GUARD_KEY", "k")
		t.Setenv("REF_GUARD_RW", "https://rw")
		// Even if the confirm var is mis-set, the enable=true path is
		// independent of the guardrail.
		if err := coordCfg(true).Validate(); err != nil {
			t.Fatalf("encrypted config: want nil, got %v", err)
		}
	})

	t.Run("coord disabled -> guardrail not evaluated", func(t *testing.T) {
		// relay-only node; with coord.enable=false the coord config does
		// not participate in startup validation.
		t.Setenv("REF_GUARD_PSK", "psk")
		cfg := Config{Relay: RelayConfig{
			Enable:      true,
			PnetPSK:     "env:REF_GUARD_PSK",
			TokenVerify: TokenVerifyConfig{Source: "config"},
		}}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("relay-only with coord disabled: want nil, got %v", err)
		}
	})
}
