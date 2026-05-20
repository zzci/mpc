package walletcli

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// uiReq drives one UI request: optional bearer, optional cookie sid, optional
// form body. Returns the recorder for assertions.
func uiReq(t *testing.T, h *httpServer, method, path, bearer, sid, body string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	mux := http.NewServeMux()
	h.ui.register(mux)
	var r *http.Request
	if body != "" {
		r = httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequestWithContext(context.Background(), method, path, nil)
	}
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	if sid != "" {
		r.AddCookie(&http.Cookie{Name: uiCookieName, Value: sid})
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec, rec.Body.String()
}

// TestUIAuthLoopback: when no token is configured the panel is open
// (loopback-only mode) and the index renders without a session.
func TestUIAuthLoopback(t *testing.T) {
	h := testServer(t, "")
	w, body := uiReq(t, h, "GET", "/ui", "", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("index code = %d, want 200", w.Code)
	}
	if !strings.Contains(body, "wallet-cli") {
		t.Fatalf("index missing branding: %s", body[:min(len(body), 120)])
	}
	w, _ = uiReq(t, h, "GET", "/ui/login", "", "", "")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/ui" {
		t.Fatalf("/ui/login w/o token: code=%d loc=%s", w.Code, w.Header().Get("Location"))
	}
}

// TestUIAuthTokenGate: with a token configured every data page demands
// either a cookie or a Bearer; browsers without either redirect to /login.
func TestUIAuthTokenGate(t *testing.T) {
	h := testServer(t, "s3cret")
	w, _ := uiReq(t, h, "GET", "/ui", "", "", "")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/ui/login" {
		t.Fatalf("no creds: code=%d loc=%s", w.Code, w.Header().Get("Location"))
	}
	w, _ = uiReq(t, h, "GET", "/ui", "wrong", "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong bearer: code=%d", w.Code)
	}
	w, _ = uiReq(t, h, "GET", "/ui", "s3cret", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("good bearer: code=%d", w.Code)
	}
}

// TestUISessionFlow: login form posts the token, sets a cookie, redirects to
// /ui; subsequent requests carry the cookie. Logout drops the cookie.
func TestUISessionFlow(t *testing.T) {
	h := testServer(t, "s3cret")
	w, body := uiReq(t, h, "GET", "/ui/login", "", "", "")
	if w.Code != http.StatusOK || !strings.Contains(body, `action="/ui/session"`) {
		t.Fatalf("login form missing form action: code=%d body=%s", w.Code, body[:min(len(body), 200)])
	}
	w, _ = uiReq(t, h, "POST", "/ui/session", "", "", "token=wrong")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/ui/login?e=1" {
		t.Fatalf("wrong token: code=%d loc=%s", w.Code, w.Header().Get("Location"))
	}
	w, _ = uiReq(t, h, "POST", "/ui/session", "", "", "token=s3cret")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/ui" {
		t.Fatalf("good token: code=%d loc=%s", w.Code, w.Header().Get("Location"))
	}
	cookie := w.Result().Cookies()
	if len(cookie) != 1 || cookie[0].Name != uiCookieName || cookie[0].Value == "" {
		t.Fatalf("session cookie not set: %#v", cookie)
	}
	if !cookie[0].HttpOnly || cookie[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie weak: HttpOnly=%v SameSite=%d", cookie[0].HttpOnly, cookie[0].SameSite)
	}
	sid := cookie[0].Value
	w, _ = uiReq(t, h, "GET", "/ui", "", sid, "")
	if w.Code != http.StatusOK {
		t.Fatalf("with sid cookie: code=%d", w.Code)
	}
	w, _ = uiReq(t, h, "POST", "/ui/logout", "", sid, "")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/ui/login" {
		t.Fatalf("logout: code=%d loc=%s", w.Code, w.Header().Get("Location"))
	}
	w, _ = uiReq(t, h, "GET", "/ui", "", sid, "")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("post-logout: code=%d (cookie should be invalid)", w.Code)
	}
}

// TestUIAssets: static htmx + tw.css served with correct content-type and
// non-empty body, no auth (assets are loaded before the session is built).
func TestUIAssets(t *testing.T) {
	h := testServer(t, "s3cret")
	for path, ct := range map[string]string{
		"/ui/assets/htmx.min.js": "text/javascript",
		"/ui/assets/tw.css":      "text/css",
	} {
		w, body := uiReq(t, h, "GET", path, "", "", "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s code = %d", path, w.Code)
		}
		if w.Header().Get("Content-Type") != ct {
			t.Fatalf("%s ctype = %s want %s", path, w.Header().Get("Content-Type"), ct)
		}
		if body == "" {
			t.Fatalf("%s empty body", path)
		}
	}
}

// TestUIPendingList: index pending count and the list page reflect what is
// in httpServer.pending; sign detail renders the WYSIWYS fields.
func TestUIPendingList(t *testing.T) {
	h := testServer(t, "")
	h.pending["abc123"] = &pendingSign{
		decoded: signDecode{
			aFactsJSON:   `{"to":"0x01","value":"1"}`,
			bInfoJSON:    `{"label":"payroll"}`,
			mismatchJSON: `[]`,
		},
		createdAt: time.Now(),
	}
	w, body := uiReq(t, h, "GET", "/ui", "", "", "")
	if w.Code != http.StatusOK || !strings.Contains(body, ">1<") {
		t.Fatalf("index pending count missing (looking for >1< near pending counter): code=%d", w.Code)
	}
	w, body = uiReq(t, h, "GET", "/ui/sign", "", "", "")
	if w.Code != http.StatusOK || !strings.Contains(body, "abc123") {
		t.Fatalf("sign list missing id: code=%d", w.Code)
	}
	w, body = uiReq(t, h, "GET", "/ui/sign/abc123", "", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("sign detail code=%d", w.Code)
	}
	for _, want := range []string{"0x01", "payroll", "no A/B discrepancies"} {
		if !strings.Contains(body, want) {
			t.Fatalf("sign detail missing %q: body=%s", want, body[:min(len(body), 400)])
		}
	}
	// hx-post wiring: detail must reference both approve and reject endpoints.
	for _, want := range []string{`hx-post="/ui/sign/abc123/approve"`, `hx-post="/ui/sign/abc123/reject"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("sign detail missing hx-post: %q", want)
		}
	}
}

// TestUISignDetail404: an unknown sign id returns 404 HTML.
func TestUISignDetail404(t *testing.T) {
	h := testServer(t, "")
	w, _ := uiReq(t, h, "GET", "/ui/sign/nope", "", "", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown id code=%d", w.Code)
	}
}

// TestUIMismatchFlag: a non-empty mismatch JSON triggers the warning badge in
// the detail view (UX safety: operator sees flagged before approving).
func TestUIMismatchFlag(t *testing.T) {
	h := testServer(t, "")
	h.pending["m1"] = &pendingSign{
		decoded: signDecode{
			aFactsJSON: `{}`, bInfoJSON: `{}`,
			mismatchJSON: `[{"field":"to"}]`,
		},
		createdAt: time.Now(),
	}
	w, body := uiReq(t, h, "GET", "/ui/sign/m1", "", "", "")
	if w.Code != http.StatusOK || !strings.Contains(body, "flagged") {
		t.Fatalf("mismatch flag missing: code=%d", w.Code)
	}
}

// TestUIResolveApproveReject exercises the htmx approve + reject paths
// against a fake pendingSign whose cb.done fires immediately. It verifies the
// pending row is popped, the cookie-gated route reaches the handler, and the
// inline result fragment swaps in.
func TestUIResolveApproveReject(t *testing.T) {
	for _, action := range []string{"approve", "reject"} {
		t.Run(action, func(t *testing.T) {
			h := testServer(t, "")
			cb := newSignCB(discardWriter{})
			h.pending["X"] = &pendingSign{
				ss: stubSession{}, cb: cb,
				decoded: signDecode{aFactsJSON: "{}", bInfoJSON: "{}", mismatchJSON: "[]"},
			}
			cb.done <- outcome{ok: action == "approve", payload: "deadbeef", code: "ECODE", msg: "msg"}
			form := url.Values{}.Encode()
			r := httptest.NewRequestWithContext(context.Background(), "POST", "/ui/sign/X/"+action, strings.NewReader(form))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("HX-Request", "true")
			rec := httptest.NewRecorder()
			mux := http.NewServeMux()
			h.ui.register(mux)
			mux.ServeHTTP(rec, r)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s code=%d body=%s", action, rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, `id="sign-decision"`) {
				t.Fatalf("%s missing swap target: %s", action, body[:min(len(body), 300)])
			}
			if action == "approve" {
				if !strings.Contains(body, "signed") || !strings.Contains(body, "deadbeef") {
					t.Fatalf("approve fragment: %s", body)
				}
			} else {
				if !strings.Contains(body, "rejected") {
					t.Fatalf("reject fragment: %s", body)
				}
			}
			if _, still := h.pending["X"]; still {
				t.Fatalf("%s did not pop pending", action)
			}
		})
	}
}

// TestUIImportFormNoPassphrase: the form page renders the warning when
// $MPC_WALLET_PASSPHRASE is unset and disables the inputs.
func TestUIImportFormNoPassphrase(t *testing.T) {
	t.Setenv(passphraseEnv, "")
	h := testServer(t, "")
	w, body := uiReq(t, h, "GET", "/ui/import", "", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("import form code=%d", w.Code)
	}
	for _, want := range []string{"not set", "disabled"} {
		if !strings.Contains(body, want) {
			t.Fatalf("import form missing %q", want)
		}
	}
}

// TestUIImportFormPassphraseSet: with $MPC_WALLET_PASSPHRASE set the form is
// enabled (no `disabled` attribute on the file input).
func TestUIImportFormPassphraseSet(t *testing.T) {
	t.Setenv(passphraseEnv, "pw")
	h := testServer(t, "")
	w, body := uiReq(t, h, "GET", "/ui/import", "", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d", w.Code)
	}
	if strings.Contains(body, `name="backup" required disabled`) {
		t.Fatalf("file input should not be disabled when passphrase configured")
	}
	if !strings.Contains(body, `enctype="multipart/form-data"`) {
		t.Fatalf("form missing multipart enctype")
	}
}

// TestUIImportNoPassphrasePost: POSTing import without the env passphrase
// returns the error result inline (412 spirit — the handler renders a
// failure card; status stays 200 so htmx can swap it).
func TestUIImportNoPassphrasePost(t *testing.T) {
	t.Setenv(passphraseEnv, "")
	h := testServer(t, "")
	form, contentType := multipartBackup(t, "backup", "file.bin", []byte("blob"))
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/ui/import", form)
	r.Header.Set("Content-Type", contentType)
	r.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	h.ui.register(mux)
	mux.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "import failed") || !strings.Contains(body, passphraseEnv) {
		t.Fatalf("missing failure fragment: %s", body)
	}
}

// TestUIImportNoFile: POST without the multipart "backup" field surfaces a
// clear missing-file error.
func TestUIImportNoFile(t *testing.T) {
	t.Setenv(passphraseEnv, "pw")
	h := testServer(t, "")
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/ui/import",
		strings.NewReader(""))
	r.Header.Set("Content-Type", "multipart/form-data; boundary=xxxxxx")
	r.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	h.ui.register(mux)
	mux.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "import failed") {
		t.Fatalf("missing failure fragment: %s", rec.Body.String())
	}
}

// TestUIImportBadBlob: a malformed backup blob is rejected by the SDK and the
// UI surfaces the underlying error (not silently). The blob never produces a
// committee mutation here.
func TestUIImportBadBlob(t *testing.T) {
	t.Setenv(passphraseEnv, "pw")
	h := testServer(t, "")
	form, contentType := multipartBackup(t, "backup", "garbage.bin", []byte("not-a-real-backup"))
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/ui/import", form)
	r.Header.Set("Content-Type", contentType)
	r.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	h.ui.register(mux)
	mux.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "import failed") {
		t.Fatalf("missing failure fragment: %s", rec.Body.String())
	}
}

// multipartBackup builds a multipart/form-data body with one file field. It
// is small enough that an io.Pipe / mime/multipart.Writer + goroutine plumb
// would be overkill; a buffer is fine for the test sizes here.
func multipartBackup(t *testing.T, field, filename string, body []byte) (io.Reader, string) {
	t.Helper()
	b := &strings.Builder{}
	w := multipart.NewWriter(b)
	fw, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(body); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return strings.NewReader(b.String()), w.FormDataContentType()
}

// stubSession is an inert signSession: the test pre-loads cb.done so the
// handler's terminal-wait returns immediately and the Approve/Reject methods
// have no real MPC state machine to drive.
type stubSession struct{}

func (stubSession) Approve() {}
func (stubSession) Reject()  {}

// discardWriter is a no-op io.Writer for the signCB progress channel.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
