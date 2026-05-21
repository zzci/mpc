package admin

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// admin-ui (UI-001): the htmx + tailwindcss server-side-rendered read-only
// inspection panel designed in admin.md §3. It runs IN the admin-api process
// (node admin role serves the HTML directly) and consumes the SAME D-001 store
// the JSON read surface (query.go) reads — query.go remains the authority for
// the read semantics; the small helpers below mirror its SQL for the fields
// the panel renders, kept here so A-001/H-004 finalized code is not modified
// (surgical; the minor SQL duplication is a deliberate, documented tradeoff).
//
// Security (admin.md §3/§4/§5, default-safe value pending L1/user — see
// Q-UI-AUTH in UI-001.md): every UI route sits behind the same outer
// s.netGate as the rest of the mux (§5 IP allowlist) and the StrongAuth seam
// (§4) when wired; a browser cannot carry Authorization: Bearer on a
// navigation, so a minimal read-only session is used — POST /admin/ui/session
// validates the read/control bearer secret in constant time and sets an
// HttpOnly; SameSite=Strict; Secure(when TLS) opaque cookie. UI handlers
// accept that cookie OR a bearer (proxy/tests) as the scopeRead credential.
// The token is only ever validated server-side and never reaches JS. The
// panel is read-only: no state-changing route exists (Q1 default — control
// operations are NOT exposed in the UI), so the only POST is the login itself
// (which already requires the secret, hence inherently CSRF-resistant).
// Data pages fail closed under LOCKED via s.lockGate; the dashboard is
// reachable under LOCKED to show the lock state only (admin.md §8).

//go:embed uiassets/htmx.min.js uiassets/tw.css uiassets/templates/*.tmpl
var uiFS embed.FS

const (
	uiCookieName = "admin_ui_sid"
	// uiRoot is the htmx panel's root; JSON API lives under "/api/*"
	// (see server.go). One origin, two prefixes — keeps reverse-proxy
	// routing trivial and the cookie scope clean ("/" for both).
	uiRoot       = "/"
	uiLoginPath  = "/login"
	uiSessionTTL = 30 * time.Minute
)

var uiFuncs = template.FuncMap{
	// msTime renders a decimal unix-millisecond string (the form
	// signing_requests/admin_audit timestamps are stored in) as UTC RFC3339;
	// a non-numeric value is passed through unchanged.
	"msTime": func(v string) string {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return v
		}
		return time.UnixMilli(n).UTC().Format("2006-01-02 15:04:05Z")
	},
	// durMS is the elapsed wall time between two unix-ms strings, "" when
	// either bound is missing/!numeric (e.g. not yet dispatched).
	"durMS": func(from, to string) string {
		a, e1 := strconv.ParseInt(from, 10, 64)
		b, e2 := strconv.ParseInt(to, 10, 64)
		if e1 != nil || e2 != nil || a == 0 || b == 0 {
			return ""
		}
		return (time.Duration(b-a) * time.Millisecond).String()
	},
	"short": func(s string) string {
		if len(s) <= 16 {
			return s
		}
		return s[:8] + "…" + s[len(s)-6:]
	},
	"statusClass": func(st string) string {
		switch st {
		case "SIGNED", "RETURNED":
			return "badge-ok"
		case "FAILED", "REJECTED", "EXPIRED":
			return "badge-err"
		case "PENDING", "DISPATCHED", "SIGNING":
			return "badge-warn"
		default:
			return ""
		}
	},
	// toS turns an int64 unix-ms value into the decimal string msTime
	// expects, so a template can chain `{{msTime (toS .ExpiresAtMS)}}`
	// without an intermediate fmt call.
	"toS": func(n int64) string { return strconv.FormatInt(n, 10) },
	// shortHex trims a long hex (e.g. an identity pubkey) for compact
	// table cells while keeping enough characters for visual matching.
	"shortHex": func(s string) string {
		if len(s) <= 18 {
			return s
		}
		return s[:10] + "…" + s[len(s)-6:]
	},
}

var uiTmpl = template.Must(
	template.New("admin-ui").Funcs(uiFuncs).ParseFS(uiFS, "uiassets/templates/*.tmpl"),
)

// uiSessions is an in-memory opaque-session table for the read-only panel.
// Sessions hold no privilege beyond scopeRead (control operations are not in
// the UI); the value is a 256-bit random id, never the token.
type uiSessions struct {
	mu  sync.Mutex
	m   map[string]time.Time // sid → absolute expiry
	now func() time.Time
}

func newUISessions(now func() time.Time) *uiSessions {
	return &uiSessions{m: make(map[string]time.Time), now: now}
}

func (u *uiSessions) create() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("admin-ui: session id: %w", err)
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

// uiHandler is the panel handler set built from the admin Server. It owns its
// own session table so multiple Server instances (tests) never share state.
type uiHandler struct {
	s    *Server
	sess *uiSessions
}

func (s *Server) newUI() *uiHandler {
	return &uiHandler{s: s, sess: newUISessions(s.now)}
}

// register mounts the UI on the shared mux. Routes are registered WITHOUT
// s.guard (which mandates a bearer): the panel uses uiAuth (StrongAuth +
// session-or-bearer). netGate already wraps the whole mux in router().
func (h *uiHandler) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /assets/htmx.min.js", h.asset("uiassets/htmx.min.js", "text/javascript"))
	mux.HandleFunc("GET /assets/tw.css", h.asset("uiassets/tw.css", "text/css"))

	mux.Handle("GET /login", h.strong(http.HandlerFunc(h.hLogin)))
	mux.Handle("POST /session", h.strong(http.HandlerFunc(h.hSession)))
	mux.Handle("POST /logout", h.strong(http.HandlerFunc(h.hLogout)))

	// Dashboard is reachable under LOCKED (admin.md §8: only lock state).
	// Use "/{$}" (Go 1.22 exact match) so the bare root resolves to the
	// dashboard without swallowing /api/* and other prefixes.
	mux.Handle("GET /{$}", h.auth(http.HandlerFunc(h.hIndex)))

	// Data pages fail closed under LOCKED via the shared s.lockGate.
	mux.Handle("GET /transactions", h.auth(h.s.lockGate(http.HandlerFunc(h.hTxList))))
	mux.Handle("GET /transactions/{requestId}", h.auth(h.s.lockGate(http.HandlerFunc(h.hTxDetail))))
	mux.Handle("GET /audit", h.auth(h.s.lockGate(http.HandlerFunc(h.hAuditPage))))
	mux.Handle("GET /relay", h.auth(h.s.lockGate(http.HandlerFunc(h.hRelayPage))))

	// Pairing console — LOCKED-reachable (tokens live in memory, no
	// encrypted-store dependency). Hidden from the nav when not wired.
	if h.s.pairingEnabled() {
		mux.Handle("GET /pairing", h.auth(http.HandlerFunc(h.hPairingPage)))
		mux.Handle("POST /pairing", h.auth(http.HandlerFunc(h.hPairingUICreate)))
		mux.Handle("POST /pairing/{token}/delete", h.auth(http.HandlerFunc(h.hPairingUIDelete)))
	}
}

// strong runs the StrongAuth seam (admin.md §4) when wired, threading the
// verified principal into the context, then delegates. It is the floor for
// unauthenticated-reachable UI routes (login/session/logout).
func (h *uiHandler) strong(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.s.strongAuth != nil {
			principal, err := h.s.strongAuth.Authenticate(r)
			if err != nil {
				h.s.log.Warn("admin-ui strong-auth rejected", "src", clientIP(r))
				h.fail(w, r, http.StatusUnauthorized, "strong authentication failed")
				return
			}
			if principal != "" {
				r = r.WithContext(withPrincipal(r.Context(), principal))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// auth = strong + a valid read session (or a scopeRead bearer for
// proxy/automation). On a missing/expired session a browser is redirected to
// the login page; a bearer caller gets 401.
func (h *uiHandler) auth(next http.Handler) http.Handler {
	return h.strong(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(uiCookieName); err == nil && h.sess.valid(c.Value) {
			next.ServeHTTP(w, r)
			return
		}
		if h.bearerReadOK(r) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			h.fail(w, r, http.StatusUnauthorized, "invalid token")
			return
		}
		http.Redirect(w, r, uiLoginPath, http.StatusSeeOther)
	}))
}

// bearerReadOK accepts the read OR control secret for the read scope (control
// implies read), constant-time (mirrors auth.go.authorize).
func (h *uiHandler) bearerReadOK(r *http.Request) bool {
	tok := bearer(r)
	if tok == "" {
		return false
	}
	ctrl := subtle.ConstantTimeCompare([]byte(tok), []byte(h.s.cfg.ControlToken)) == 1
	read := subtle.ConstantTimeCompare([]byte(tok), []byte(h.s.cfg.ReadToken)) == 1
	return ctrl || read
}

func (h *uiHandler) asset(name, ctype string) http.HandlerFunc {
	body, _ := uiFS.ReadFile(name)
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	}
}

// --- auth pages ----------------------------------------------------------

func (h *uiHandler) hLogin(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "login.tmpl", map[string]any{"Error": r.URL.Query().Get("e") != ""})
}

func (h *uiHandler) hSession(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, http.StatusBadRequest, "malformed form")
		return
	}
	tok := r.PostFormValue("token")
	ctrl := subtle.ConstantTimeCompare([]byte(tok), []byte(h.s.cfg.ControlToken)) == 1
	read := subtle.ConstantTimeCompare([]byte(tok), []byte(h.s.cfg.ReadToken)) == 1
	if !ctrl && !read {
		h.s.log.Warn("admin-ui login rejected", "src", clientIP(r))
		http.Redirect(w, r, uiLoginPath+"?e=1", http.StatusSeeOther)
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
		Path:     uiRoot,
		HttpOnly: true,
		Secure:   h.s.tlsCfg != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(uiSessionTTL.Seconds()),
	})
	http.Redirect(w, r, uiRoot, http.StatusSeeOther)
}

func (h *uiHandler) hLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(uiCookieName); err == nil {
		h.sess.drop(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: uiCookieName, Value: "", Path: uiRoot,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1,
	})
	http.Redirect(w, r, uiLoginPath, http.StatusSeeOther)
}

// --- read pages ----------------------------------------------------------

func (h *uiHandler) hIndex(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "index.tmpl", map[string]any{
		"Unlocked": h.s.store.IsUnlocked(),
		"Active":   "home",
	})
}

func (h *uiHandler) hTxList(w http.ResponseWriter, r *http.Request) {
	f, err := parseTxFilter(r)
	if err != nil {
		h.fail(w, r, http.StatusBadRequest, err.Error())
		return
	}
	items, next, qErr := h.readTransactions(r, f)
	if qErr != nil {
		h.fail(w, r, http.StatusInternalServerError, "query failed")
		return
	}
	data := map[string]any{
		"Active": "tx", "Items": items, "NextBefore": next,
		"F": map[string]string{
			"group": f.groupID, "status": f.status, "proposer": f.proposer,
		},
		"Statuses": orderedStatuses,
	}
	if r.Header.Get("HX-Request") == "true" {
		h.renderPart(w, "tx_rows.tmpl", data)
		return
	}
	h.render(w, r, "transactions.tmpl", data)
}

func (h *uiHandler) hTxDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("requestId")
	d, qErr := h.readTxDetail(r, id)
	if errors.Is(qErr, errUINotFound) {
		h.fail(w, r, http.StatusNotFound, "unknown requestId")
		return
	}
	if qErr != nil {
		h.fail(w, r, http.StatusInternalServerError, "query failed")
		return
	}
	h.render(w, r, "tx_detail.tmpl", map[string]any{"Active": "tx", "D": d})
}

func (h *uiHandler) hAuditPage(w http.ResponseWriter, r *http.Request) {
	limit := defaultPageLimit
	beforeID := int64(-1)
	if v := r.URL.Query().Get("before"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			beforeID = n
		}
	}
	items, qErr := h.readAudit(r, beforeID, limit)
	if qErr != nil {
		h.fail(w, r, http.StatusInternalServerError, "query failed")
		return
	}
	var next int64 = -1
	if len(items) == limit {
		next = items[len(items)-1].ID
	}
	data := map[string]any{"Active": "audit", "Items": items, "Next": next}
	if r.Header.Get("HX-Request") == "true" {
		h.renderPart(w, "audit_rows.tmpl", data)
		return
	}
	h.render(w, r, "audit.tmpl", data)
}

func (h *uiHandler) hRelayPage(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{"Active": "relay", "Available": false}
	if h.s.relayMetrics != nil {
		snap, err := h.s.relayMetrics.Snapshot(r.Context())
		if err != nil {
			h.fail(w, r, http.StatusInternalServerError, "relay metrics unavailable")
			return
		}
		data["Available"] = true
		data["Metrics"] = snap
	}
	h.render(w, r, "relay.tmpl", data)
}

// --- render helpers ------------------------------------------------------

func (h *uiHandler) render(w http.ResponseWriter, _ *http.Request, page string, data map[string]any) {
	data["Page"] = page
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'self'; script-src 'self'; "+
			"connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	if err := uiTmpl.ExecuteTemplate(w, page, data); err != nil {
		h.s.log.Error("admin-ui render", "page", page, "err", err.Error())
	}
}

func (h *uiHandler) renderPart(w http.ResponseWriter, part string, data map[string]any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if err := uiTmpl.ExecuteTemplate(w, part, data); err != nil {
		h.s.log.Error("admin-ui render part", "part", part, "err", err.Error())
	}
}

// fail renders the error as HTML for a browser, JSON-ish text for bearer
// callers. It never leaks internals (admin.md api.md:79 discipline).
func (h *uiHandler) fail(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		h.s.writeErr(w, &apiError{status: status, code: "ui_error", message: msg})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = uiTmpl.ExecuteTemplate(w, "error.tmpl", map[string]any{
		"Status": status, "Message": msg,
	})
}

// --- read models (mirror query.go SQL; query.go is the authority) --------

var orderedStatuses = []string{
	"PENDING", "DISPATCHED", "SIGNING", "SIGNED",
	"RETURNED", "EXPIRED", "REJECTED", "FAILED",
}

type uiTx struct {
	RequestID, GroupID, Chain, Proposer, Status string
	CreatedAt, Expiry, DispatchedAt, FailReason string
}

type uiEvent struct {
	FromStatus, ToStatus, Actor, Detail, At string
}

type uiApproval struct {
	MemberID, Decision, DecidedAt string
}

type uiTxDetail struct {
	uiTx
	UnsignedTx, Digest32, MetaHash, ProposerSig string
	BusinessInfo, Signers, ResultRSV            string
	Timeline                                    []uiEvent
	Approvals                                   []uiApproval
}

type uiAuditRow struct {
	ID                         int64
	AdminID, Action, SrcIP, At string
	Params                     string
}

var errUINotFound = errors.New("admin-ui: not found")

// readTransactions mirrors query.go hTransactions (newest-first, same filters
// and created_at cursor) for the panel's list view.
func (h *uiHandler) readTransactions(r *http.Request, f txFilter) ([]uiTx, string, error) {
	where := "1=1"
	args := []any{}
	add := func(c string, v any) { where += " AND " + c; args = append(args, v) }
	if f.groupID != "" {
		add("group_id = ?", f.groupID)
	}
	if f.status != "" {
		add("status = ?", f.status)
	}
	if f.proposer != "" {
		add("proposer = ?", f.proposer)
	}
	if f.fromMS != "" {
		add("created_at >= ?", f.fromMS)
	}
	if f.toMS != "" {
		add("created_at <= ?", f.toMS)
	}
	if f.beforeMS != "" {
		add("created_at < ?", f.beforeMS)
	}
	args = append(args, f.limit)
	items := []uiTx{}
	err := h.s.store.WithTx(r.Context(), func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(r.Context(),
			`SELECT request_id, group_id, chain, proposer, status,
			        created_at, expiry, dispatched_at, fail_reason
			   FROM signing_requests WHERE `+where+`
			  ORDER BY created_at DESC, request_id DESC LIMIT ?`, args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var t uiTx
			var disp, fr sql.NullString
			if err := rows.Scan(&t.RequestID, &t.GroupID, &t.Chain, &t.Proposer,
				&t.Status, &t.CreatedAt, &t.Expiry, &disp, &fr); err != nil {
				return err
			}
			t.DispatchedAt, t.FailReason = disp.String, fr.String
			items = append(items, t)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if n := len(items); n > 0 && n == f.limit {
		next = items[n-1].CreatedAt
	}
	return items, next, nil
}

// readTxDetail mirrors query.go hTransactionDetail.
func (h *uiHandler) readTxDetail(r *http.Request, id string) (uiTxDetail, error) {
	var d uiTxDetail
	d.RequestID = id
	d.Timeline = []uiEvent{}
	d.Approvals = []uiApproval{}
	err := h.s.store.WithTx(r.Context(), func(tx *sql.Tx) error {
		var unsigned, digest, metaHash, proposerSig, rsv []byte
		var business, disp, signers, fr sql.NullString
		row := tx.QueryRowContext(r.Context(),
			`SELECT group_id, chain, unsigned_tx, digest32, proposer, business_info,
			        meta_hash, proposer_sig, status, created_at, expiry,
			        dispatched_at, signers, result_rsv, fail_reason
			   FROM signing_requests WHERE request_id = ?`, id)
		if err := row.Scan(&d.GroupID, &d.Chain, &unsigned, &digest, &d.Proposer,
			&business, &metaHash, &proposerSig, &d.Status, &d.CreatedAt, &d.Expiry,
			&disp, &signers, &rsv, &fr); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errUINotFound
			}
			return err
		}
		d.DispatchedAt, d.FailReason = disp.String, fr.String
		d.UnsignedTx, d.Digest32 = b64(unsigned), b64(digest)
		d.MetaHash, d.ProposerSig = b64(metaHash), b64(proposerSig)
		d.BusinessInfo, d.Signers = business.String, signers.String
		if len(rsv) > 0 {
			d.ResultRSV = b64(rsv)
		}
		evRows, err := tx.QueryContext(r.Context(),
			`SELECT from_status, to_status, actor, detail, at
			   FROM request_events WHERE request_id = ? ORDER BY at, id`, id)
		if err != nil {
			return err
		}
		defer func() { _ = evRows.Close() }()
		for evRows.Next() {
			var e uiEvent
			var from, to, det sql.NullString
			if err := evRows.Scan(&from, &to, &e.Actor, &det, &e.At); err != nil {
				return err
			}
			e.FromStatus, e.ToStatus, e.Detail = from.String, to.String, det.String
			d.Timeline = append(d.Timeline, e)
		}
		if err := evRows.Err(); err != nil {
			return err
		}
		apRows, err := tx.QueryContext(r.Context(),
			`SELECT member_id, decision, decided_at
			   FROM request_approvals WHERE request_id = ? ORDER BY decided_at`, id)
		if err != nil {
			return err
		}
		defer func() { _ = apRows.Close() }()
		for apRows.Next() {
			var a uiApproval
			if err := apRows.Scan(&a.MemberID, &a.Decision, &a.DecidedAt); err != nil {
				return err
			}
			d.Approvals = append(d.Approvals, a)
		}
		return apRows.Err()
	})
	return d, err
}

// readAudit mirrors query.go hAudit (newest-first, id cursor).
func (h *uiHandler) readAudit(r *http.Request, beforeID int64, limit int) ([]uiAuditRow, error) {
	items := []uiAuditRow{}
	err := h.s.store.WithTx(r.Context(), func(tx *sql.Tx) error {
		where := ""
		args := []any{}
		if beforeID >= 0 {
			where = "WHERE id < ?"
			args = append(args, beforeID)
		}
		args = append(args, limit)
		rows, err := tx.QueryContext(r.Context(),
			`SELECT id, admin_id, action, params, src_ip, at
			   FROM admin_audit `+where+` ORDER BY id DESC LIMIT ?`, args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var a uiAuditRow
			var params, srcIP sql.NullString
			if err := rows.Scan(&a.ID, &a.AdminID, &a.Action, &params, &srcIP, &a.At); err != nil {
				return err
			}
			a.Params, a.SrcIP = params.String, srcIP.String
			items = append(items, a)
		}
		return rows.Err()
	})
	return items, err
}
