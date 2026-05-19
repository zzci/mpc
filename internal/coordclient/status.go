package coordclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// RequestStatus is the api.md A3 single-request view
// (GET /v1/requests/{requestId} → {requestId,status,fail_reason?,result?}).
// Only the fields a member device needs for App listing/detail are kept; the
// result RSV is intentionally omitted (a member never displays the raw
// signature, and FetchTransactions never surfaces it — docs/design/mcp/sdk.md
// §2.1).
type RequestStatus struct {
	RequestID  string `json:"requestId"`
	Status     string `json:"status"`
	FailReason string `json:"failReason,omitempty"`
}

// Status fetches the current status of a single request (api.md A3,
// GET /v1/requests/{requestId}). It signs the request with the member
// identity key (B1) and reuses the shared retry / api.md error mapping: 404
// maps to ErrNotFound, 410 to ErrExpired, 503 LOCKED is retried with backoff
// and then surfaced verbatim. requestID must not be empty.
//
// Note (docs/design/mcp/sdk.md §2.1, "via coord member API"): A3 is reused for
// the single-request branch with the member-signed client and without any
// coord server change, as ruled. The envelope is NOT part of the A3 payload,
// so the single-request branch carries status only; the device-side A-zone
// recompute applies to the B3 pending branch where the envelope is present.
func (c *Client) Status(ctx context.Context, requestID string) (*RequestStatus, error) {
	if requestID == "" {
		return nil, fmt.Errorf("coordclient: requestId must not be empty")
	}
	raw, err := c.do(ctx, request{
		method:   http.MethodGet,
		path:     "/v1/requests/" + requestID,
		authVerb: "A3:status",
	})
	if err != nil {
		return nil, err
	}
	var body struct {
		RequestID  string `json:"requestId"`
		Status     string `json:"status"`
		FailReason string `json:"fail_reason"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("coordclient: decode status: %w", err)
	}
	return &RequestStatus{
		RequestID:  body.RequestID,
		Status:     body.Status,
		FailReason: body.FailReason,
	}, nil
}
