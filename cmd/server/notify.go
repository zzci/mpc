package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/zzci/mpc/internal/server/coord"
)

// webhookNotifier implements coord.Notifier as the single fixed
// notification webhook (user ruling 2026-05-19): coord POSTs a
// dispatch-wake event to one URL and an external notification channel
// translates/delivers it (FCM/APNS/etc.). coord holds no push credentials
// and never distinguishes platforms.
//
// Delivery is best-effort and strictly non-blocking: NotifyDispatch
// returns immediately and the POST runs in a detached goroutine with a
// bounded timeout, so a slow/unreachable notification channel never
// blocks the quorum engine (the long-poll dispatch channel remains the
// authoritative wake path; this is an out-of-band nudge).
type webhookNotifier struct {
	url string
	// secret/apiKey are the dual-mode anti-forgery callback-auth credentials
	// (coord.notify.{secret,api_key}, user ruling 2026-05-19): same scheme
	// as the result webhook — secret → HMAC-SHA256 signature (preferred),
	// else apiKey → Bearer.
	secret string
	apiKey string
	client *http.Client
	log    *slog.Logger
}

func newWebhookNotifier(url, secret, apiKey string, log *slog.Logger) *webhookNotifier {
	return &webhookNotifier{
		url:    url,
		secret: secret,
		apiKey: apiKey,
		client: &http.Client{Timeout: 10 * time.Second},
		log:    log,
	}
}

var _ coord.Notifier = (*webhookNotifier)(nil)

// dispatchEvent is the notification payload: the group whose signers
// should be woken and the selected signer member IDs. It carries no MPC
// content (coord never sees shares) — only routing metadata for the
// external channel.
type dispatchEvent struct {
	Event   string   `json:"event"`
	GroupID string   `json:"groupId"`
	Signers []string `json:"signers"`
}

func (n *webhookNotifier) NotifyDispatch(ctx context.Context, groupID string, signers []string) {
	body, err := json.Marshal(dispatchEvent{Event: "dispatch", GroupID: groupID, Signers: signers})
	if err != nil {
		n.log.Error("notify marshal", "groupId", groupID, "err", err.Error())
		return
	}
	go func() {
		// Detach from the caller's request context (it may be cancelled as
		// soon as dispatch returns) but keep a hard upper bound.
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(rctx, http.MethodPost, n.url, bytes.NewReader(body))
		if err != nil {
			n.log.Error("notify build request", "groupId", groupID, "err", err.Error())
			return
		}
		req.Header.Set("Content-Type", "application/json")
		// Same anti-forgery callback auth as the result webhook over the
		// exact bytes sent (user ruling 2026-05-19).
		coord.ApplyWebhookAuth(req, body, time.Now(), n.secret, n.apiKey)
		resp, err := n.client.Do(req)
		if err != nil {
			n.log.Warn("notify webhook post failed", "groupId", groupID, "err", err.Error())
			return
		}
		_ = resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			n.log.Warn("notify webhook non-2xx", "groupId", groupID, "status", resp.StatusCode)
		}
	}()
}
