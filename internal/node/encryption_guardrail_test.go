package node

import (
	"errors"
	"testing"
)

// 独立加密专测 (e) — 生产铁律护栏(database.md §7.1/§7.2,与 E2E-001 解耦)。
// coord 启用且整库加密被禁用时:未经 ALLOW_INSECURE_DB=1 显式非生产确认
// → Validate fail-closed 返回 errInsecureDBNotConfirmed(node 启动即拒)。
// 默认 enable=true 不触发本护栏(由 TestValidateFullValid 等覆盖)。
func TestEncryptionDisableProductionGuardrail(t *testing.T) {
	// 各用例独立设置 ALLOW_INSECURE_DB(t.Setenv 自动还原)。DSN 经 env 注入,
	// 使「确认通过」用例能走到放行而非卡在缺 secret。
	coordCfg := func(encEnable bool) Config {
		return Config{Coord: CoordConfig{
			Enable:   true,
			DB:       CoordDBConfig{DSNRef: "env:TSSNODE_GUARD_DSN", Encryption: CoordDBEncryptionConfig{Enable: encEnable}},
			External: CoordExternalConfig{Auth: "mtls", ResultCallback: "webhook"},
			Quorum:   CoordQuorumConfig{SignerSelect: "liveness"},
		}}
	}

	t.Run("disabled without confirmation -> fail-closed", func(t *testing.T) {
		t.Setenv("TSSNODE_GUARD_DSN", "postgres://x")
		// ALLOW_INSECURE_DB 未设置(模拟生产/release 路径)。
		err := coordCfg(false).Validate()
		if !errors.Is(err, errInsecureDBNotConfirmed) {
			t.Fatalf("unconfirmed disable in prod: want errInsecureDBNotConfirmed, got %v", err)
		}
	})

	t.Run("disabled with wrong confirmation value -> still fail-closed", func(t *testing.T) {
		t.Setenv("TSSNODE_GUARD_DSN", "postgres://x")
		t.Setenv(allowInsecureDBEnv, "true") // 仅精确 "1" 视为确认
		err := coordCfg(false).Validate()
		if !errors.Is(err, errInsecureDBNotConfirmed) {
			t.Fatalf("non-\"1\" confirmation must not pass guardrail: got %v", err)
		}
	})

	t.Run("disabled with explicit non-prod confirmation -> allowed", func(t *testing.T) {
		t.Setenv("TSSNODE_GUARD_DSN", "postgres://x")
		t.Setenv(allowInsecureDBEnv, "1")
		if err := coordCfg(false).Validate(); err != nil {
			t.Fatalf("confirmed non-prod disable: want nil, got %v", err)
		}
	})

	t.Run("encryption enabled -> guardrail never triggers", func(t *testing.T) {
		t.Setenv("TSSNODE_GUARD_DSN", "postgres://x")
		// 即便误置确认变量,enable=true 路径与护栏无关。
		if err := coordCfg(true).Validate(); err != nil {
			t.Fatalf("encrypted config: want nil, got %v", err)
		}
	})

	t.Run("coord disabled -> guardrail not evaluated", func(t *testing.T) {
		// relay-only 节点;coord.enable=false 时 coord 配置不参与启动校验。
		t.Setenv("TSSNODE_GUARD_PSK", "psk")
		cfg := Config{Relay: RelayConfig{
			Enable:      true,
			PnetPSKRef:  "env:TSSNODE_GUARD_PSK",
			TokenVerify: TokenVerifyConfig{Source: "config"},
		}}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("relay-only with coord disabled: want nil, got %v", err)
		}
	})
}
