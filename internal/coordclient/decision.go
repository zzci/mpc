package coordclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Decision is a member's B4 verdict on a pending request (api.md:49).
type Decision string

const (
	// DecisionApproved approves the request for MPC signing.
	DecisionApproved Decision = "approved"
	// DecisionRejected rejects it; once t members can no longer reach the
	// threshold the request becomes REJECTED (api.md:50).
	DecisionRejected Decision = "rejected"
)

// decisionBody mirrors internal/server/coord decisionBody (api.md:49).
type decisionBody struct {
	MemberID string `json:"memberId"`
	Decision string `json:"decision"`
}

// Decide submits an approve/reject for requestID
// (api.md B4, POST /v1/requests/{requestId}/decision → 200 {status}).
// Only a PENDING, unexpired request is accepted: a 410 maps to ErrExpired and
// a 409 to ErrStateConflict (both terminal for this request). The returned
// string is the resulting coord status (e.g. PENDING/DISPATCHED/REJECTED).
//
// The signed coord auth method is "B4:decision:<decision>" so the verb binds
// the decision value (matching internal/server/coord api.go hDecision).
func (c *Client) Decide(ctx context.Context, requestID string, d Decision) (string, error) {
	if d != DecisionApproved && d != DecisionRejected {
		return "", fmt.Errorf("coordclient: decision must be approved or rejected, got %q", d)
	}
	body, err := json.Marshal(decisionBody{MemberID: c.memberID, Decision: string(d)})
	if err != nil {
		return "", fmt.Errorf("coordclient: marshal decision: %w", err)
	}
	raw, err := c.do(ctx, request{
		method:   http.MethodPost,
		path:     "/v1/requests/" + requestID + "/decision",
		body:     body,
		authVerb: "B4:decision:" + string(d),
	})
	if err != nil {
		return "", err
	}
	var out struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("coordclient: decode decision response: %w", err)
	}
	return out.Status, nil
}
