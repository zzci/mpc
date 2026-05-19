package coordclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// (B2 register-push-token removed with the single-fixed-webhook ruling
// 2026-05-19: coord holds no push tokens/credentials and no longer
// distinguishes fcm/apns; an external notification channel owns delivery.)

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
