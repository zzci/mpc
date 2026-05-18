// Package node implements the node config system: built-in defaults <
// config file < environment variables, role switches, and fail-fast
// validation of required secrets for enabled roles (server.md "config").
package node

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	defaultConfigPath = "./node.yaml"
	configPathEnv     = "NODE_CONFIG"
	envPrefix         = "TSSNODE_"
	envNestSep        = "__"
)

// Config is the full node config (log/metrics/relay/coord).
type Config struct {
	Log     LogConfig     `yaml:"log"`
	Metrics MetricsConfig `yaml:"metrics"`
	Relay   RelayConfig   `yaml:"relay"`
	Coord   CoordConfig   `yaml:"coord"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type MetricsConfig struct {
	Listen string `yaml:"listen"`
}

// RelayConfig is the relay-role config (circuit-relay v2 + rendezvous +
// access control).
type RelayConfig struct {
	Enable      bool              `yaml:"enable"`
	Listen      []string          `yaml:"listen"`
	PnetPSKRef  string            `yaml:"pnet_psk_ref"`
	TokenVerify TokenVerifyConfig `yaml:"token_verify"`
	Rendezvous  RendezvousConfig  `yaml:"rendezvous"`
	Limits      RelayLimitsConfig `yaml:"limits"`
}

type TokenVerifyConfig struct {
	Source       string   `yaml:"source"`
	GroupPubkeys []string `yaml:"group_pubkeys"`
}

type RendezvousConfig struct {
	Enable bool `yaml:"enable"`
}

// RelayLimitsConfig is the reservation/connection/bandwidth quota
// (anti-DoS). CircuitMaxDuration is the max lifetime of one circuit-v2
// relayed link before reset (Go duration, e.g. "10m"; empty = libp2p
// default 2m). security.md invariant #10 / RA-001 P1-1: production
// networked keygen/reshare MUST run Paillier proofs and is minutes-long,
// so the default 2m cap would reset the link mid-keygen; this must be
// keygen-aware / configurable rather than dropping proofs (testing.md §6).
type RelayLimitsConfig struct {
	ReservationPerToken int    `yaml:"reservation_per_token"`
	ReservationPerGroup int    `yaml:"reservation_per_group"`
	BandwidthPerConn    string `yaml:"bandwidth_per_conn"`
	CircuitMaxDuration  string `yaml:"circuit_max_duration"`
}

// CoordConfig is the coord-role config (orchestrator + external-service
// entry).
type CoordConfig struct {
	Enable   bool                `yaml:"enable"`
	HTTP     CoordHTTPConfig     `yaml:"http"`
	DB       CoordDBConfig       `yaml:"db"`
	External CoordExternalConfig `yaml:"external"`
	Push     CoordPushConfig     `yaml:"push"`
	TTL      CoordTTLConfig      `yaml:"ttl"`
	Quorum   CoordQuorumConfig   `yaml:"quorum"`
	Dispatch CoordDispatchConfig `yaml:"dispatch"`
}

type CoordHTTPConfig struct {
	Listen string `yaml:"listen"`
}

type CoordDBConfig struct {
	DSNRef     string                  `yaml:"dsn_ref"`
	Encryption CoordDBEncryptionConfig `yaml:"encryption"`
}

// CoordDBEncryptionConfig controls coord whole-DB encryption at rest
// (database.md §7/§7.1). Default enable=true (set in defaults()):
// encrypted + LOCKED. Only dev/test may set false; disabling requires the
// explicit non-production confirmation enforced by Validate(), else the
// node refuses to start.
type CoordDBEncryptionConfig struct {
	Enable bool `yaml:"enable"`
}

// allowInsecureDBEnv is the explicit "non-production confirmation" env
// that must be set to disable whole-DB encryption (database.md §7.1
// production iron-law guardrail). Fail-closed: absent = refuse. Hardened
// production/release CI never sets it, so disabling encryption in
// production is impossible, not merely discouraged.
const allowInsecureDBEnv = "ALLOW_INSECURE_DB"

type CoordExternalConfig struct {
	Auth           string `yaml:"auth"`
	APIKeyRef      string `yaml:"api_key_ref"`
	ResultCallback string `yaml:"result_callback"`
}

type CoordPushConfig struct {
	FCMCredRef  string `yaml:"fcm_cred_ref"`
	APNSCredRef string `yaml:"apns_cred_ref"`
}

type CoordTTLConfig struct {
	SkewTolerance string `yaml:"skew_tolerance"`
}

type CoordQuorumConfig struct {
	SignerSelect string `yaml:"signer_select"`
}

type CoordDispatchConfig struct {
	Timeout string `yaml:"timeout"`
}

// ErrNoRoleEnabled means relay.enable and coord.enable are both false.
var ErrNoRoleEnabled = errors.New("relay.enable and coord.enable are both false")

var errSecretMissing = errors.New("required secret missing")

// errSecretPlaintext means a secret appeared as a plaintext literal:
// server.md requires every secret to be injected via env:VAR / file:/path;
// plaintext in a committed config file is forbidden.
var errSecretPlaintext = errors.New("secret must be an env: or file: reference (plaintext forbidden)")

// errInsecureDBNotConfirmed is the production iron-law guardrail
// (database.md §7.1): coord enabled with whole-DB encryption disabled but
// no explicit ALLOW_INSECURE_DB=1 non-production confirmation →
// fail-closed refuse to start. Disabling encryption in production =
// plaintext fund-orchestration data at rest (a security red line); the
// guardrail makes it impossible.
var errInsecureDBNotConfirmed = fmt.Errorf(
	"coord enabled with db.encryption.enable=false but %s=1 not set: "+
		"whole-DB encryption may be disabled only in non-production "+
		"(database.md §7.1 production iron-law guardrail)", allowInsecureDBEnv)

func defaults() Config {
	return Config{
		Log:     LogConfig{Level: "info", Format: "json"},
		Metrics: MetricsConfig{Listen: ":9090"},
		Relay: RelayConfig{
			TokenVerify: TokenVerifyConfig{Source: "config"},
			Rendezvous:  RendezvousConfig{Enable: true},
			Limits: RelayLimitsConfig{
				ReservationPerToken: 4,
				ReservationPerGroup: 8,
				BandwidthPerConn:    "1MiB/s",
				// keygen-aware: a proof-enabled networked keygen/reshare is
				// minutes-long; 10m comfortably covers a small-group session
				// while staying a bounded anti-DoS cap (security.md #10).
				CircuitMaxDuration: "10m",
			},
		},
		Coord: CoordConfig{
			HTTP:     CoordHTTPConfig{Listen: ":8080"},
			DB:       CoordDBConfig{Encryption: CoordDBEncryptionConfig{Enable: true}},
			External: CoordExternalConfig{Auth: "mtls", ResultCallback: "webhook"},
			TTL:      CoordTTLConfig{SkewTolerance: "30s"},
			Quorum:   CoordQuorumConfig{SignerSelect: "liveness"},
			Dispatch: CoordDispatchConfig{Timeout: "120s"},
		},
	}
}

// Load assembles config as built-in defaults < config file < env vars.
// The path is NODE_CONFIG (default ./node.yaml); a missing default path
// allows pure-env config, but a missing explicitly-set NODE_CONFIG errors.
func Load() (Config, error) {
	cfg := defaults()

	path, explicit := configPath()
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config %s: %w", path, err)
		}
	case errors.Is(err, os.ErrNotExist) && !explicit:
		// Default path absent: allow pure-env injection (containers/CI).
	default:
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	if err := applyEnvOverrides(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate runs startup checks: fail if both roles are off; enabled
// roles' constrained enums must be valid; required secrets must resolve
// and be env:/file: refs (plaintext = fail-fast); a configured optional
// secret is held to the same ref discipline. Only enabled roles are
// checked (a disabled role's config does not gate startup).
func (c Config) Validate() error {
	if !c.Relay.Enable && !c.Coord.Enable {
		return ErrNoRoleEnabled
	}
	if c.Relay.Enable {
		if err := validateEnum("relay.token_verify.source", c.Relay.TokenVerify.Source,
			"config", "coord-sync"); err != nil {
			return err
		}
		if _, err := resolveSecret(c.Relay.PnetPSKRef); err != nil {
			return fmt.Errorf("relay enabled: pnet_psk: %w", err)
		}
	}
	if c.Coord.Enable {
		// Production iron-law guardrail (database.md §7.1): if whole-DB
		// encryption is disabled, ALLOW_INSECURE_DB=1 must explicitly
		// confirm non-production, else fail-closed. Default enable=true, so
		// normal/production config is unaffected; this only triggers on an
		// explicit enable=false.
		if !c.Coord.DB.Encryption.Enable && os.Getenv(allowInsecureDBEnv) != "1" {
			return errInsecureDBNotConfirmed
		}
		if err := validateEnum("coord.external.auth", c.Coord.External.Auth,
			"mtls", "api_key"); err != nil {
			return err
		}
		if err := validateEnum("coord.external.result_callback", c.Coord.External.ResultCallback,
			"webhook", "longpoll"); err != nil {
			return err
		}
		if err := validateEnum("coord.quorum.signer_select", c.Coord.Quorum.SignerSelect,
			"stable", "liveness"); err != nil {
			return err
		}
		if _, err := resolveSecret(c.Coord.DB.DSNRef); err != nil {
			return fmt.Errorf("coord enabled: db.dsn: %w", err)
		}
		if c.Coord.External.Auth == "api_key" {
			if _, err := resolveSecret(c.Coord.External.APIKeyRef); err != nil {
				return fmt.Errorf("coord enabled (auth=api_key): external.api_key: %w", err)
			}
		}
		if err := optionalSecret(c.Coord.Push.FCMCredRef); err != nil {
			return fmt.Errorf("coord enabled: push.fcm: %w", err)
		}
		if err := optionalSecret(c.Coord.Push.APNSCredRef); err != nil {
			return fmt.Errorf("coord enabled: push.apns: %w", err)
		}
	}
	return nil
}

func configPath() (path string, explicit bool) {
	if p := os.Getenv(configPathEnv); p != "" {
		return p, true
	}
	return defaultConfigPath, false
}

// resolveSecret resolves a secret ref: only env:VAR / file:/path; empty =
// missing; any other literal is treated as a plaintext secret and
// rejected (server.md: no plaintext in committed config files).
func resolveSecret(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errSecretMissing
	}
	switch {
	case strings.HasPrefix(ref, "env:"):
		v := os.Getenv(strings.TrimPrefix(ref, "env:"))
		if v == "" {
			return "", errSecretMissing
		}
		return v, nil
	case strings.HasPrefix(ref, "file:"):
		b, err := os.ReadFile(strings.TrimPrefix(ref, "file:"))
		if err != nil {
			return "", fmt.Errorf("read secret file: %w", err)
		}
		v := strings.TrimSpace(string(b))
		if v == "" {
			return "", errSecretMissing
		}
		return v, nil
	default:
		return "", errSecretPlaintext
	}
}

// optionalSecret validates an optional secret: skip if unset (empty ref);
// once set, the same env:/file: ref discipline applies and it must resolve.
func optionalSecret(ref string) error {
	if strings.TrimSpace(ref) == "" {
		return nil
	}
	_, err := resolveSecret(ref)
	return err
}

// validateEnum checks a constrained string value (server.md enum column).
func validateEnum(field, val string, allowed ...string) error {
	for _, a := range allowed {
		if val == a {
			return nil
		}
	}
	return fmt.Errorf("%s: invalid value %q (allowed: %s)", field, val, strings.Join(allowed, "|"))
}

func applyEnvOverrides(cfg *Config) error {
	return walkEnv(reflect.ValueOf(cfg).Elem(), envPrefix)
}

// walkEnv recurses yaml-tagged structs, overriding scalars from
// TSSNODE_<UPPER KEY> (nested keys joined by __).
func walkEnv(v reflect.Value, prefix string) error {
	t := v.Type()
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		key := prefix + strings.ToUpper(name)

		fv := v.Field(i)
		if fv.Kind() == reflect.Struct {
			if err := walkEnv(fv, key+envNestSep); err != nil {
				return err
			}
			continue
		}
		raw, ok := os.LookupEnv(key)
		if !ok {
			continue
		}
		if err := setScalar(fv, raw); err != nil {
			return fmt.Errorf("env %s: %w", key, err)
		}
	}
	return nil
}

func setScalar(fv reflect.Value, raw string) error {
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("parse bool: %w", err)
		}
		fv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return fmt.Errorf("parse int: %w", err)
		}
		fv.SetInt(n)
	case reflect.Slice:
		if fv.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("unsupported slice element %s", fv.Type().Elem().Kind())
		}
		parts := strings.Split(raw, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		fv.Set(reflect.ValueOf(parts))
	default:
		return fmt.Errorf("unsupported kind %s", fv.Kind())
	}
	return nil
}
