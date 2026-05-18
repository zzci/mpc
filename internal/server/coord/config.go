package coord

import (
	"fmt"
	"time"
)

// Config is the resolved coord runtime configuration. cmd/node builds it from
// node.CoordConfig (docs/design/server/server.md "配置" chapter) after secrets are
// resolved, so this package never reads files/env itself and stays unit
// testable. Durations arrive pre-parsed.
type Config struct {
	// Listen is the external+member API bind address (coord.http.listen).
	Listen string
	// DBPath is the encrypted single-file path resolved from coord.db.dsn.
	DBPath string
	// ExternalAuth is "mtls" or "api_key" (coord.external.auth).
	ExternalAuth string
	// APIKey is the external API key, set only when ExternalAuth=="api_key".
	APIKey string
	// ResultCallback is "webhook" or "longpoll" (coord.external.result_callback).
	ResultCallback string
	// CallbackURL is the external webhook URL (ResultCallback=="webhook").
	CallbackURL string
	// SkewTolerance is the clock-skew slack; outside it expiry is judged
	// conservatively (docs/design/server/server.md C6(e)).
	SkewTolerance time.Duration
	// SignerSelect is "stable" or "liveness" (coord.quorum.signer_select).
	SignerSelect string
	// DispatchTimeout bounds the wait after DISPATCHED; it is further clamped
	// to the remaining TTL (docs/design/server/server.md C5).
	DispatchTimeout time.Duration
}

const (
	authMTLS         = "mtls"
	authAPIKey       = "api_key"
	callbackWebhook  = "webhook"
	callbackLongpoll = "longpoll"
	signerStable     = "stable"
	signerLiveness   = "liveness"
)

// validate rejects an internally inconsistent Config. Enum legality is already
// enforced by node.Config.Validate; this guards the cross-field invariants
// coord itself relies on.
func (c Config) validate() error {
	if c.Listen == "" {
		return fmt.Errorf("coord: empty listen address")
	}
	if c.DBPath == "" {
		return fmt.Errorf("coord: empty db path")
	}
	if c.ExternalAuth == authAPIKey && c.APIKey == "" {
		return fmt.Errorf("coord: auth=api_key but api key is empty")
	}
	if c.ResultCallback == callbackWebhook && c.CallbackURL == "" {
		return fmt.Errorf("coord: result_callback=webhook but callback url is empty")
	}
	if c.SkewTolerance < 0 || c.DispatchTimeout <= 0 {
		return fmt.Errorf("coord: skew_tolerance must be >=0 and dispatch.timeout >0")
	}
	return nil
}
