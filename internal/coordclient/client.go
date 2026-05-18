package coordclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
)

// maxErrBody caps how much of an error response body is read before mapping it
// to an APIError (defends the client against a hostile/oversized coord reply).
const maxErrBody = 1 << 16

// RetryPolicy bounds backoff for transient coord conditions (503 LOCKED, 5xx
// INTERNAL, 429). Delay is exponential from BaseDelay, capped at MaxDelay,
// with full jitter; the loop also stops on context cancellation.
type RetryPolicy struct {
	MaxAttempts int           // total attempts including the first (>=1)
	BaseDelay   time.Duration // first backoff step
	MaxDelay    time.Duration // per-step ceiling before jitter
}

// DefaultRetryPolicy is a conservative policy suitable for a mobile member
// device polling a coord that may be briefly LOCKED.
var DefaultRetryPolicy = RetryPolicy{
	MaxAttempts: 5,
	BaseDelay:   500 * time.Millisecond,
	MaxDelay:    8 * time.Second,
}

// Client is a single member's coord API client, scoped to one (groupId,
// memberId) identity. It is safe for concurrent use: per-request state (nonce,
// ts, signature) is created on each call.
type Client struct {
	baseURL  string
	memberID string
	groupID  string
	priv     *btcec.PrivateKey

	hc    *http.Client
	now   func() time.Time
	rng   io.Reader
	retry RetryPolicy
}

// Option customizes a Client (functional-options pattern).
type Option func(*Client)

// WithHTTPClient overrides the default *http.Client (e.g. to set TLS or a
// transport timeout that bounds long-poll waits).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.hc = hc
		}
	}
}

// WithClock overrides the time source (test seam for ts/backoff).
func WithClock(now func() time.Time) Option {
	return func(c *Client) {
		if now != nil {
			c.now = now
		}
	}
}

// WithRand overrides the nonce entropy source (test seam).
func WithRand(r io.Reader) Option {
	return func(c *Client) {
		if r != nil {
			c.rng = r
		}
	}
}

// WithRetryPolicy overrides DefaultRetryPolicy. A zero MaxAttempts is clamped
// to 1 (no retry) at request time.
func WithRetryPolicy(p RetryPolicy) Option {
	return func(c *Client) { c.retry = p }
}

// New builds a Client for member memberID of group groupID against the coord
// at baseURL, signing every request with identity key priv. baseURL must
// include scheme and host (e.g. https://coord.example); a trailing slash is
// trimmed.
func New(baseURL, groupID, memberID string, priv *btcec.PrivateKey, opts ...Option) (*Client, error) {
	if baseURL == "" || groupID == "" || memberID == "" {
		return nil, fmt.Errorf("coordclient: baseURL, groupID and memberID are required")
	}
	if priv == nil {
		return nil, fmt.Errorf("coordclient: identity private key is required")
	}
	c := &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		memberID: memberID,
		groupID:  groupID,
		priv:     priv,
		hc:       &http.Client{Timeout: 60 * time.Second},
		now:      time.Now,
		rng:      rand.Reader,
		retry:    DefaultRetryPolicy,
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// NewFromKeyBytes is New with the identity key supplied as a raw 32-byte
// secp256k1 scalar (keystore export form).
func NewFromKeyBytes(baseURL, groupID, memberID string, keyBytes []byte, opts ...Option) (*Client, error) {
	priv, err := loadIdentityKey(keyBytes)
	if err != nil {
		return nil, err
	}
	return New(baseURL, groupID, memberID, priv, opts...)
}

// request describes one signed coord call. params is the exact byte string the
// server binds the signature to: the request body for POST, the raw query for
// GET (see auth.go).
type request struct {
	method   string // HTTP verb
	path     string // path beginning with /v1
	rawQuery string // for GET endpoints; also the signed params
	body     []byte // for POST/PUT endpoints; also the signed params
	authVerb string // coord auth method label, e.g. "B5:heartbeat"
}

// do executes req with member-auth signing and the retry policy. On a non-2xx
// it parses the api.md error envelope into *APIError; transient codes are
// retried with jittered exponential backoff, terminal codes return at once.
// The decoded success body (if any) is returned as raw bytes for the caller's
// endpoint-specific unmarshal; status 204 yields nil.
func (c *Client) do(ctx context.Context, req request) ([]byte, error) {
	attempts := c.retry.MaxAttempts
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			if err := c.sleep(ctx, attempt); err != nil {
				return nil, err
			}
		}
		body, err := c.attempt(ctx, req)
		if err == nil {
			return body, nil
		}
		lastErr = err
		var ae *APIError
		if !errors.As(err, &ae) || !retryable(ae) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("coordclient: exhausted %d attempts: %w", attempts, lastErr)
}

// attempt performs exactly one signed round-trip (fresh nonce+ts each call so
// a retried attempt is never a replay).
func (c *Client) attempt(ctx context.Context, req request) ([]byte, error) {
	u := c.baseURL + req.path
	if req.rawQuery != "" {
		u += "?" + req.rawQuery
	}
	var bodyR io.Reader
	if len(req.body) > 0 {
		bodyR = bytes.NewReader(req.body)
	}
	hr, err := http.NewRequestWithContext(ctx, req.method, u, bodyR)
	if err != nil {
		return nil, fmt.Errorf("coordclient: build request: %w", err)
	}
	if len(req.body) > 0 {
		hr.Header.Set("Content-Type", "application/json")
	}

	// The signed params are the exact bytes the server re-reads: body for
	// POST/PUT, raw query for GET (auth.go / api.go memberGate).
	signParams := req.body
	if req.method == http.MethodGet {
		signParams = []byte(req.rawQuery)
	}
	if err := c.signRequest(hr, req.authVerb, signParams); err != nil {
		return nil, err
	}

	resp, err := c.hc.Do(hr)
	if err != nil {
		return nil, fmt.Errorf("coordclient: transport: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		b, rerr := io.ReadAll(resp.Body)
		if rerr != nil {
			return nil, fmt.Errorf("coordclient: read body: %w", rerr)
		}
		return b, nil
	}
	return nil, parseAPIError(resp)
}

// parseAPIError maps a non-2xx coord response to *APIError using the
// {error:{code,message,requestId?}} envelope; an unparsable body still yields
// a typed error keyed on HTTP status so retry classification works.
func parseAPIError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBody))
	var env struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"requestId"`
		} `json:"error"`
	}
	ae := &APIError{Status: resp.StatusCode}
	if json.Unmarshal(raw, &env) == nil && env.Error.Code != "" {
		ae.Code = env.Error.Code
		ae.Message = env.Error.Message
		ae.RequestID = env.Error.RequestID
	} else {
		ae.Code = statusToCode(resp.StatusCode)
		ae.Message = strings.TrimSpace(string(raw))
	}
	return ae
}

// statusToCode is the fallback when the body is not the api.md envelope.
func statusToCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return CodeInvalidEnvelope
	case http.StatusUnauthorized:
		return CodeUnauthenticated
	case http.StatusForbidden:
		return CodeForbidden
	case http.StatusNotFound:
		return CodeNotFound
	case http.StatusConflict:
		return CodeStateConflict
	case http.StatusGone:
		return CodeExpired
	case http.StatusTooManyRequests:
		return CodeRateLimited
	case http.StatusServiceUnavailable:
		return CodeLocked
	default:
		return CodeInternal
	}
}

// sleep waits the backoff for the given retry attempt (1-based step) or
// returns the context error if it is cancelled first.
func (c *Client) sleep(ctx context.Context, attempt int) error {
	d := c.backoff(attempt)
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("coordclient: retry aborted: %w", ctx.Err())
	case <-t.C:
		return nil
	}
}

// backoff is full-jittered exponential: random in [0, min(MaxDelay,
// BaseDelay*2^(attempt-1))].
func (c *Client) backoff(attempt int) time.Duration {
	base := c.retry.BaseDelay
	if base <= 0 {
		base = DefaultRetryPolicy.BaseDelay
	}
	maxD := c.retry.MaxDelay
	if maxD <= 0 {
		maxD = DefaultRetryPolicy.MaxDelay
	}
	step := float64(base) * math.Pow(2, float64(attempt-1))
	if step > float64(maxD) {
		step = float64(maxD)
	}
	return time.Duration(c.jitter(int64(step)))
}

// jitter returns a uniform value in [0, n] using the nonce entropy source.
func (c *Client) jitter(n int64) int64 {
	if n <= 0 {
		return 0
	}
	var b [8]byte
	if _, err := io.ReadFull(c.rng, b[:]); err != nil {
		return n / 2
	}
	var v uint64
	for _, x := range b {
		v = v<<8 | uint64(x)
	}
	return int64(v % uint64(n+1))
}
