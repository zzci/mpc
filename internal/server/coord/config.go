package coord

import (
	"fmt"
	"time"
)

// Config is the resolved coord runtime configuration. cmd/server builds it from
// server.CoordConfig (docs/design/server/server.md "config" chapter) after
// values are resolved, so this package never reads files/env itself and stays
// unit testable. Durations arrive pre-parsed.
//
// Per the user ruling 2026-05-19 (config framework v2): external auth is
// fixed api_key (no mtls), result delivery is fixed webhook (no longpoll),
// and notification is a single fixed webhook. APIKey, CallbackURL and
// NotifyWebhook are therefore always required when coord runs.
type Config struct {
	// Listen is the external+member API bind address (coord.http.listen).
	Listen string
	// DBPath is the encrypted single-file path resolved from coord.db.dsn.
	DBPath string
	// APIKey is the external API key (auth is fixed api_key,
	// coord.external.api_key); constant-time compared in checkAPIKey.
	APIKey string
	// CallbackURL is the fixed result webhook URL
	// (coord.external.result_webhook); the A4 result is always POSTed here.
	CallbackURL string
	// NotifyWebhook is the single fixed notification webhook
	// (coord.notify.webhook): coord POSTs notification events here and an
	// external channel translates/delivers them (FCM/APNS/etc.).
	NotifyWebhook string
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
	signerStable   = "stable"
	signerLiveness = "liveness"
)

// validate rejects an internally inconsistent Config. Enum legality is already
// enforced by server.Config.Validate; this guards the cross-field invariants
// coord itself relies on.
func (c Config) validate() error {
	if c.Listen == "" {
		return fmt.Errorf("coord: empty listen address")
	}
	if c.DBPath == "" {
		return fmt.Errorf("coord: empty db path")
	}
	if c.APIKey == "" {
		return fmt.Errorf("coord: external api key is empty")
	}
	if c.CallbackURL == "" {
		return fmt.Errorf("coord: external result webhook url is empty")
	}
	if c.NotifyWebhook == "" {
		return fmt.Errorf("coord: notify webhook url is empty")
	}
	if c.SkewTolerance < 0 || c.DispatchTimeout <= 0 {
		return fmt.Errorf("coord: skew_tolerance must be >=0 and dispatch.timeout >0")
	}
	return nil
}
