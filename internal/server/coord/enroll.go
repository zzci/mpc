package coord

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zzci/mpc/internal/server"
)

// PairingPublicInfo is what coord embeds in a pairing GET-config response
// (and what the device persists after a successful enroll). It is the
// runtime-configurable half of the QR contents — token + groupId come from
// the ticket itself, but coord's base URL and the relay bootstrap details
// are deployment-specific and must be supplied by cmd/server (or, in
// tests, by WithPairingInfo).
type PairingPublicInfo struct {
	// CoordBaseURL is the externally-visible base URL the device should
	// use for its B-side API calls (heartbeat, dispatch, attestation,
	// xpub, …). When empty, coord falls back to "http://" + r.Host on
	// each request — works on a loopback dev box, never enough for
	// production with a reverse proxy / TLS termination.
	CoordBaseURL string `json:"coordBaseUrl,omitempty"`
	// RelayPeerID is the libp2p peer id of the project's relay role.
	RelayPeerID string `json:"relayPeerID,omitempty"`
	// RelayAddrs is the relay's external multiaddrs (one or more).
	RelayAddrs []string `json:"relayAddrs,omitempty"`
}

// WithPairingInfo wires the runtime-configurable half of the pairing
// public-config response. Without it pairing still works (coordBaseURL
// falls back to r.Host) but the QR will lack the relay bootstrap, so
// operators are expected to call this in production wiring.
func WithPairingInfo(info PairingPublicInfo) Option {
	return func(c *Coord) { c.pairingInfo = info }
}

// WithPairingStore injects the shared PairingStore (admin-api and coord
// share it). A nil store turns the pairing endpoints into 503: the
// feature is opt-in at wiring time.
func WithPairingStore(s *server.PairingStore) Option {
	return func(c *Coord) { c.pairing = s }
}

// pairingPublicResponse is the JSON shape returned by both the config-fetch
// (GET) and the enroll (POST) endpoints. Devices persist these exact fields.
type pairingPublicResponse struct {
	Token        string   `json:"token"`
	GroupID      string   `json:"groupId,omitempty"`
	Label        string   `json:"label,omitempty"`
	ExpiresAtMS  int64    `json:"expiresAtMs"`
	CoordBaseURL string   `json:"coordBaseUrl"`
	RelayPeerID  string   `json:"relayPeerID,omitempty"`
	RelayAddrs   []string `json:"relayAddrs,omitempty"`
}

// enrollRequest is what a freshly-scanned device POSTs to claim its ticket.
// identityPubkey is the device's secp256k1 pubkey (compressed 33B or
// uncompressed 65B), hex-encoded. label is an optional human-readable
// note (e.g. "iPhone 15") echoed back in subsequent admin UI views.
type enrollRequest struct {
	IdentityPubkey string `json:"identityPubkey"`
	Label          string `json:"label,omitempty"`
}

// registerPairingRoutes hooks the two public endpoints onto the same mux
// the rest of coord uses. The pairing handlers run UNDER lockGate (no
// access to encrypted data is required, but we still surface 503 LOCKED
// to keep the operator's mental model uniform: "everything 503s while
// the store is locked") — see api.md §C.
func (c *Coord) registerPairingRoutes(mux *http.ServeMux) {
	if c.pairing == nil {
		return
	}
	mux.HandleFunc("GET /v1/pairing/{token}/config", c.hPairingConfig)
	mux.HandleFunc("POST /v1/pairing/{token}/enroll", c.hPairingEnroll)
}

func (c *Coord) hPairingConfig(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	t, ok := c.pairing.Get(token)
	if !ok {
		c.writeErr(w, errNotFound("unknown pairing token"))
		return
	}
	c.writePairingResponse(w, r, t)
}

func (c *Coord) hPairingEnroll(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")

	var body enrollRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		c.writeErr(w, errInvalidEnvelope("malformed body: "+err.Error()))
		return
	}
	pubBytes, err := hex.DecodeString(body.IdentityPubkey)
	if err != nil || (len(pubBytes) != 33 && len(pubBytes) != 65) {
		c.writeErr(w, errInvalidEnvelope("identityPubkey must be 33 or 65 byte hex secp256k1"))
		return
	}

	t, err := c.pairing.Consume(token, body.IdentityPubkey)
	switch {
	case errors.Is(err, server.ErrPairingUnknown):
		c.writeErr(w, errNotFound("unknown pairing token"))
		return
	case errors.Is(err, server.ErrPairingExpired):
		c.writeErr(w, errStateConflict("pairing token expired"))
		return
	case errors.Is(err, server.ErrPairingUsed):
		c.writeErr(w, errStateConflict("pairing token already consumed"))
		return
	case err != nil:
		c.writeErr(w, asAPIError(err))
		return
	}

	if body.Label != "" && t.Label == "" {
		// purely cosmetic; the device's submitted label persists in the
		// audit row alongside its identity hex.
		t.Label = body.Label
	}

	c.log.Info("pairing enrolled",
		"token", token[:8]+"…",
		"groupId", t.GroupID,
		"identity", body.IdentityPubkey[:min(len(body.IdentityPubkey), 16)]+"…",
		"src", clientIP(r))

	c.writePairingResponse(w, r, t)
}

func (c *Coord) writePairingResponse(w http.ResponseWriter, r *http.Request, t server.PairingTicket) {
	resp := pairingPublicResponse{
		Token:        t.Token,
		GroupID:      t.GroupID,
		Label:        t.Label,
		ExpiresAtMS:  t.ExpiresAt.UnixMilli(),
		CoordBaseURL: c.coordBaseURL(r),
		RelayPeerID:  c.pairingInfo.RelayPeerID,
		RelayAddrs:   c.pairingInfo.RelayAddrs,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}

// coordBaseURL returns the configured public URL when WithPairingInfo
// supplied one, otherwise a best-effort scheme://host derived from the
// incoming request. We do not include r.URL.Path because pairing
// responses are deployment-bootstrap data, not request-specific.
func (c *Coord) coordBaseURL(r *http.Request) string {
	if c.pairingInfo.CoordBaseURL != "" {
		return strings.TrimRight(c.pairingInfo.CoordBaseURL, "/")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
