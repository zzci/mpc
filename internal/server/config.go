// Package server implements the config framework v2 (server.md "config"
// chapter, user ruling 2026-05-19): a Traefik-style three-source assembly
// — built-in defaults < config file < environment variables < CLI flags,
// one unified key space — where any value may be a literal or an
// env:VAR / file:/path reference, role switches, and fail-fast validation
// of the required values of enabled roles.
package server

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	defaultConfigPath = "./server.yaml"
	configPathEnv     = "SERVER_CONFIG"
	configPathFlag    = "config"
	// envPrefix + the upper-cased dotted key (segments joined by envSep)
	// is the env name (user ruling 2026-05-19, cbd278f). Because the
	// nesting separator and any key-internal '_' are both '_', env names
	// are AMBIGUOUS to parse — the env layer never parses an env name;
	// it generates each known leaf key's name from the schema and
	// exact-matches the environment (schema-driven generate-and-match).
	envPrefix     = "MPC_"
	envSep        = "_"
	cliFlagPrefix = "--"
	cliKeySep     = "."
)

// Config is the full node config (log/metrics/relay/coord).
type Config struct {
	Log     LogConfig     `yaml:"log"`
	Metrics MetricsConfig `yaml:"metrics"`
	Relay   RelayConfig   `yaml:"relay"`
	Coord   CoordConfig   `yaml:"coord"`
}

// LogConfig is the log level and format.
type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// MetricsConfig is the health-check / metrics listen address.
type MetricsConfig struct {
	Listen string `yaml:"listen"`
}

// RelayConfig is the relay-role config (circuit-relay v2 + rendezvous +
// access control).
type RelayConfig struct {
	Enable      bool              `yaml:"enable"`
	Listen      []string          `yaml:"listen"`
	PnetPSK     string            `yaml:"pnet_psk"`
	TokenVerify TokenVerifyConfig `yaml:"token_verify"`
	Rendezvous  RendezvousConfig  `yaml:"rendezvous"`
	Limits      RelayLimitsConfig `yaml:"limits"`
}

// TokenVerifyConfig holds the self-sovereign trust anchor for
// capability-token verification: relay verifies CapToken.groupSig
// locally against this group public key set and never depends on coord
// (server.md R5 / change-summary item 5).
type TokenVerifyConfig struct {
	GroupPubkeys []string `yaml:"group_pubkeys"`
}

// RendezvousConfig toggles rendezvous discovery.
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
	Notify   CoordNotifyConfig   `yaml:"notify"`
	TTL      CoordTTLConfig      `yaml:"ttl"`
	Quorum   CoordQuorumConfig   `yaml:"quorum"`
	Dispatch CoordDispatchConfig `yaml:"dispatch"`
}

// CoordHTTPConfig is the external-service + member API listen address.
type CoordHTTPConfig struct {
	Listen string `yaml:"listen"`
}

// CoordDBConfig is the persistence DSN + whole-DB encryption switch. DSN
// is a required value (literal or env:/file: reference).
type CoordDBConfig struct {
	DSN        string                  `yaml:"dsn"`
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

// OutboundWebhookConfig is a coord→external callback target with
// anti-forgery callback auth (user ruling 2026-05-19, server.md
// change-summary item 4 / api.md A4). URL is always required.
// Secret/APIKey are the two auth modes — at least one must be set;
// both set means signature is used:
//   - Secret: HMAC-SHA256 over "<unix>.<raw body>" (preferred; body-bound,
//     replay-resistant).
//   - APIKey: a static Authorization: Bearer token (fallback for receivers
//     that only support Bearer; no body binding, weaker).
//
// These outbound credentials are physically isolated from the inbound
// CoordExternalConfig.APIKey (different direction, independent fields,
// never reused). Any value may be a literal or an env:/file: reference.
type OutboundWebhookConfig struct {
	URL    string `yaml:"url"`
	Secret string `yaml:"secret"`
	APIKey string `yaml:"api_key"`
}

// CoordExternalConfig is the external-service entry: inbound auth is fixed
// api_key (always required when coord is enabled), result delivery is the
// fixed Result webhook with anti-forgery callback auth (user ruling
// 2026-05-19; renamed from result_webhook to result to disambiguate from
// notify).
//
// ExpectedMembers is the per-group strict identity allowlist required by the
// distributed-mpc design (R3 / §2.1 / api.md B9-B11, user ruling 2026-05-19):
// keygen/reshare/attestation requests are accepted only when every relevant
// identity is pre-declared here. Keys are groupId, values are the hex-encoded
// secp256k1 identity pubkeys (33B compressed or 65B uncompressed) of the
// allowed members. Empty / absent map = no group is keygen-eligible (DM-4
// endpoints fail-close with EXPECTED_MEMBER_MISMATCH); coord still serves
// existing provisioned groups for sign/dispatch. The map is config-file only
// (env/CLI overrides not supported for nested map values).
type CoordExternalConfig struct {
	APIKey          string                `yaml:"api_key"`
	Result          OutboundWebhookConfig `yaml:"result"`
	ExpectedMembers map[string][]string   `yaml:"expected_members"`
}

// CoordNotifyConfig is the single fixed notification webhook, flattened to
// {url, secret, api_key} (user ruling 2026-05-19): coord only POSTs
// notification events here; FCM/APNS/etc. are translated and delivered by
// an external notification channel. coord holds no push credentials and
// does not distinguish fcm/apns. Callback auth is the same dual mode as
// the result webhook.
type CoordNotifyConfig OutboundWebhookConfig

// CoordTTLConfig is the clock-skew tolerance.
type CoordTTLConfig struct {
	SkewTolerance string `yaml:"skew_tolerance"`
}

// CoordQuorumConfig is the signer-subset selection policy.
type CoordQuorumConfig struct {
	SignerSelect string `yaml:"signer_select"`
}

// CoordDispatchConfig is the post-dispatch signing-completion timeout.
type CoordDispatchConfig struct {
	Timeout string `yaml:"timeout"`
}

// ErrNoRoleEnabled means relay.enable and coord.enable are both false.
var ErrNoRoleEnabled = errors.New("relay.enable and coord.enable are both false")

// errValueMissing means a required value (literal or resolved reference)
// is absent. Plaintext literals are no longer rejected (user ruling
// 2026-05-19: any value may be a literal or an env:/file: reference; the
// secret-must-be-a-reference hard rule is relaxed).
var errValueMissing = errors.New("required value missing")

// errInsecureDBNotConfirmed is the production iron-law guardrail
// (database.md §7.1): coord enabled with whole-DB encryption disabled but
// no explicit ALLOW_INSECURE_DB=1 non-production confirmation →
// fail-closed refuse to start. This guardrail is explicitly unaffected by
// the config-framework-v2 relaxation (user ruling 2026-05-19).
var errInsecureDBNotConfirmed = fmt.Errorf(
	"coord enabled with db.encryption.enable=false but %s=1 not set: "+
		"whole-DB encryption may be disabled only in non-production "+
		"(database.md §7.1 production iron-law guardrail)", allowInsecureDBEnv)

func defaults() Config {
	return Config{
		Log:     LogConfig{Level: "info", Format: "json"},
		Metrics: MetricsConfig{Listen: ":9090"},
		Relay: RelayConfig{
			Rendezvous: RendezvousConfig{Enable: true},
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
			TTL:      CoordTTLConfig{SkewTolerance: "30s"},
			Quorum:   CoordQuorumConfig{SignerSelect: "liveness"},
			Dispatch: CoordDispatchConfig{Timeout: "120s"},
		},
	}
}

// Load assembles config as built-in defaults < config file < env vars <
// CLI flags (the Traefik-style three sources, user ruling 2026-05-19).
// The file path is the CLI --config flag, else SERVER_CONFIG, else
// ./server.yaml; a missing default path allows pure-env/CLI config, but a
// missing explicitly-set path errors. CLI flags are os.Args[1:].
func Load() (Config, error) {
	return loadFrom(os.Args[1:])
}

// loadFrom is Load with injectable CLI args (deterministic tests).
func loadFrom(args []string) (Config, error) {
	flags, err := parseCLIFlags(args)
	if err != nil {
		return Config{}, err
	}

	cfg := defaults()

	path, explicit := configPath(flags)
	data, rerr := os.ReadFile(path)
	switch {
	case rerr == nil:
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config %s: %w", path, err)
		}
	case errors.Is(rerr, os.ErrNotExist) && !explicit:
		// Default path absent: allow pure env/CLI injection (containers/CI).
	default:
		return Config{}, fmt.Errorf("read config %s: %w", path, rerr)
	}

	if err := applyEnvOverrides(&cfg); err != nil {
		return Config{}, err
	}
	if err := applyCLIOverrides(&cfg, flags); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate runs startup checks: fail if both roles are off; enabled
// roles' constrained enums must be valid; required values of enabled
// roles must resolve (literal or env:/file: reference; an empty/unset one
// is fail-fast). Only enabled roles are checked.
func (c Config) Validate() error {
	if !c.Relay.Enable && !c.Coord.Enable {
		return ErrNoRoleEnabled
	}
	if c.Relay.Enable {
		if _, err := resolveValue(c.Relay.PnetPSK); err != nil {
			return fmt.Errorf("relay enabled: pnet_psk: %w", err)
		}
	}
	if c.Coord.Enable {
		// Production iron-law guardrail (database.md §7.1): unchanged by
		// config-framework-v2. Default enable=true, so normal/production
		// config is unaffected; this only triggers on explicit enable=false.
		if !c.Coord.DB.Encryption.Enable && os.Getenv(allowInsecureDBEnv) != "1" {
			return errInsecureDBNotConfirmed
		}
		if err := validateEnum("coord.quorum.signer_select", c.Coord.Quorum.SignerSelect,
			"stable", "liveness"); err != nil {
			return err
		}
		if _, err := resolveValue(c.Coord.DB.DSN); err != nil {
			return fmt.Errorf("coord enabled: db.dsn: %w", err)
		}
		// Inbound auth is fixed api_key (always required). Result delivery
		// and notification are fixed webhooks with anti-forgery callback
		// auth: url required, plus at least one of secret/api_key (user
		// ruling 2026-05-19).
		if _, err := resolveValue(c.Coord.External.APIKey); err != nil {
			return fmt.Errorf("coord enabled: external.api_key: %w", err)
		}
		if err := validateOutboundWebhook("external.result", c.Coord.External.Result); err != nil {
			return err
		}
		if err := validateOutboundWebhook("notify", OutboundWebhookConfig(c.Coord.Notify)); err != nil {
			return err
		}
		if err := validateExpectedMembers(c.Coord.External.ExpectedMembers); err != nil {
			return err
		}
	}
	return nil
}

// validateExpectedMembers fail-fasts a malformed
// coord.external.expected_members entry: every hex value must decode to a
// secp256k1 serialized pubkey length (33 compressed / 65 uncompressed), the
// only forms accepted on the wire (api.md B9/B10 memberSet, distributed-mpc.md
// §2.1). An empty map is permitted: it simply means no group is keygen
// eligible until configured.
func validateExpectedMembers(m map[string][]string) error {
	for gid, members := range m {
		if strings.TrimSpace(gid) == "" {
			return fmt.Errorf("coord enabled: external.expected_members: empty groupId key")
		}
		if len(members) == 0 {
			return fmt.Errorf("coord enabled: external.expected_members[%q]: empty member list", gid)
		}
		seen := map[string]bool{}
		for i, hexKey := range members {
			k := strings.TrimSpace(hexKey)
			if k == "" {
				return fmt.Errorf("coord enabled: external.expected_members[%q][%d]: empty hex pubkey", gid, i)
			}
			if seen[k] {
				return fmt.Errorf("coord enabled: external.expected_members[%q][%d]: duplicate identity pubkey", gid, i)
			}
			seen[k] = true
			b, err := hex.DecodeString(k)
			if err != nil {
				return fmt.Errorf("coord enabled: external.expected_members[%q][%d]: not hex: %w", gid, i, err)
			}
			if len(b) != 33 && len(b) != 65 {
				return fmt.Errorf("coord enabled: external.expected_members[%q][%d]: pubkey length %d not 33/65", gid, i, len(b))
			}
		}
	}
	return nil
}

func configPath(flags map[string]string) (path string, explicit bool) {
	if p := flags[configPathFlag]; p != "" {
		return p, true
	}
	if p := os.Getenv(configPathEnv); p != "" {
		return p, true
	}
	return defaultConfigPath, false
}

// resolveValue resolves a config value: env:VAR / file:/path are resolved
// references; any other non-empty string is a literal and returned as-is
// (user ruling 2026-05-19: literals are allowed, plaintext is no longer
// rejected). Empty (or an unset/empty reference) = errValueMissing.
func resolveValue(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", errValueMissing
	}
	switch {
	case strings.HasPrefix(v, "env:"):
		got := os.Getenv(strings.TrimPrefix(v, "env:"))
		if got == "" {
			return "", errValueMissing
		}
		return got, nil
	case strings.HasPrefix(v, "file:"):
		b, err := os.ReadFile(strings.TrimPrefix(v, "file:"))
		if err != nil {
			return "", fmt.Errorf("read value file: %w", err)
		}
		got := strings.TrimSpace(string(b))
		if got == "" {
			return "", errValueMissing
		}
		return got, nil
	default:
		return v, nil
	}
}

// errWebhookAuthNeitherSet means a coord→external webhook configured
// neither secret nor api_key. Callback auth is mandatory (anti-forgery,
// user ruling 2026-05-19): at least one mode must be set, else fail-fast.
var errWebhookAuthNeitherSet = errors.New(
	"callback auth missing: set secret (HMAC-SHA256, preferred) or api_key (Bearer)")

// validateOutboundWebhook fail-fasts a coord→external callback target: the
// url must resolve, and at least one auth mode (secret/api_key) must
// resolve. When both are set the signer prefers the signature; validation
// only requires one to be present.
func validateOutboundWebhook(prefix string, w OutboundWebhookConfig) error {
	if _, err := resolveValue(w.URL); err != nil {
		return fmt.Errorf("coord enabled: %s.url: %w", prefix, err)
	}
	_, secErr := resolveValue(w.Secret)
	_, keyErr := resolveValue(w.APIKey)
	if secErr != nil && keyErr != nil {
		return fmt.Errorf("coord enabled: %s: %w", prefix, errWebhookAuthNeitherSet)
	}
	return nil
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

// walkEnv recurses yaml-tagged structs and, for every known leaf key,
// GENERATES its env name (envPrefix + the upper-cased yaml path, segments
// joined by envSep) and exact-matches the environment. It never parses an
// env name back into a key: the nesting separator and any key-internal
// '_' are both '_', so env names are inherently ambiguous to split —
// schema-driven generate-and-match is unambiguous because the key set is
// static (user ruling 2026-05-19, cbd278f).
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
			if err := walkEnv(fv, key+envSep); err != nil {
				return err
			}
			continue
		}
		// Map-typed leaves (e.g. coord.external.expected_members) are
		// structured nested values; env/CLI cannot express them
		// unambiguously and are intentionally config-file only.
		if fv.Kind() == reflect.Map {
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

// parseCLIFlags collects --<dotted.key>=<value> (and --config <path> /
// --config=<path>) from args into a dotted-key map. Non-"--" args are
// ignored so `go test` single-dash flags never collide with the unified
// key space. The CLI is the highest-priority source (Traefik-style).
func parseCLIFlags(args []string) (map[string]string, error) {
	out := map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, cliFlagPrefix) {
			continue
		}
		body := strings.TrimPrefix(a, cliFlagPrefix)
		if body == "" {
			continue
		}
		key, val, hasEq := strings.Cut(body, "=")
		if !hasEq {
			// --config <path>: the only space-separated form; every other
			// key requires --key=value.
			if key == configPathFlag && i+1 < len(args) {
				out[key] = args[i+1]
				i++
				continue
			}
			return nil, fmt.Errorf("cli flag %s%s: want %s<key>=<value>", cliFlagPrefix, body, cliFlagPrefix)
		}
		if key == "" {
			return nil, fmt.Errorf("cli flag %s: empty key", a)
		}
		out[key] = val
	}
	return out, nil
}

// applyCLIOverrides applies the dotted-key flags onto cfg by walking the
// yaml-tag path (the same unified key space as file/env).
func applyCLIOverrides(cfg *Config, flags map[string]string) error {
	for key, val := range flags {
		if key == configPathFlag {
			continue // consumed by configPath
		}
		fv, err := fieldByYAMLPath(reflect.ValueOf(cfg).Elem(), strings.Split(key, cliKeySep))
		if err != nil {
			return fmt.Errorf("cli flag %s%s: %w", cliFlagPrefix, key, err)
		}
		if err := setScalar(fv, val); err != nil {
			return fmt.Errorf("cli flag %s%s: %w", cliFlagPrefix, key, err)
		}
	}
	return nil
}

// fieldByYAMLPath descends a struct following yaml-tag names, returning
// the addressable scalar field the dotted path points at.
func fieldByYAMLPath(v reflect.Value, path []string) (reflect.Value, error) {
	if len(path) == 0 {
		return reflect.Value{}, errors.New("empty key")
	}
	t := v.Type()
	for i := range t.NumField() {
		tag := t.Field(i).Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		if strings.Split(tag, ",")[0] != path[0] {
			continue
		}
		fv := v.Field(i)
		if len(path) == 1 {
			if fv.Kind() == reflect.Struct {
				return reflect.Value{}, fmt.Errorf("%q is a section, not a value", path[0])
			}
			if fv.Kind() == reflect.Map {
				return reflect.Value{}, fmt.Errorf("%q is a map; configure via the file source", path[0])
			}
			return fv, nil
		}
		if fv.Kind() != reflect.Struct {
			return reflect.Value{}, fmt.Errorf("%q is not a section", path[0])
		}
		return fieldByYAMLPath(fv, path[1:])
	}
	return reflect.Value{}, fmt.Errorf("unknown key %q", path[0])
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
