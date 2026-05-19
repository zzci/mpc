package coord

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
)

// callbackSink delivers terminal status to the external business service
// (docs/design/contract/api.md A4). Result delivery is fixed webhook (user
// ruling 2026-05-19): it POSTs with bounded exponential-backoff retries
// until acknowledged or the terminal-timeout. The parked A4 long-poll
// waiter is still signaled separately (the request row already holds
// status/result). The result must always be returned (server.md C1).
type callbackSink struct {
	url    string
	client *http.Client
	log    *slog.Logger
}

// callbackBody is the A4 payload {requestId, status, RSV?, reason?} (api.md:28).
type callbackBody struct {
	RequestID string `json:"requestId"`
	Status    string `json:"status"`
	RSV       string `json:"rsv,omitempty"`    // base64, RETURNED only
	Reason    string `json:"reason,omitempty"` // FAILED/EXPIRED/REJECTED
}

// notifyTerminal reports a terminal transition. For webhook it retries with
// backoff in a bounded loop; the caller invokes this from a goroutine so a
// slow/unreachable external service never blocks the state machine.
func (s callbackSink) notifyTerminal(ctx context.Context, body callbackBody) {
	backoff := 500 * time.Millisecond
	const maxBackoff = 30 * time.Second
	deadline := time.Now().Add(2 * time.Minute)
	for attempt := 1; ; attempt++ {
		if err := s.postOnce(ctx, body); err == nil {
			return
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			s.log.Error("callback delivery gave up",
				"requestId", body.RequestID, "status", body.Status)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (s callbackSink) postOnce(ctx context.Context, body callbackBody) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("coord: marshal callback: %w", err)
	}
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, s.url, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("coord: build callback request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("coord: callback post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("coord: callback non-2xx: %d", resp.StatusCode)
	}
	return nil
}

// rsvLen is the {R,S,V} wire form: 65 bytes [V+27 || R(32) || S(32)], the
// btcec ecdsa.RecoverCompact layout produced by internal/mpc Signature.Compact
// (the device's signer output). coord re-encodes nothing; it only verifies.
const rsvLen = 65

// verifyRSV is the trust-free integrity gate of docs/design/contract/api.md:30 and
// the C8 "false result / forged RSV" defense: before returning a result coord
// recovers the public key from (digest32, R, S, V) and requires it to equal
// the group's ecdsa_pubkey. RecoverCompact succeeding with the matching key
// proves ECDSA(pub, digest32, R, S) holds, so a forged or mismatched RSV fails
// here and the request goes FAILED with no result leaked.
func verifyRSV(groupECDSAPub, digest32, rsv []byte) error {
	if len(digest32) != 32 {
		return fmt.Errorf("coord: digest32 must be 32 bytes")
	}
	if len(rsv) != rsvLen {
		return fmt.Errorf("coord: rsv must be %d bytes", rsvLen)
	}
	want, err := btcec.ParsePubKey(groupECDSAPub)
	if err != nil {
		return fmt.Errorf("coord: bad group pubkey: %w", err)
	}
	rec, _, err := ecdsa.RecoverCompact(rsv, digest32)
	if err != nil {
		return fmt.Errorf("coord: rsv recover: %w", err)
	}
	if !rec.IsEqual(want) {
		return fmt.Errorf("coord: rsv does not recover the group public key")
	}
	return nil
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// reportTerminal fires the mandatory external callback for a terminal
// transition and wakes any A4 long-poll waiter. For RETURNED it re-asserts the
// group-pubkey RSV gate (api.md:30 "verify before returning"): a result that
// fails the gate is never sent — defense-in-depth on top of the B7 check. The
// webhook POST runs in a goroutine so a slow external service never blocks the
// state machine; the row is already persisted so longpoll/A3 still serve it.
func (c *Coord) reportTerminal(ctx context.Context, requestID, status string, rsv []byte, reason string) {
	body := callbackBody{RequestID: requestID, Status: status}
	switch status {
	case stReturned:
		r, err := c.db.loadRequest(ctx, requestID)
		if err != nil {
			c.log.Error("reportTerminal load", "requestId", requestID, "err", err.Error())
			return
		}
		use := rsv
		if len(use) == 0 {
			use = r.ResultRSV
		}
		g, err := c.db.group(ctx, r.GroupID)
		if err != nil {
			c.log.Error("reportTerminal group", "requestId", requestID, "err", err.Error())
			return
		}
		if err := verifyRSV(g.ECDSAPubkey, r.Digest32, use); err != nil {
			c.log.Error("reportTerminal rsv gate failed; not returning result",
				"requestId", requestID)
			return
		}
		body.RSV = b64(use)
	default:
		body.Reason = reason
	}
	go c.callback.notifyTerminal(context.WithoutCancel(ctx), body)
	c.results.signal(requestID)
}
