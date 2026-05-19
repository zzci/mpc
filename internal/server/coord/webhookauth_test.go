package coord

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newReq(t *testing.T) *http.Request {
	t.Helper()
	r, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://ext/result", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return r
}

// TestApplyWebhookAuthSignature: secret set → X-MCP-Timestamp +
// X-MCP-Signature "t=<ts>,v1=<hex>" with the HMAC over "<ts>.<body>", and
// no Authorization header.
func TestApplyWebhookAuthSignature(t *testing.T) {
	body := []byte(`{"requestId":"r1","status":"RETURNED"}`)
	now := time.Unix(1_700_000_000, 0)
	r := newReq(t)

	ApplyWebhookAuth(r, body, now, "topsecret", "ignored-when-secret-set")

	ts := r.Header.Get(webhookTimestampHeader)
	if ts != "1700000000" {
		t.Fatalf("timestamp header: got %q want 1700000000", ts)
	}
	if r.Header.Get("Authorization") != "" {
		t.Fatal("signature mode must not set Authorization (secret wins over api_key)")
	}
	sig := r.Header.Get(webhookSignatureHeader)
	pfx := "t=" + ts + ",v1="
	if !strings.HasPrefix(sig, pfx) {
		t.Fatalf("signature header format: got %q want prefix %q", sig, pfx)
	}
	gotHex := strings.TrimPrefix(sig, pfx)

	mac := hmac.New(sha256.New, []byte("topsecret"))
	mac.Write([]byte("1700000000."))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if gotHex != want {
		t.Fatalf("HMAC mismatch: got %s want %s", gotHex, want)
	}
	// Independent constant-time check, as the external verifier would do.
	if !hmac.Equal([]byte(gotHex), []byte(want)) {
		t.Fatal("hmac.Equal disagreed")
	}
}

// TestApplyWebhookAuthBearer: no secret but api_key → Authorization Bearer
// and no signature headers (fallback mode).
func TestApplyWebhookAuthBearer(t *testing.T) {
	r := newReq(t)
	ApplyWebhookAuth(r, []byte("{}"), time.Now(), "", "tok-123")

	if got := r.Header.Get("Authorization"); got != "Bearer tok-123" {
		t.Fatalf("Authorization: got %q want %q", got, "Bearer tok-123")
	}
	if r.Header.Get(webhookSignatureHeader) != "" || r.Header.Get(webhookTimestampHeader) != "" {
		t.Fatal("token mode must not set signature headers")
	}
}

// TestApplyWebhookAuthNeither: both empty (an upstream-rejected config) is a
// safe no-op rather than a panic.
func TestApplyWebhookAuthNeither(t *testing.T) {
	r := newReq(t)
	ApplyWebhookAuth(r, []byte("{}"), time.Now(), "", "")
	if r.Header.Get("Authorization") != "" ||
		r.Header.Get(webhookSignatureHeader) != "" ||
		r.Header.Get(webhookTimestampHeader) != "" {
		t.Fatal("no-credential case must set no auth headers")
	}
}

// TestSignedPreimageBindsBody: the preimage is "<ts>.<body>", so a changed
// body (forgery) yields a different signature under the same key/timestamp.
func TestSignedPreimageBindsBody(t *testing.T) {
	ts := int64(1700000000)
	a := signedPreimage(ts, []byte("alpha"))
	if string(a) != strconv.FormatInt(ts, 10)+".alpha" {
		t.Fatalf("preimage: got %q", a)
	}
	mac := func(b []byte) string {
		m := hmac.New(sha256.New, []byte("k"))
		m.Write(b)
		return hex.EncodeToString(m.Sum(nil))
	}
	if mac(a) == mac(signedPreimage(ts, []byte("beta"))) {
		t.Fatal("different bodies must not collide under the same ts/key")
	}
}
