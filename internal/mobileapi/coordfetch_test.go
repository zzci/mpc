package mobileapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testMemberKeyHex = "0101010101010101010101010101010101010101010101010101010101010101"

// coordStub serves the two FetchTransactions endpoints. Member-auth is not
// re-verified here (coordclient's signing path is covered by the coordclient
// suite); the stub focuses on the api.md B3 / A3 response shapes and the
// error envelope so the assembly logic is what is under test.
func coordStub(t *testing.T, pendingBody string, status int, statusBody string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/groups/{groupId}/pending", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pendingBody))
	})
	mux.HandleFunc("GET /v1/requests/{requestId}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(statusBody))
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func reqJSON(url, requestID string) string {
	r := map[string]any{
		"coordBaseURL": url,
		"groupId":      "g1",
		"memberId":     "m1",
		"memberKeyHex": testMemberKeyHex,
	}
	if requestID != "" {
		r["requestId"] = requestID
	}
	b, _ := json.Marshal(r)
	return string(b)
}

func TestFetchTransactions_PendingAssembly(t *testing.T) {
	// One item whose proposerSig/metaHash do not verify: aFacts MUST be null
	// (the device never renders coord display fields as authoritative).
	pending := `{"serverTime":1700000000123,"items":[` +
		`{"version":1,"requestId":"r1","groupId":"g1","chain":"eth",` +
		`"unsignedTx":"AAAA","digest32":"` + strings.Repeat("A", 43) + `=",` +
		`"proposer":"","metaHash":"AA==","proposerSig":"AA==",` +
		`"status":"PENDING","remainingTTL":120}]}`
	srv := coordStub(t, pending, http.StatusOK, "")

	out, err := newTestSDK(t).FetchTransactions(reqJSON(srv.URL, ""))
	if err != nil {
		t.Fatalf("FetchTransactions: %v", err)
	}
	var resp struct {
		ServerTime int64 `json:"serverTime"`
		Items      []struct {
			RequestID    string          `json:"requestId"`
			Status       string          `json:"status"`
			RemainingTTL int64           `json:"remainingTTL"`
			AFacts       json.RawMessage `json:"aFacts"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal result: %v (%s)", err, out)
	}
	if resp.ServerTime != 1700000000123 {
		t.Errorf("serverTime not passed through: %d", resp.ServerTime)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("want 1 item, got %d", len(resp.Items))
	}
	it := resp.Items[0]
	if it.RequestID != "r1" || it.Status != "PENDING" || it.RemainingTTL != 120 {
		t.Errorf("item fields not passed through: %+v", it)
	}
	if string(it.AFacts) != "null" {
		t.Errorf("aFacts must be null for an unverifiable item, got %s", it.AFacts)
	}
}

func TestFetchTransactions_SingleStatus(t *testing.T) {
	srv := coordStub(t, "", http.StatusOK, `{"requestId":"r9","status":"RETURNED"}`)
	out, err := newTestSDK(t).FetchTransactions(reqJSON(srv.URL, "r9"))
	if err != nil {
		t.Fatalf("FetchTransactions: %v", err)
	}
	var resp struct {
		Items []struct {
			RequestID string `json:"requestId"`
			Status    string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].RequestID != "r9" || resp.Items[0].Status != "RETURNED" {
		t.Fatalf("single-status branch wrong: %s", out)
	}
}

func TestFetchTransactions_CoordErrorPassthrough(t *testing.T) {
	srv := coordStub(t, "", http.StatusNotFound,
		`{"error":{"code":"NOT_FOUND","message":"unknown requestId"}}`)
	_, err := newTestSDK(t).FetchTransactions(reqJSON(srv.URL, "missing"))
	if err == nil {
		t.Fatal("expected coord NOT_FOUND surfaced as error")
	}
	if !strings.Contains(err.Error(), "NOT_FOUND") {
		t.Fatalf("coord error not surfaced verbatim: %v", err)
	}
}

func TestFetchTransactions_NoKeyRejected(t *testing.T) {
	// No network: a missing member key is rejected before any coord call.
	s := newTestSDK(t)
	for _, rj := range []string{
		`{"coordBaseURL":"http://x","groupId":"g1","memberId":"m1"}`,
		`{"coordBaseURL":"http://x","groupId":"g1","memberId":"m1","memberKeyHex":"zz"}`,
		`{"coordBaseURL":"http://x","groupId":"g1","memberId":"m1","memberKeyHex":"0102"}`,
	} {
		if _, err := s.FetchTransactions(rj); err == nil {
			t.Fatalf("expected rejection for reqJSON %q", rj)
		}
	}
	if _, err := s.FetchTransactions(`not json`); err == nil {
		t.Fatal("expected invalid-reqJSON rejection")
	}
	if _, err := s.FetchTransactions(`{"groupId":"g1","memberId":"m1","memberKeyHex":"` + testMemberKeyHex + `"}`); err == nil {
		t.Fatal("expected missing-coordBaseURL rejection")
	}
}
