package node

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeConfig writes a temp config file and points NODE_CONFIG at it.
func writeConfig(t *testing.T, body string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "node.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("NODE_CONFIG", p)
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("NODE_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))
	// An explicitly-set but missing file must error (unlike a missing
	// default path).
	if _, err := Load(); err == nil {
		t.Fatal("explicit missing NODE_CONFIG: want error, got nil")
	}

	writeConfig(t, "relay: { enable: true }\n")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Built-in defaults remain where the file does not override.
	if cfg.Log.Level != "info" || cfg.Log.Format != "json" {
		t.Errorf("log defaults lost: %+v", cfg.Log)
	}
	if cfg.Metrics.Listen != ":9090" {
		t.Errorf("metrics default lost: %q", cfg.Metrics.Listen)
	}
	if cfg.Coord.HTTP.Listen != ":8080" || cfg.Coord.External.Auth != "mtls" {
		t.Errorf("coord defaults lost: %+v", cfg.Coord)
	}
	if cfg.Relay.TokenVerify.Source != "config" || !cfg.Relay.Rendezvous.Enable {
		t.Errorf("relay defaults lost: %+v", cfg.Relay)
	}
}

func TestPrecedenceDefaultFileEnv(t *testing.T) {
	writeConfig(t, "log: { level: warn }\nrelay: { enable: true }\n")
	t.Setenv("TSSNODE_LOG__LEVEL", "error")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Log.Format != "json" {
		t.Errorf("default not kept: log.format=%q want json", cfg.Log.Format)
	}
	if !cfg.Relay.Enable {
		t.Error("file did not override default: relay.enable should be true")
	}
	if cfg.Log.Level != "error" {
		t.Errorf("env did not override file: log.level=%q want error", cfg.Log.Level)
	}
}

func TestEnvNestedOverride(t *testing.T) {
	writeConfig(t, "coord: { enable: true }\n")
	t.Setenv("TSSNODE_RELAY__ENABLE", "true")
	t.Setenv("TSSNODE_COORD__HTTP__LISTEN", ":18080")
	t.Setenv("TSSNODE_RELAY__LISTEN", "/ip4/0.0.0.0/tcp/1, /ip4/0.0.0.0/tcp/2")
	t.Setenv("TSSNODE_RELAY__LIMITS__RESERVATION_PER_TOKEN", "9")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Relay.Enable {
		t.Error("TSSNODE_RELAY__ENABLE not applied")
	}
	if cfg.Coord.HTTP.Listen != ":18080" {
		t.Errorf("nested http listen: %q", cfg.Coord.HTTP.Listen)
	}
	if len(cfg.Relay.Listen) != 2 || cfg.Relay.Listen[1] != "/ip4/0.0.0.0/tcp/2" {
		t.Errorf("slice env override: %v", cfg.Relay.Listen)
	}
	if cfg.Relay.Limits.ReservationPerToken != 9 {
		t.Errorf("int env override: %d", cfg.Relay.Limits.ReservationPerToken)
	}
}

func TestValidateDoubleFalse(t *testing.T) {
	if err := (Config{}).Validate(); !errors.Is(err, ErrNoRoleEnabled) {
		t.Fatalf("both false: want ErrNoRoleEnabled, got %v", err)
	}
}

func TestValidateRequiredSecretFailFast(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{
			"relay pnet_psk missing",
			Config{Relay: RelayConfig{
				Enable:      true,
				PnetPSKRef:  "env:TSSNODE_TEST_UNSET",
				TokenVerify: TokenVerifyConfig{Source: "config"},
			}},
		},
		{
			"coord db.dsn missing",
			Config{Coord: CoordConfig{
				Enable:   true,
				DB:       CoordDBConfig{DSNRef: "env:TSSNODE_TEST_UNSET", Encryption: CoordDBEncryptionConfig{Enable: true}},
				External: CoordExternalConfig{Auth: "mtls", ResultCallback: "webhook"},
				Quorum:   CoordQuorumConfig{SignerSelect: "liveness"},
			}},
		},
		{
			"coord api_key missing when auth=api_key",
			Config{Coord: CoordConfig{
				Enable:   true,
				DB:       CoordDBConfig{DSNRef: "env:TSSNODE_TEST_DSN", Encryption: CoordDBEncryptionConfig{Enable: true}},
				External: CoordExternalConfig{Auth: "api_key", APIKeyRef: "env:TSSNODE_TEST_UNSET", ResultCallback: "webhook"},
				Quorum:   CoordQuorumConfig{SignerSelect: "liveness"},
			}},
		},
	}
	t.Setenv("TSSNODE_TEST_DSN", "postgres://x")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); err == nil {
				t.Fatal("want fail-fast error, got nil")
			}
		})
	}
}

func TestValidatePlaintextSecretRejected(t *testing.T) {
	cfg := Config{Relay: RelayConfig{
		Enable:      true,
		PnetPSKRef:  "rawplaintextsecret",
		TokenVerify: TokenVerifyConfig{Source: "config"},
	}}
	err := cfg.Validate()
	if !errors.Is(err, errSecretPlaintext) {
		t.Fatalf("plaintext secret: want errSecretPlaintext, got %v", err)
	}
}

func TestValidateEnum(t *testing.T) {
	t.Setenv("TSSNODE_TEST_PSK", "x")
	t.Setenv("TSSNODE_TEST_DSN", "y")
	bad := []struct {
		name string
		cfg  Config
	}{
		{"relay source", Config{Relay: RelayConfig{
			Enable: true, PnetPSKRef: "env:TSSNODE_TEST_PSK",
			TokenVerify: TokenVerifyConfig{Source: "bogus"},
		}}},
		{"coord auth", Config{Coord: CoordConfig{
			Enable: true, DB: CoordDBConfig{DSNRef: "env:TSSNODE_TEST_DSN", Encryption: CoordDBEncryptionConfig{Enable: true}},
			External: CoordExternalConfig{Auth: "bogus", ResultCallback: "webhook"},
			Quorum:   CoordQuorumConfig{SignerSelect: "liveness"},
		}}},
		{"coord result_callback", Config{Coord: CoordConfig{
			Enable: true, DB: CoordDBConfig{DSNRef: "env:TSSNODE_TEST_DSN", Encryption: CoordDBEncryptionConfig{Enable: true}},
			External: CoordExternalConfig{Auth: "mtls", ResultCallback: "bogus"},
			Quorum:   CoordQuorumConfig{SignerSelect: "liveness"},
		}}},
		{"coord signer_select", Config{Coord: CoordConfig{
			Enable: true, DB: CoordDBConfig{DSNRef: "env:TSSNODE_TEST_DSN", Encryption: CoordDBEncryptionConfig{Enable: true}},
			External: CoordExternalConfig{Auth: "mtls", ResultCallback: "webhook"},
			Quorum:   CoordQuorumConfig{SignerSelect: "bogus"},
		}}},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); err == nil {
				t.Fatal("want enum error, got nil")
			}
		})
	}
}

func TestResolveSecret(t *testing.T) {
	t.Setenv("TSSNODE_SECRET_OK", "s3cret")
	f := filepath.Join(t.TempDir(), "psk")
	if err := os.WriteFile(f, []byte("  filesecret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if v, err := resolveSecret("env:TSSNODE_SECRET_OK"); err != nil || v != "s3cret" {
		t.Errorf("env ref: v=%q err=%v", v, err)
	}
	if v, err := resolveSecret("file:" + f); err != nil || v != "filesecret" {
		t.Errorf("file ref: v=%q err=%v", v, err)
	}
	if _, err := resolveSecret(""); !errors.Is(err, errSecretMissing) {
		t.Errorf("empty: want errSecretMissing, got %v", err)
	}
	if _, err := resolveSecret("env:TSSNODE_DEFINITELY_UNSET"); !errors.Is(err, errSecretMissing) {
		t.Errorf("unset env: want errSecretMissing, got %v", err)
	}
	if _, err := resolveSecret("plaintext"); !errors.Is(err, errSecretPlaintext) {
		t.Errorf("plaintext: want errSecretPlaintext, got %v", err)
	}
	if _, err := resolveSecret("file:/no/such/path"); err == nil {
		t.Error("missing file: want error, got nil")
	}
}

func TestOptionalSecret(t *testing.T) {
	t.Setenv("TSSNODE_OPT_OK", "v")
	if err := optionalSecret(""); err != nil {
		t.Errorf("empty optional: want nil, got %v", err)
	}
	if err := optionalSecret("env:TSSNODE_OPT_OK"); err != nil {
		t.Errorf("set optional: want nil, got %v", err)
	}
	if err := optionalSecret("plainpush"); !errors.Is(err, errSecretPlaintext) {
		t.Errorf("plaintext optional: want errSecretPlaintext, got %v", err)
	}
}

// TestFullParamTableParse uses the server.md config-chapter sample YAML
// to verify every parameter-table item lands.
func TestFullParamTableParse(t *testing.T) {
	writeConfig(t, `
log:   { level: info, format: json }
metrics: { listen: ":9090" }

relay:
  enable: true
  listen: ["/ip4/0.0.0.0/tcp/4001"]
  pnet_psk_ref: env:TSSNODE_RELAY__PNET_PSK
  token_verify:
    source: config
    group_pubkeys: ["pk1", "pk2"]
  rendezvous: { enable: true }
  limits:
    reservation_per_token: 4
    reservation_per_group: 8
    bandwidth_per_conn: "1MiB/s"

coord:
  enable: true
  http: { listen: ":8080" }
  db:   { dsn_ref: env:TSSNODE_COORD__DB_DSN }
  external:
    auth: mtls
    api_key_ref: env:TSSNODE_COORD__EXTERNAL__API_KEY
    result_callback: webhook
  push:
    fcm_cred_ref: env:TSSNODE_COORD__PUSH__FCM
    apns_cred_ref: env:TSSNODE_COORD__PUSH__APNS
  ttl: { skew_tolerance: "30s" }
  quorum: { signer_select: liveness }
  dispatch: { timeout: "120s" }
`)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	checks := map[string]bool{
		"log.level":                          cfg.Log.Level == "info",
		"log.format":                         cfg.Log.Format == "json",
		"metrics.listen":                     cfg.Metrics.Listen == ":9090",
		"relay.enable":                       cfg.Relay.Enable,
		"relay.listen":                       len(cfg.Relay.Listen) == 1 && cfg.Relay.Listen[0] == "/ip4/0.0.0.0/tcp/4001",
		"relay.pnet_psk_ref":                 cfg.Relay.PnetPSKRef == "env:TSSNODE_RELAY__PNET_PSK",
		"relay.token_verify.source":          cfg.Relay.TokenVerify.Source == "config",
		"relay.token_verify.group_pubkeys":   len(cfg.Relay.TokenVerify.GroupPubkeys) == 2,
		"relay.rendezvous.enable":            cfg.Relay.Rendezvous.Enable,
		"relay.limits.reservation_per_token": cfg.Relay.Limits.ReservationPerToken == 4,
		"relay.limits.reservation_per_group": cfg.Relay.Limits.ReservationPerGroup == 8,
		"relay.limits.bandwidth_per_conn":    cfg.Relay.Limits.BandwidthPerConn == "1MiB/s",
		"relay.limits.circuit_max_duration":  cfg.Relay.Limits.CircuitMaxDuration == "10m",
		"coord.enable":                       cfg.Coord.Enable,
		"coord.http.listen":                  cfg.Coord.HTTP.Listen == ":8080",
		"coord.db.dsn_ref":                   cfg.Coord.DB.DSNRef == "env:TSSNODE_COORD__DB_DSN",
		"coord.external.auth":                cfg.Coord.External.Auth == "mtls",
		"coord.external.api_key_ref":         cfg.Coord.External.APIKeyRef == "env:TSSNODE_COORD__EXTERNAL__API_KEY",
		"coord.external.result_callback":     cfg.Coord.External.ResultCallback == "webhook",
		"coord.push.fcm_cred_ref":            cfg.Coord.Push.FCMCredRef == "env:TSSNODE_COORD__PUSH__FCM",
		"coord.push.apns_cred_ref":           cfg.Coord.Push.APNSCredRef == "env:TSSNODE_COORD__PUSH__APNS",
		"coord.ttl.skew_tolerance":           cfg.Coord.TTL.SkewTolerance == "30s",
		"coord.quorum.signer_select":         cfg.Coord.Quorum.SignerSelect == "liveness",
		"coord.dispatch.timeout":             cfg.Coord.Dispatch.Timeout == "120s",
	}
	for k, ok := range checks {
		if !ok {
			t.Errorf("param table mismatch: %s", k)
		}
	}
}

// TestValidateFullValid injects all required/optional secrets and
// expects overall pass.
func TestValidateFullValid(t *testing.T) {
	t.Setenv("TSSNODE_RELAY__PNET_PSK", "psk")
	t.Setenv("TSSNODE_COORD__DB_DSN", "postgres://x")
	t.Setenv("TSSNODE_COORD__PUSH__FCM", "fcm")
	t.Setenv("TSSNODE_COORD__PUSH__APNS", "apns")
	cfg := Config{
		Relay: RelayConfig{
			Enable:      true,
			PnetPSKRef:  "env:TSSNODE_RELAY__PNET_PSK",
			TokenVerify: TokenVerifyConfig{Source: "config"},
		},
		Coord: CoordConfig{
			Enable:   true,
			DB:       CoordDBConfig{DSNRef: "env:TSSNODE_COORD__DB_DSN", Encryption: CoordDBEncryptionConfig{Enable: true}},
			External: CoordExternalConfig{Auth: "mtls", ResultCallback: "webhook"},
			Push:     CoordPushConfig{FCMCredRef: "env:TSSNODE_COORD__PUSH__FCM", APNSCredRef: "env:TSSNODE_COORD__PUSH__APNS"},
			Quorum:   CoordQuorumConfig{SignerSelect: "liveness"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("full valid config: want nil, got %v", err)
	}
}
