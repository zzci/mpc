package coordclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// resultBody mirrors internal/server/coord resultBody (api.md:61). RSV is
// emitted as a JSON base64 string (Go encoding/json []byte), matching the
// server's resultBody.RSV []byte field.
type resultBody struct {
	MemberID string `json:"memberId"`
	RSV      []byte `json:"rsv"`
}

// ReportResult uploads the final {R,S,V} for requestID
// (api.md B7, POST /v1/requests/{requestId}/result → 200 {status}). It is
// called by the single signer designated to report; coord verifies the result
// against the group public key (valid → SIGNED→RETURNED, invalid → FAILED) and
// is idempotent across duplicate reports, taking the first valid one
// (api.md:62-63). rsv is the raw 65-byte R‖S‖V; the returned string is the
// resulting coord status. A 410 maps to ErrExpired (terminal).
func (c *Client) ReportResult(ctx context.Context, requestID string, rsv []byte) (string, error) {
	if len(rsv) == 0 {
		return "", fmt.Errorf("coordclient: rsv must not be empty")
	}
	body, err := json.Marshal(resultBody{MemberID: c.memberID, RSV: rsv})
	if err != nil {
		return "", fmt.Errorf("coordclient: marshal result: %w", err)
	}
	raw, err := c.do(ctx, request{
		method:   http.MethodPost,
		path:     "/v1/requests/" + requestID + "/result",
		body:     body,
		authVerb: "B7:result",
	})
	if err != nil {
		return "", err
	}
	var out struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("coordclient: decode result response: %w", err)
	}
	return out.Status, nil
}
