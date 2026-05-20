package mobileapi

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// AD-4 SDK.FetchXpub: gomobile-flat delegate to coordclient.Xpub. The mobile
// host pipeline reuses the FetchTransactions reqJSON shape so the test stub
// here exercises only the B8 wire shape and the reqJSON validation; the
// member-auth + retry contract is covered in the coordclient suite.

func xpubCoordStub(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/groups/{groupId}/xpub", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func xpubReqJSON(url string) string {
	r := map[string]any{
		"coordBaseURL": url,
		"groupId":      "g1",
		"memberId":     "m1",
		"memberKeyHex": testMemberKeyHex,
	}
	b, _ := json.Marshal(r)
	return string(b)
}

func TestFetchXpub_HappyPath(t *testing.T) {
	wantPub := bytes.Repeat([]byte{0x02, 0xaa, 0xbb}, 11) // 33 bytes
	wantCC := bytes.Repeat([]byte{0x42}, 32)
	body := `{"ecdsaPubkeyHex":"` + hex.EncodeToString(wantPub) +
		`","chaincodeHex":"` + hex.EncodeToString(wantCC) + `"}`
	srv := xpubCoordStub(t, http.StatusOK, body)

	out, err := newTestSDK(t).FetchXpub(xpubReqJSON(srv.URL))
	if err != nil {
		t.Fatalf("FetchXpub: %v", err)
	}
	var got struct {
		ECDSAPubkeyHex string `json:"ecdsaPubkeyHex"`
		ChaincodeHex   string `json:"chaincodeHex"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, out)
	}
	if got.ECDSAPubkeyHex != hex.EncodeToString(wantPub) {
		t.Fatalf("pubkey hex mismatch: got %s", got.ECDSAPubkeyHex)
	}
	if got.ChaincodeHex != hex.EncodeToString(wantCC) {
		t.Fatalf("chaincode hex mismatch: got %s", got.ChaincodeHex)
	}
}

func TestFetchXpub_LegacyErrorPassthrough(t *testing.T) {
	srv := xpubCoordStub(t, http.StatusConflict,
		`{"error":{"code":"LEGACY_NO_HD","message":"group predates HD; multi-group remains the multi-address path"}}`)
	_, err := newTestSDK(t).FetchXpub(xpubReqJSON(srv.URL))
	if err == nil {
		t.Fatal("expected LEGACY_NO_HD error")
	}
	if !strings.Contains(err.Error(), "LEGACY_NO_HD") {
		t.Fatalf("coord error not surfaced verbatim: %v", err)
	}
}

func TestFetchXpub_NoKeyRejected(t *testing.T) {
	// No network: a missing / malformed member key is rejected before any call.
	s := newTestSDK(t)
	for _, rj := range []string{
		`{"coordBaseURL":"http://x","groupId":"g1","memberId":"m1"}`,
		`{"coordBaseURL":"http://x","groupId":"g1","memberId":"m1","memberKeyHex":"zz"}`,
		`{"coordBaseURL":"http://x","groupId":"g1","memberId":"m1","memberKeyHex":"0102"}`,
	} {
		if _, err := s.FetchXpub(rj); err == nil {
			t.Fatalf("expected rejection for reqJSON %q", rj)
		}
	}
	if _, err := s.FetchXpub(`not json`); err == nil {
		t.Fatal("expected invalid-reqJSON rejection")
	}
	if _, err := s.FetchXpub(`{"groupId":"g1","memberId":"m1","memberKeyHex":"` + testMemberKeyHex + `"}`); err == nil {
		t.Fatal("expected missing-coordBaseURL rejection")
	}
}
