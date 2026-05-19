package admin

import (
	"context"
	"net/http"
)

// RelayController is the relay-side enforcement seam for the operations
// kill-switches (admin.md §1 controllable, server.md R4 layer 3). The relay role is
// stateless and coord-independent (server.md R5) — a deployment may run it as
// a separate instance — so admin-api does not import the relay package; it
// records every directive in admin_audit and, when a controller is wired,
// enforces it. A nil controller means enforcement is delegated to the relay
// role (the directive is still audited).
//
// These are operations kill-switches only: nothing here grants access. The
// admin cannot admit a peer, issue a group token, forge an approval or move
// funds — there is deliberately no such method (admin.md §21/§70 hard
// boundary).
type RelayController interface {
	// BanPeer blocks a libp2p peerID at the relay (operations censor).
	BanPeer(ctx context.Context, peerID string) error
	// RevokeReservation drops a peer's active circuit-relay reservation.
	RevokeReservation(ctx context.Context, peerID string) error
	// RotatePSK rotates the private-network swarm key (pnet PSK). The new key
	// material never transits admin-api in cleartext and is never audited.
	RotatePSK(ctx context.Context) error
	// SetQuota updates reservation/bandwidth/rate-limit parameters.
	SetQuota(ctx context.Context, params map[string]int) error
}

// RelayMetrics is the read-only relay observability seam (server.md R6).
type RelayMetrics interface {
	Snapshot(ctx context.Context) (map[string]any, error)
}

// applyControl is the shared control path: audit the directive first
// (append-only, non-secret params), then enforce via the controller when
// wired. Auditing before enforcement guarantees the operator action is
// recorded even if enforcement is delegated or fails (admin.md §7bis
// "all control operations ... go into admin_audit").
func (s *Server) applyControl(w http.ResponseWriter, r *http.Request,
	action string, params map[string]any, enforce func(RelayController) error) {

	if err := s.audit(r.Context(), r, scopeControl, action, params); err != nil {
		s.writeErr(w, asAPIError(err))
		return
	}
	resp := map[string]any{"recorded": true, "action": action}
	if s.relayController == nil {
		resp["enforced"] = false
		resp["note"] = "relay role decoupled (server.md R5); directive audited, enforcement delegated to relay instance"
		s.writeJSON(w, http.StatusAccepted, resp)
		return
	}
	if err := enforce(s.relayController); err != nil {
		s.writeErr(w, asAPIError(err))
		return
	}
	resp["enforced"] = true
	s.writeJSON(w, http.StatusOK, resp)
}

type peerBody struct {
	PeerID string `json:"peerId"`
}

func (s *Server) hBanPeer(w http.ResponseWriter, r *http.Request) {
	var b peerBody
	if !s.readJSON(w, r, &b) {
		return
	}
	if b.PeerID == "" {
		s.writeErr(w, errBadRequest("missing peerId"))
		return
	}
	s.applyControl(w, r, "relay.ban_peer", map[string]any{"peerId": b.PeerID},
		func(rc RelayController) error { return rc.BanPeer(r.Context(), b.PeerID) })
}

func (s *Server) hRevokeReservation(w http.ResponseWriter, r *http.Request) {
	var b peerBody
	if !s.readJSON(w, r, &b) {
		return
	}
	if b.PeerID == "" {
		s.writeErr(w, errBadRequest("missing peerId"))
		return
	}
	s.applyControl(w, r, "relay.revoke_reservation", map[string]any{"peerId": b.PeerID},
		func(rc RelayController) error { return rc.RevokeReservation(r.Context(), b.PeerID) })
}

// hRotatePSK rotates the pnet PSK. The new key value is generated relay-side
// and is a secret: it is never accepted from, returned by, or audited through
// admin-api (database.md §6 params contain no plaintext secret). Only the rotation event
// is recorded.
func (s *Server) hRotatePSK(w http.ResponseWriter, r *http.Request) {
	s.applyControl(w, r, "relay.rotate_psk", nil,
		func(rc RelayController) error { return rc.RotatePSK(r.Context()) })
}

type quotaBody struct {
	Params map[string]int `json:"params"`
}

// hSetQuota updates relay quota / rate-limit parameters (admin.md §1
// quota/rate-limit params). Values are non-secret operational integers and are audited.
func (s *Server) hSetQuota(w http.ResponseWriter, r *http.Request) {
	var b quotaBody
	if !s.readJSON(w, r, &b) {
		return
	}
	if len(b.Params) == 0 {
		s.writeErr(w, errBadRequest("missing params"))
		return
	}
	audited := make(map[string]any, len(b.Params))
	for k, v := range b.Params {
		audited[k] = v
	}
	s.applyControl(w, r, "relay.set_quota", audited,
		func(rc RelayController) error { return rc.SetQuota(r.Context(), b.Params) })
}
