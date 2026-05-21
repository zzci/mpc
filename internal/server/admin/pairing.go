package admin

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/skip2/go-qrcode"

	"github.com/zzci/mpc/internal/server"
)

// PairingPublicCoordURL is the externally-visible coord base URL that the
// admin server embeds in pairing-config URLs when an operator generates a
// QR. Without it the QR contains "/v1/pairing/<token>/config" as a relative
// path — fine for testing on the same host, useless for a real device. The
// operator wires this at construction (WithPairingPublicCoordURL).
type pairingHooks struct {
	store        *server.PairingStore
	coordBaseURL string
}

// WithPairing wires the shared PairingStore plus the externally-visible
// coord base URL (used to build the QR contents). store==nil disables the
// pairing API+UI entirely (handlers 404). When coordBaseURL is empty the
// admin UI surfaces a clear warning so the operator knows scanning the
// QR off-host will not work.
func WithPairing(store *server.PairingStore, coordBaseURL string) Option {
	return func(s *Server) { s.pairing = pairingHooks{store: store, coordBaseURL: coordBaseURL} }
}

// pairingEnabled reports whether the admin server should expose the
// pairing routes / UI page. Used by the registration helper to skip the
// routes cleanly when the feature is not wired.
func (s *Server) pairingEnabled() bool { return s.pairing.store != nil }

// --- JSON CRUD ----------------------------------------------------------

type pairingCreateRequest struct {
	GroupID    string `json:"groupId,omitempty"`
	Label      string `json:"label,omitempty"`
	TTLSeconds int    `json:"ttlSeconds,omitempty"`
}

type pairingTicketView struct {
	Token       string `json:"token"`
	GroupID     string `json:"groupId,omitempty"`
	Label       string `json:"label,omitempty"`
	CreatedAtMS int64  `json:"createdAtMs"`
	ExpiresAtMS int64  `json:"expiresAtMs"`
	UsedAtMS    int64  `json:"usedAtMs,omitempty"` // 0 = still pending
	UsedBy      string `json:"usedBy,omitempty"`
	ConfigURL   string `json:"configUrl"`
}

const (
	defaultPairingTTL = 10 * time.Minute
	maxPairingTTL     = 24 * time.Hour
)

func (s *Server) hPairingList(w http.ResponseWriter, _ *http.Request) {
	out := make([]pairingTicketView, 0)
	for _, t := range s.pairing.store.List() {
		out = append(out, s.toTicketView(t))
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) hPairingCreate(w http.ResponseWriter, r *http.Request) {
	var body pairingCreateRequest
	if r.ContentLength > 0 {
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			s.writeErr(w, &apiError{status: http.StatusBadRequest, code: "bad_body", message: err.Error()})
			return
		}
	}
	ttl := time.Duration(body.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = defaultPairingTTL
	}
	if ttl > maxPairingTTL {
		s.writeErr(w, &apiError{status: http.StatusBadRequest, code: "bad_ttl", message: "ttlSeconds exceeds 24h cap"})
		return
	}
	t, err := s.pairing.store.Create(body.GroupID, body.Label, ttl)
	if err != nil {
		s.writeErr(w, &apiError{status: http.StatusInternalServerError, code: "create_failed", message: err.Error()})
		return
	}
	s.auditPairing(r, scopeControl, "pairing.create", t.Token, body.GroupID)
	s.writeJSON(w, http.StatusCreated, s.toTicketView(t))
}

func (s *Server) hPairingDelete(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if !s.pairing.store.Delete(token) {
		s.writeErr(w, &apiError{status: http.StatusNotFound, code: "unknown_token", message: "pairing token not found"})
		return
	}
	s.auditPairing(r, scopeControl, "pairing.delete", token, "")
	w.WriteHeader(http.StatusNoContent)
}

// hPairingQR renders the ticket's config URL as a PNG QR code. The image
// is 256×256 and Q-level error correction (medium-high). The URL inside
// the QR points at the public coord config endpoint so a device scanning
// the QR can GET its bootstrap data directly — operator never copies the
// raw token elsewhere.
func (s *Server) hPairingQR(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	t, ok := s.pairing.store.Get(token)
	if !ok {
		s.writeErr(w, &apiError{status: http.StatusNotFound, code: "unknown_token", message: "pairing token not found"})
		return
	}
	url := s.pairingConfigURL(t.Token)
	png, err := qrcode.Encode(url, qrcode.Medium, 256)
	if err != nil {
		s.writeErr(w, &apiError{status: http.StatusInternalServerError, code: "qr_failed", message: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

// pairingConfigURL is the full URL a scanning device GETs to fetch its
// bootstrap config. It is the *only* thing encoded in the QR — the QR
// contents never include the raw token outside this URL path.
func (s *Server) pairingConfigURL(token string) string {
	base := s.pairing.coordBaseURL
	if base == "" {
		// Best-effort relative URL: only safe on the same host.
		return "/v1/pairing/" + token + "/config"
	}
	return base + "/v1/pairing/" + token + "/config"
}

func (s *Server) toTicketView(t server.PairingTicket) pairingTicketView {
	v := pairingTicketView{
		Token:       t.Token,
		GroupID:     t.GroupID,
		Label:       t.Label,
		CreatedAtMS: t.CreatedAt.UnixMilli(),
		ExpiresAtMS: t.ExpiresAt.UnixMilli(),
		ConfigURL:   s.pairingConfigURL(t.Token),
		UsedBy:      t.UsedBy,
	}
	if t.UsedAt != nil {
		v.UsedAtMS = t.UsedAt.UnixMilli()
	}
	return v
}

func (s *Server) auditPairing(r *http.Request, sc scope, action, token, groupID string) {
	if s.store == nil {
		return
	}
	shortToken := token
	if len(shortToken) > 16 {
		shortToken = shortToken[:16] + "…"
	}
	if err := s.audit(r.Context(), r, sc, action,
		map[string]any{"token": shortToken, "groupId": groupID},
	); err != nil {
		s.log.Warn("pairing audit failed", "action", action, "err", err.Error())
	}
}

// --- htmx UI ------------------------------------------------------------

// hPairingPage renders /pairing — the operator's enrollment console. It is
// reachable under LOCKED (same as the dashboard) because pairing-token
// management does not require encrypted-store access.
func (h *uiHandler) hPairingPage(w http.ResponseWriter, r *http.Request) {
	if !h.s.pairingEnabled() {
		h.fail(w, r, http.StatusNotFound, "pairing not configured")
		return
	}
	items := make([]pairingTicketView, 0)
	for _, t := range h.s.pairing.store.List() {
		items = append(items, h.s.toTicketView(t))
	}
	h.render(w, r, "pairing.tmpl", map[string]any{
		"Active":             "pairing",
		"Items":              items,
		"PublicCoordURL":     h.s.pairing.coordBaseURL,
		"PublicCoordMissing": h.s.pairing.coordBaseURL == "",
	})
}

// hPairingUICreate is the form-post target from /pairing. It re-renders
// the page on both success and failure (htmx outer swap).
func (h *uiHandler) hPairingUICreate(w http.ResponseWriter, r *http.Request) {
	if !h.s.pairingEnabled() {
		h.fail(w, r, http.StatusNotFound, "pairing not configured")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, http.StatusBadRequest, "malformed form")
		return
	}
	groupID := r.PostFormValue("groupId")
	label := r.PostFormValue("label")
	ttlSec := atoiOrDefault(r.PostFormValue("ttlSeconds"), 600)
	ttl := time.Duration(ttlSec) * time.Second
	if ttl <= 0 || ttl > maxPairingTTL {
		ttl = defaultPairingTTL
	}
	if _, err := h.s.pairing.store.Create(groupID, label, ttl); err != nil {
		h.fail(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	h.s.auditPairing(r, scopeControl, "pairing.create", "", groupID)
	http.Redirect(w, r, "/pairing", http.StatusSeeOther)
}

func (h *uiHandler) hPairingUIDelete(w http.ResponseWriter, r *http.Request) {
	if !h.s.pairingEnabled() {
		h.fail(w, r, http.StatusNotFound, "pairing not configured")
		return
	}
	token := r.PathValue("token")
	if !h.s.pairing.store.Delete(token) {
		h.fail(w, r, http.StatusNotFound, "unknown token")
		return
	}
	h.s.auditPairing(r, scopeControl, "pairing.delete", token, "")
	http.Redirect(w, r, "/pairing", http.StatusSeeOther)
}

func atoiOrDefault(s string, def int) int {
	n := 0
	if s == "" {
		return def
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
		if n > 86_400 {
			return def
		}
	}
	if n == 0 {
		return def
	}
	return n
}

// b64 is exported only to satisfy the import; we keep base64 in scope for
// future QR variants (e.g. data: URLs). Unused for now.
var _ = base64.StdEncoding
