package walletcli

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// wallet-cli-ui: an htmx + tailwindcss server-side-rendered inspection panel
// for the `cli serve` HTTP service. It mirrors the read-only stance of
// admin-ui (internal/server/admin/ui.go) but only exposes operations the
// operator can already perform through /v1/*: pending-sign list + WYSIWYS
// approve/reject. Destructive operations (keygen/reshare/import/export) stay
// JSON-only — the panel never initiates them.
//
// Auth model: when $MPC_WALLET_HTTP_TOKEN is set the panel requires a session
// cookie (acquired by POSTing the token to /ui/session) OR a Bearer token on
// every request — identical to the JSON guard. When the token is unset, the
// JSON guard already allows unauthenticated access (loopback-only by the
// serveHTTP fail-closed check) so the panel matches that behavior and skips
// the login flow entirely. The cookie carries only an opaque session id; the
// token is never reflected back to the browser.

//go:embed uiassets/htmx.min.js uiassets/tw.css uiassets/templates/*.tmpl
var uiFS embed.FS

const (
	uiCookieName = "wallet_ui_sid"
	uiPathPrefix = "/ui"
	uiSessionTTL = 30 * time.Minute
)

var uiFuncs = template.FuncMap{
	"short": func(s string) string {
		if len(s) <= 16 {
			return s
		}
		return s[:8] + "…" + s[len(s)-6:]
	},
	"sinceMS": func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return time.Since(t).Truncate(time.Second).String()
	},
	"prettyJSON": func(raw string) string {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return ""
		}
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return raw
		}
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return raw
		}
		return string(b)
	},
}

var uiTmpl = template.Must(
	template.New("wallet-ui").Funcs(uiFuncs).ParseFS(uiFS, "uiassets/templates/*.tmpl"),
)

// uiSessions is an in-memory opaque-session table for the panel. The stored
// value is a 256-bit random id, never the token. Sessions are TTL'd and the
// table never persists across restarts.
type uiSessions struct {
	mu  sync.Mutex
	m   map[string]time.Time
	now func() time.Time
}

func newUISessions(now func() time.Time) *uiSessions {
	return &uiSessions{m: make(map[string]time.Time), now: now}
}

func (u *uiSessions) create() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("wallet-ui: session id: %w", err)
	}
	sid := hex.EncodeToString(b[:])
	u.mu.Lock()
	u.m[sid] = u.now().Add(uiSessionTTL)
	u.mu.Unlock()
	return sid, nil
}

func (u *uiSessions) valid(sid string) bool {
	if sid == "" {
		return false
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	exp, ok := u.m[sid]
	if !ok {
		return false
	}
	if u.now().After(exp) {
		delete(u.m, sid)
		return false
	}
	return true
}

func (u *uiSessions) drop(sid string) {
	u.mu.Lock()
	delete(u.m, sid)
	u.mu.Unlock()
}

// uiHandler is the panel handler set, parameterized by the httpServer it
// inspects. It owns its own session table so multiple servers (tests) never
// share state.
type uiHandler struct {
	s    *httpServer
	sess *uiSessions
}

func newUI(s *httpServer) *uiHandler {
	now := s.now
	if now == nil {
		now = time.Now
	}
	return &uiHandler{s: s, sess: newUISessions(now)}
}

func (h *uiHandler) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /ui/assets/htmx.min.js", h.asset("uiassets/htmx.min.js", "text/javascript"))
	mux.HandleFunc("GET /ui/assets/tw.css", h.asset("uiassets/tw.css", "text/css"))

	mux.HandleFunc("GET /ui/login", h.hLogin)
	mux.HandleFunc("POST /ui/session", h.hSession)
	mux.HandleFunc("POST /ui/logout", h.hLogout)

	mux.Handle("GET /ui", h.auth(http.HandlerFunc(h.hIndex)))
	mux.Handle("GET /ui/", h.auth(http.HandlerFunc(h.hIndex)))
	mux.Handle("GET /ui/sign", h.auth(http.HandlerFunc(h.hSignList)))
	mux.Handle("GET /ui/sign/{id}", h.auth(http.HandlerFunc(h.hSignDetail)))
	mux.Handle("POST /ui/sign/{id}/approve", h.auth(http.HandlerFunc(h.hSignApprove)))
	mux.Handle("POST /ui/sign/{id}/reject", h.auth(http.HandlerFunc(h.hSignReject)))
}

// authNeeded reports whether the panel currently requires a session/bearer.
// It mirrors the JSON guard: no token configured → no auth (loopback-only).
func (h *uiHandler) authNeeded() bool { return h.s.token != "" }

// auth gates every data-page route. Cookie or Bearer satisfy the gate; a
// browser without either is redirected to /ui/login (a Bearer caller gets
// 401 instead so automation surfaces the failure).
func (h *uiHandler) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.authNeeded() {
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie(uiCookieName); err == nil && h.sess.valid(c.Value) {
			next.ServeHTTP(w, r)
			return
		}
		if h.bearerOK(r) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			h.fail(w, r, http.StatusUnauthorized, "invalid token")
			return
		}
		http.Redirect(w, r, uiPathPrefix+"/login", http.StatusSeeOther)
	})
}

func (h *uiHandler) bearerOK(r *http.Request) bool {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tok == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(tok), []byte(h.s.token)) == 1
}

func (h *uiHandler) asset(name, ctype string) http.HandlerFunc {
	body, _ := uiFS.ReadFile(name)
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	}
}

// --- auth pages ---------------------------------------------------------

func (h *uiHandler) hLogin(w http.ResponseWriter, r *http.Request) {
	if !h.authNeeded() {
		http.Redirect(w, r, uiPathPrefix, http.StatusSeeOther)
		return
	}
	h.render(w, r, "login.tmpl", map[string]any{"Error": r.URL.Query().Get("e") != ""})
}

func (h *uiHandler) hSession(w http.ResponseWriter, r *http.Request) {
	if !h.authNeeded() {
		http.Redirect(w, r, uiPathPrefix, http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, http.StatusBadRequest, "malformed form")
		return
	}
	tok := r.PostFormValue("token")
	if subtle.ConstantTimeCompare([]byte(tok), []byte(h.s.token)) != 1 {
		http.Redirect(w, r, uiPathPrefix+"/login?e=1", http.StatusSeeOther)
		return
	}
	sid, err := h.sess.create()
	if err != nil {
		h.fail(w, r, http.StatusInternalServerError, "session error")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     uiCookieName,
		Value:    sid,
		Path:     uiPathPrefix,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(uiSessionTTL.Seconds()),
	})
	http.Redirect(w, r, uiPathPrefix, http.StatusSeeOther)
}

func (h *uiHandler) hLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(uiCookieName); err == nil {
		h.sess.drop(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: uiCookieName, Value: "", Path: uiPathPrefix,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1,
	})
	target := uiPathPrefix
	if h.authNeeded() {
		target = uiPathPrefix + "/login"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// --- data pages ---------------------------------------------------------

func (h *uiHandler) hIndex(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "index.tmpl", map[string]any{
		"Active":      "home",
		"Version":     version,
		"AuthEnabled": h.authNeeded(),
		"Pending":     len(h.snapshotPending()),
	})
}

// uiPendingRow is the projection of pendingSign that the UI list renders.
type uiPendingRow struct {
	ID        string
	CreatedAt time.Time
	AFacts    string
	BInfo     string
	Mismatch  string
}

func (h *uiHandler) snapshotPending() []uiPendingRow {
	h.s.mu.Lock()
	rows := make([]uiPendingRow, 0, len(h.s.pending))
	for id, p := range h.s.pending {
		rows = append(rows, uiPendingRow{
			ID:        id,
			CreatedAt: p.createdAt,
			AFacts:    p.decoded.aFactsJSON,
			BInfo:     p.decoded.bInfoJSON,
			Mismatch:  p.decoded.mismatchJSON,
		})
	}
	h.s.mu.Unlock()
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt.After(rows[j].CreatedAt) })
	return rows
}

func (h *uiHandler) hSignList(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "sign_list.tmpl", map[string]any{
		"Active": "sign",
		"Items":  h.snapshotPending(),
	})
}

func (h *uiHandler) hSignDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	h.s.mu.Lock()
	p, ok := h.s.pending[id]
	var row uiPendingRow
	if ok {
		row = uiPendingRow{
			ID: id, CreatedAt: p.createdAt,
			AFacts: p.decoded.aFactsJSON, BInfo: p.decoded.bInfoJSON,
			Mismatch: p.decoded.mismatchJSON,
		}
	}
	h.s.mu.Unlock()
	if !ok {
		h.fail(w, r, http.StatusNotFound, "unknown or already-resolved sign id")
		return
	}
	h.render(w, r, "sign_detail.tmpl", map[string]any{"Active": "sign", "D": row})
}

// hSignApprove + hSignReject reuse the same pending pop + terminal-wait flow
// as signDecisionH so the WYSIWYS contract is identical: an explicit operator
// click is the only path to a signature. htmx swaps in a result fragment
// inline; a non-htmx POST falls back to a full result page.
func (h *uiHandler) hSignApprove(w http.ResponseWriter, r *http.Request) {
	h.resolve(w, r, true)
}

func (h *uiHandler) hSignReject(w http.ResponseWriter, r *http.Request) {
	h.resolve(w, r, false)
}

func (h *uiHandler) resolve(w http.ResponseWriter, r *http.Request, approve bool) {
	id := r.PathValue("id")
	h.s.mu.Lock()
	p, ok := h.s.pending[id]
	if ok {
		delete(h.s.pending, id)
	}
	h.s.mu.Unlock()
	if !ok {
		h.fail(w, r, http.StatusNotFound, "unknown or already-resolved sign id")
		return
	}
	if approve {
		p.ss.Approve()
	} else {
		p.ss.Reject()
	}
	o := <-p.cb.done
	if p.host != nil {
		_ = p.host.Close()
	}
	if p.ctxCancel != nil {
		p.ctxCancel()
	}
	data := map[string]any{
		"Active":   "sign",
		"ID":       id,
		"Approved": approve,
		"OK":       o.ok,
		"RSV":      o.payload,
		"Code":     o.code,
		"Msg":      o.msg,
	}
	if r.Header.Get("HX-Request") == "true" {
		h.renderPart(w, "sign_result.tmpl", data)
		return
	}
	h.render(w, r, "sign_result.tmpl", data)
}

// --- render helpers -----------------------------------------------------

func (h *uiHandler) render(w http.ResponseWriter, _ *http.Request, page string, data map[string]any) {
	data["Page"] = page
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'self'; script-src 'self'; "+
			"connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	_ = uiTmpl.ExecuteTemplate(w, page, data)
}

func (h *uiHandler) renderPart(w http.ResponseWriter, part string, data map[string]any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = uiTmpl.ExecuteTemplate(w, part, data)
}

func (h *uiHandler) fail(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		writeJSON(w, status, map[string]string{"error": msg})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = uiTmpl.ExecuteTemplate(w, "error.tmpl", map[string]any{
		"Status": status, "Message": msg,
	})
}
