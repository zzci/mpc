package walletcli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zzci/mpc/sdk"
)

func testServer(t *testing.T, token string) *httpServer {
	t.Helper()
	s, err := sdk.NewSDK(t.TempDir())
	if err != nil {
		t.Fatalf("NewSDK: %v", err)
	}
	hs := &httpServer{
		sdk:     s,
		token:   token,
		pending: map[string]*pendingSign{},
		now:     time.Now,
	}
	hs.ui = newUI(hs)
	return hs
}

func TestIsLoopbackAddr(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:8787": true, "[::1]:9": true, "localhost:1": true,
		"0.0.0.0:8787": false, ":8787": false, "10.0.0.5:80": false, "bad": false,
	}
	for in, want := range cases {
		if got := isLoopbackAddr(in); got != want {
			t.Errorf("isLoopbackAddr(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestServeHTTPNonLoopbackNeedsToken(t *testing.T) {
	t.Setenv(httpTokenEnv, "")
	t.Setenv(keystoreEnv, t.TempDir())
	if rc := serveHTTP([]string{"--listen", "0.0.0.0:8799"}); rc != 2 {
		t.Fatalf("non-loopback w/o token rc = %d, want 2", rc)
	}
}

func TestGuardToken(t *testing.T) {
	h := testServer(t, "s3cret")
	rec := httptest.NewRecorder()
	h.guard(h.health)(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/health", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: code = %d, want 401", rec.Code)
	}
	rec = httptest.NewRecorder()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/health", nil)
	r.Header.Set("Authorization", "Bearer s3cret")
	h.guard(h.health)(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("good token: code = %d, want 200", rec.Code)
	}
}

func TestHealthVersionOpenOnLoopback(t *testing.T) {
	h := testServer(t, "") // no token
	rec := httptest.NewRecorder()
	h.guard(h.versionH)(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/version", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "wallet-cli") {
		t.Fatalf("version = %d %q", rec.Code, rec.Body.String())
	}
}

func TestMethodEnforced(t *testing.T) {
	h := testServer(t, "")
	rec := httptest.NewRecorder()
	h.keygenH(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/keygen", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET keygen = %d, want 405", rec.Code)
	}
}

func TestBadJSONBody(t *testing.T) {
	h := testServer(t, "")
	rec := httptest.NewRecorder()
	h.keygenH(rec, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/keygen", strings.NewReader("{not json")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d, want 400", rec.Code)
	}
}

func TestPassphraseGuardHTTP(t *testing.T) {
	t.Setenv(passphraseEnv, "")
	h := testServer(t, "")
	rec := httptest.NewRecorder()
	h.keygenH(rec, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/keygen", strings.NewReader(`{"Threshold":1,"Parties":3}`)))
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("missing passphrase = %d, want 412", rec.Code)
	}
}

func TestImportBadBase64(t *testing.T) {
	t.Setenv(passphraseEnv, "pw")
	h := testServer(t, "")
	rec := httptest.NewRecorder()
	h.importH(rec, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/import", strings.NewReader(`{"blob":"@@@"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad base64 = %d, want 400", rec.Code)
	}
}

func TestSignMissingStart(t *testing.T) {
	h := testServer(t, "")
	rec := httptest.NewRecorder()
	h.signH(rec, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/sign", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing start = %d, want 400", rec.Code)
	}
}

func TestSignDecisionUnknownID(t *testing.T) {
	h := testServer(t, "")
	rec := httptest.NewRecorder()
	h.signDecisionH(rec, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/sign/deadbeef/approve", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown sign id = %d, want 404", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.signDecisionH(rec, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/sign/x/bogus", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("bad action = %d, want 404", rec.Code)
	}
}
