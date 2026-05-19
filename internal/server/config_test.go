package server

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Reference env names used by literal-or-ref tests. They are deliberately
// NOT of the form MPC_<schema key> so the schema-driven env layer never
// matches them — the test controls exactly one source at a time.
const (
	refPSK = "REF_RELAY_PSK"
	refDSN = "REF_COORD_DSN"
	refKey = "REF_COORD_KEY"
	refRW  = "REF_COORD_RESULT_WEBHOOK"
	refNW  = "REF_COORD_NOTIFY_WEBHOOK"
)

// writeConfig writes a temp config file and points SERVER_CONFIG at it.
func writeConfig(t *testing.T, body string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "server.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("SERVER_CONFIG", p)
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("SERVER_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))
	// An explicitly-set but missing file must error (unlike a missing
	// default path).
	if _, err := Load(); err == nil {
		t.Fatal("explicit missing SERVER_CONFIG: want error, got nil")
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
	if cfg.Coord.HTTP.Listen != ":8080" {
		t.Errorf("coord defaults lost: %+v", cfg.Coord)
	}
	if !cfg.Relay.Rendezvous.Enable {
		t.Errorf("relay defaults lost: %+v", cfg.Relay)
	}
}

func TestPrecedenceDefaultFileEnv(t *testing.T) {
	writeConfig(t, "log: { level: warn }\nrelay: { enable: true }\n")
	t.Setenv("MPC_LOG_LEVEL", "error")

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

// TestPrecedenceCLIWins exercises the Traefik-style fourth source: a CLI
// flag beats env, which beats file, which beats the built-in default
// (user ruling 2026-05-19).
func TestPrecedenceCLIWins(t *testing.T) {
	writeConfig(t, "log: { level: warn }\ncoord: { http: { listen: \":1111\" } }\nrelay: { enable: true }\n")
	t.Setenv("MPC_LOG_LEVEL", "error")

	cfg, err := loadFrom([]string{
		"--log.level=debug",
		"--coord.http.listen=:2222",
		"--relay.enable=true",
	})
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("CLI did not beat env: log.level=%q want debug", cfg.Log.Level)
	}
	if cfg.Coord.HTTP.Listen != ":2222" {
		t.Errorf("CLI did not beat file: coord.http.listen=%q want :2222", cfg.Coord.HTTP.Listen)
	}
	if !cfg.Relay.Enable {
		t.Error("CLI bool flag not applied")
	}
}

// TestCLIConfigFlag: --config selects the file (and outranks SERVER_CONFIG
// for the path); --config=path and --config path both work.
func TestCLIConfigFlag(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cli.yaml")
	if err := os.WriteFile(p, []byte("log: { level: warn }\nrelay: { enable: true }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// SERVER_CONFIG points elsewhere (missing) — CLI --config must win.
	t.Setenv("SERVER_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))

	for _, args := range [][]string{
		{"--config", p},
		{"--config=" + p},
	} {
		cfg, err := loadFrom(args)
		if err != nil {
			t.Fatalf("loadFrom %v: %v", args, err)
		}
		if cfg.Log.Level != "warn" || !cfg.Relay.Enable {
			t.Errorf("--config not honored for %v: %+v", args, cfg.Log)
		}
	}
}

func TestCLIMalformedFlag(t *testing.T) {
	writeConfig(t, "relay: { enable: true }\n")
	if _, err := loadFrom([]string{"--log.level"}); err == nil {
		t.Fatal("missing =value: want error, got nil")
	}
	if _, err := loadFrom([]string{"--no.such.key=x"}); err == nil {
		t.Fatal("unknown key: want error, got nil")
	}
	// Single-dash (go test) flags are ignored, not parsed.
	if _, err := loadFrom([]string{"-test.v", "-test.run=X"}); err != nil {
		t.Fatalf("single-dash args must be ignored: %v", err)
	}
}

// TestEnvSchemaDrivenMatch verifies the env layer GENERATES MPC_<UPPER
// dotted key> (segments joined by single '_') and exact-matches — no
// env-name parsing. Keys with an internal '_' (pnet_psk,
// reservation_per_token) land correctly because generation is from the
// schema, not by splitting the env name.
func TestEnvSchemaDrivenMatch(t *testing.T) {
	writeConfig(t, "coord: { enable: true }\n")
	t.Setenv("MPC_RELAY_ENABLE", "true")
	t.Setenv("MPC_COORD_HTTP_LISTEN", ":18080")
	t.Setenv("MPC_RELAY_LISTEN", "/ip4/0.0.0.0/tcp/1, /ip4/0.0.0.0/tcp/2")
	t.Setenv("MPC_RELAY_LIMITS_RESERVATION_PER_TOKEN", "9")
	t.Setenv("MPC_RELAY_PNET_PSK", "from-env-psk")
	t.Setenv("MPC_COORD_TTL_SKEW_TOLERANCE", "45s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Relay.Enable {
		t.Error("MPC_RELAY_ENABLE not applied")
	}
	if cfg.Coord.HTTP.Listen != ":18080" {
		t.Errorf("nested http listen: %q", cfg.Coord.HTTP.Listen)
	}
	if len(cfg.Relay.Listen) != 2 || cfg.Relay.Listen[1] != "/ip4/0.0.0.0/tcp/2" {
		t.Errorf("slice env override: %v", cfg.Relay.Listen)
	}
	if cfg.Relay.Limits.ReservationPerToken != 9 {
		t.Errorf("internal-underscore key (reservation_per_token): %d", cfg.Relay.Limits.ReservationPerToken)
	}
	if cfg.Relay.PnetPSK != "from-env-psk" {
		t.Errorf("internal-underscore key (pnet_psk): %q", cfg.Relay.PnetPSK)
	}
	if cfg.Coord.TTL.SkewTolerance != "45s" {
		t.Errorf("internal-underscore key (skew_tolerance): %q", cfg.Coord.TTL.SkewTolerance)
	}
}

func TestValidateDoubleFalse(t *testing.T) {
	if err := (Config{}).Validate(); !errors.Is(err, ErrNoRoleEnabled) {
		t.Fatalf("both false: want ErrNoRoleEnabled, got %v", err)
	}
}

// fullCoord builds a coord config whose required values are env: refs to
// the non-schema REF_* names (set via t.Setenv by the caller). rw/nw are
// the result/notify webhook URLs; callback auth is satisfied with a
// literal api_key so the at-least-one rule passes (its own failure mode is
// covered by TestValidateCallbackAuthRequired).
func fullCoord(dsn, key, rw, nw string) CoordConfig {
	return CoordConfig{
		Enable: true,
		DB:     CoordDBConfig{DSN: dsn, Encryption: CoordDBEncryptionConfig{Enable: true}},
		External: CoordExternalConfig{
			APIKey: key,
			Result: OutboundWebhookConfig{URL: rw, APIKey: "result-bearer-literal"},
		},
		Notify: CoordNotifyConfig{URL: nw, APIKey: "notify-bearer-literal"},
		Quorum: CoordQuorumConfig{SignerSelect: "liveness"},
	}
}

func TestValidateRequiredValueFailFast(t *testing.T) {
	t.Setenv(refDSN, "postgres://x")
	t.Setenv(refKey, "k")
	t.Setenv(refRW, "https://rw")
	t.Setenv(refNW, "https://nw")
	cases := []struct {
		name string
		cfg  Config
	}{
		{
			"relay pnet_psk missing",
			Config{Relay: RelayConfig{
				Enable:  true,
				PnetPSK: "env:REF_UNSET",
			}},
		},
		{
			"coord db.dsn missing",
			Config{Coord: fullCoord("env:REF_UNSET", "env:"+refKey, "env:"+refRW, "env:"+refNW)},
		},
		{
			"coord external.api_key missing",
			Config{Coord: fullCoord("env:"+refDSN, "env:REF_UNSET", "env:"+refRW, "env:"+refNW)},
		},
		{
			"coord external.result.url missing",
			Config{Coord: fullCoord("env:"+refDSN, "env:"+refKey, "", "env:"+refNW)},
		},
		{
			"coord notify.url missing",
			Config{Coord: fullCoord("env:"+refDSN, "env:"+refKey, "env:"+refRW, "")},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); err == nil {
				t.Fatal("want fail-fast error, got nil")
			}
		})
	}
}

// TestValidateCallbackAuthRequired exercises the anti-forgery callback-auth
// rule (user ruling 2026-05-19): a coord→external webhook with a url but
// neither secret nor api_key fail-fasts; either mode alone (or both) passes.
func TestValidateCallbackAuthRequired(t *testing.T) {
	t.Setenv(refDSN, "postgres://x")
	t.Setenv(refKey, "k")

	base := func() Config {
		return Config{Coord: CoordConfig{
			Enable: true,
			DB:     CoordDBConfig{DSN: "env:" + refDSN, Encryption: CoordDBEncryptionConfig{Enable: true}},
			External: CoordExternalConfig{
				APIKey: "env:" + refKey,
				Result: OutboundWebhookConfig{URL: "https://ext/result", APIKey: "rk"},
			},
			Notify: CoordNotifyConfig{URL: "https://ext/notify", APIKey: "nk"},
			Quorum: CoordQuorumConfig{SignerSelect: "liveness"},
		}}
	}

	t.Run("result neither secret nor api_key -> fail-fast", func(t *testing.T) {
		c := base()
		c.Coord.External.Result = OutboundWebhookConfig{URL: "https://ext/result"}
		if err := c.Validate(); !errors.Is(err, errWebhookAuthNeitherSet) {
			t.Fatalf("want errWebhookAuthNeitherSet, got %v", err)
		}
	})
	t.Run("notify neither secret nor api_key -> fail-fast", func(t *testing.T) {
		c := base()
		c.Coord.Notify = CoordNotifyConfig{URL: "https://ext/notify"}
		if err := c.Validate(); !errors.Is(err, errWebhookAuthNeitherSet) {
			t.Fatalf("want errWebhookAuthNeitherSet, got %v", err)
		}
	})
	t.Run("secret-only passes", func(t *testing.T) {
		c := base()
		c.Coord.External.Result = OutboundWebhookConfig{URL: "https://ext/result", Secret: "s"}
		c.Coord.Notify = CoordNotifyConfig{URL: "https://ext/notify", Secret: "s"}
		if err := c.Validate(); err != nil {
			t.Fatalf("secret-only: want nil, got %v", err)
		}
	})
	t.Run("api_key-only and both-set pass", func(t *testing.T) {
		c := base()
		c.Coord.External.Result = OutboundWebhookConfig{URL: "https://ext/result", Secret: "s", APIKey: "k"}
		if err := c.Validate(); err != nil {
			t.Fatalf("both-set: want nil, got %v", err)
		}
	})
}

// TestValidateLiteralAccepted: the v2 ruling relaxes the
// secret-must-be-a-reference rule — a plaintext literal is a valid value.
func TestValidateLiteralAccepted(t *testing.T) {
	cfg := Config{Relay: RelayConfig{
		Enable:  true,
		PnetPSK: "a-literal-psk-value",
	}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("literal value: want nil, got %v", err)
	}
}

func TestValidateEnum(t *testing.T) {
	t.Setenv(refPSK, "x")
	t.Setenv(refDSN, "y")
	t.Setenv(refKey, "k")
	t.Setenv(refRW, "https://rw")
	t.Setenv(refNW, "https://nw")
	bad := []struct {
		name string
		cfg  Config
	}{
		{"coord signer_select", func() Config {
			c := fullCoord("env:"+refDSN, "env:"+refKey, "env:"+refRW, "env:"+refNW)
			c.Quorum.SignerSelect = "bogus"
			return Config{Coord: c}
		}()},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); err == nil {
				t.Fatal("want enum error, got nil")
			}
		})
	}
}

func TestResolveValue(t *testing.T) {
	t.Setenv("REF_VAL_OK", "s3cret")
	f := filepath.Join(t.TempDir(), "v")
	if err := os.WriteFile(f, []byte("  filevalue\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if v, err := resolveValue("env:REF_VAL_OK"); err != nil || v != "s3cret" {
		t.Errorf("env ref: v=%q err=%v", v, err)
	}
	if v, err := resolveValue("file:" + f); err != nil || v != "filevalue" {
		t.Errorf("file ref: v=%q err=%v", v, err)
	}
	if v, err := resolveValue("plain-literal"); err != nil || v != "plain-literal" {
		t.Errorf("literal: want pass-through, got v=%q err=%v", v, err)
	}
	if _, err := resolveValue(""); !errors.Is(err, errValueMissing) {
		t.Errorf("empty: want errValueMissing, got %v", err)
	}
	if _, err := resolveValue("env:REF_DEFINITELY_UNSET"); !errors.Is(err, errValueMissing) {
		t.Errorf("unset env: want errValueMissing, got %v", err)
	}
	if _, err := resolveValue("file:/no/such/path"); err == nil {
		t.Error("missing file: want error, got nil")
	}
}

// TestFullParamTableParse uses the server.md v2 config-chapter sample YAML
// to verify every parameter-table item lands. The pnet_psk/api_key/etc.
// refs point at REF_* (non-schema) names that are NOT set, so the env
// layer leaves the ref strings intact for the assertions.
func TestFullParamTableParse(t *testing.T) {
	writeConfig(t, `
log: { level: info, format: json }
metrics: { listen: ":9090" }

relay:
  enable: true
  listen: ["/ip4/0.0.0.0/tcp/4001"]
  pnet_psk: env:REF_RELAY_PSK
  token_verify:
    group_pubkeys: ["pk1", "pk2"]
  rendezvous: { enable: true }
  limits:
    reservation_per_token: 4
    reservation_per_group: 8
    bandwidth_per_conn: "1MiB/s"
    circuit_max_duration: "10m"

coord:
  enable: true
  http: { listen: ":8080" }
  db: { dsn: env:REF_COORD_DSN }
  external:
    api_key: env:REF_COORD_KEY
    result:
      url: env:REF_COORD_RESULT_WEBHOOK
      secret: env:REF_COORD_RESULT_SECRET
      api_key: env:REF_COORD_RESULT_APIKEY
  notify:
    url: env:REF_COORD_NOTIFY_WEBHOOK
    secret: env:REF_COORD_NOTIFY_SECRET
    api_key: env:REF_COORD_NOTIFY_APIKEY
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
		"relay.pnet_psk":                     cfg.Relay.PnetPSK == "env:REF_RELAY_PSK",
		"relay.token_verify.group_pubkeys":   len(cfg.Relay.TokenVerify.GroupPubkeys) == 2,
		"relay.rendezvous.enable":            cfg.Relay.Rendezvous.Enable,
		"relay.limits.reservation_per_token": cfg.Relay.Limits.ReservationPerToken == 4,
		"relay.limits.reservation_per_group": cfg.Relay.Limits.ReservationPerGroup == 8,
		"relay.limits.bandwidth_per_conn":    cfg.Relay.Limits.BandwidthPerConn == "1MiB/s",
		"relay.limits.circuit_max_duration":  cfg.Relay.Limits.CircuitMaxDuration == "10m",
		"coord.enable":                       cfg.Coord.Enable,
		"coord.http.listen":                  cfg.Coord.HTTP.Listen == ":8080",
		"coord.db.dsn":                       cfg.Coord.DB.DSN == "env:REF_COORD_DSN",
		"coord.external.api_key":             cfg.Coord.External.APIKey == "env:REF_COORD_KEY",
		"coord.external.result.url":          cfg.Coord.External.Result.URL == "env:REF_COORD_RESULT_WEBHOOK",
		"coord.external.result.secret":       cfg.Coord.External.Result.Secret == "env:REF_COORD_RESULT_SECRET",
		"coord.external.result.api_key":      cfg.Coord.External.Result.APIKey == "env:REF_COORD_RESULT_APIKEY",
		"coord.notify.url":                   cfg.Coord.Notify.URL == "env:REF_COORD_NOTIFY_WEBHOOK",
		"coord.notify.secret":                cfg.Coord.Notify.Secret == "env:REF_COORD_NOTIFY_SECRET",
		"coord.notify.api_key":               cfg.Coord.Notify.APIKey == "env:REF_COORD_NOTIFY_APIKEY",
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

// TestValidateFullValid injects all required values (via REF_* refs) and
// expects pass.
func TestValidateFullValid(t *testing.T) {
	t.Setenv(refPSK, "psk")
	t.Setenv(refDSN, "postgres://x")
	t.Setenv(refKey, "key")
	t.Setenv(refRW, "https://ext/result")
	t.Setenv(refNW, "https://ext/notify")
	cfg := Config{
		Relay: RelayConfig{
			Enable:  true,
			PnetPSK: "env:" + refPSK,
		},
		Coord: fullCoord("env:"+refDSN, "env:"+refKey, "env:"+refRW, "env:"+refNW),
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("full valid config: want nil, got %v", err)
	}
}
