package coord

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"
)

// Outbound callback auth (anti-forgery, user ruling 2026-05-19;
// docs/design/server/server.md change-summary item 4, api.md A4).
// coord→external result/notify callbacks were previously unauthenticated, so
// an attacker could forge {requestId,status,RSV} to the business callback
// endpoint. Every coord→external POST now carries one of two modes; this is
// the single place the header wire format is produced (the mock-extsvc
// verifier re-implements the same format independently for the E2E proof).

const (
	// webhookTimestampHeader carries the unix-second send time; the verifier
	// uses it for the skew/replay window and as the HMAC preimage prefix.
	webhookTimestampHeader = "X-MCP-Timestamp"
	// webhookSignatureHeader is "t=<unix>,v1=<hex(HMAC-SHA256)>". t repeats
	// the timestamp so the signed value is self-describing.
	webhookSignatureHeader = "X-MCP-Signature"
)

// signedPreimage is the bytes the HMAC covers: "<unix seconds>.<raw body>".
// Binding the body makes the signature non-replayable onto a different
// payload; binding the timestamp lets the verifier bound replay by skew.
func signedPreimage(ts int64, body []byte) []byte {
	p := append([]byte(strconv.FormatInt(ts, 10)), '.')
	return append(p, body...)
}

// ApplyWebhookAuth sets the auth headers on a coord→external callback
// request per the dual-mode design (user ruling 2026-05-19):
//
//   - secret set → signature mode (preferred): HMAC-SHA256 over
//     "<ts>.<body>", emitted as X-MCP-Timestamp + X-MCP-Signature. Body- and
//     time-bound, so it resists forgery and replay.
//   - secret empty but apiKey set → token mode (fallback): a static
//     Authorization: Bearer header (compat for Bearer-only receivers; no
//     body binding, weaker — hence the fallback).
//
// secret/apiKey having "at least one set, signature wins when both" is
// enforced upstream by server.Config.Validate; this mirrors that precedence
// and is a no-op only if both are empty (an already-rejected config).
func ApplyWebhookAuth(req *http.Request, body []byte, now time.Time, secret, apiKey string) {
	switch {
	case secret != "":
		ts := now.Unix()
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(signedPreimage(ts, body))
		sig := hex.EncodeToString(mac.Sum(nil))
		tsStr := strconv.FormatInt(ts, 10)
		req.Header.Set(webhookTimestampHeader, tsStr)
		req.Header.Set(webhookSignatureHeader, "t="+tsStr+",v1="+sig)
	case apiKey != "":
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}
