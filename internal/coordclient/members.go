package coordclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Platform is the push transport a member device registers (api.md B2).
type Platform string

const (
	// PlatformFCM is Android Firebase Cloud Messaging.
	PlatformFCM Platform = "fcm"
	// PlatformAPNs is Apple Push Notification service.
	PlatformAPNs Platform = "apns"
)

// pushBody mirrors internal/server/coord pushBody (api.md:39). The body is the
// signed params for B2; the member signature lives in the X-Member-* headers,
// so no body `sig` field is sent (the server reads it from headers).
type pushBody struct {
	GroupID  string `json:"groupId"`
	MemberID string `json:"memberId"`
	Platform string `json:"platform"`
	Token    string `json:"token"`
}

// RegisterPush registers (or replaces) this member's push token
// (api.md B2, PUT /v1/members/self/push → 204). platform must be FCM or APNs.
func (c *Client) RegisterPush(ctx context.Context, platform Platform, token string) error {
	if platform != PlatformFCM && platform != PlatformAPNs {
		return fmt.Errorf("coordclient: platform must be fcm or apns, got %q", platform)
	}
	body, err := json.Marshal(pushBody{
		GroupID:  c.groupID,
		MemberID: c.memberID,
		Platform: string(platform),
		Token:    token,
	})
	if err != nil {
		return fmt.Errorf("coordclient: marshal push: %w", err)
	}
	_, err = c.do(ctx, request{
		method:   http.MethodPut,
		path:     "/v1/members/self/push",
		body:     body,
		authVerb: "B2:push",
	})
	return err
}

// heartbeatBody mirrors internal/server/coord heartbeatBody (api.md:53).
type heartbeatBody struct {
	GroupID     string `json:"groupId"`
	MemberID    string `json:"memberId"`
	RelayPeerID string `json:"relayPeerID"`
}

// Heartbeat reports member connectivity so coord's online set can drive quorum
// (api.md B5, POST /v1/members/self/heartbeat → 204). relayPeerID is the
// member's libp2p relay peer ID; coord does not depend on relay self-reporting
// (protocol.md §4).
func (c *Client) Heartbeat(ctx context.Context, relayPeerID string) error {
	body, err := json.Marshal(heartbeatBody{
		GroupID:     c.groupID,
		MemberID:    c.memberID,
		RelayPeerID: relayPeerID,
	})
	if err != nil {
		return fmt.Errorf("coordclient: marshal heartbeat: %w", err)
	}
	_, err = c.do(ctx, request{
		method:   http.MethodPost,
		path:     "/v1/members/self/heartbeat",
		body:     body,
		authVerb: "B5:heartbeat",
	})
	return err
}
