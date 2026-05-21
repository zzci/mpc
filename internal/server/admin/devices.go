package admin

import (
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
)

// Device cross-group view (admin-ui multi-group, user ruling 2026-05-18).
// An operator types or pastes an identity pubkey hex on the /devices page
// and admin queries coord's group_members for every (groupId, memberId)
// row that identity is bound to, so the operator can audit "where does
// this device sign?" — the same identity may be a member of N groups.

// deviceGroupRow is one (groupId, memberId, status) row returned by the
// devices view. created_at lets the UI render relative time.
type deviceGroupRow struct {
	GroupID   string `json:"groupId"`
	MemberID  string `json:"memberId"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

// hDevicesByIdentity serves GET /api/devices/{identityHex}/groups. The
// identityHex is required to be 33-byte (compressed) or 65-byte
// (uncompressed) secp256k1 hex; the handler decodes it and queries
// group_members.identity_pubkey for a byte-equal match across all groups.
func (s *Server) hDevicesByIdentity(w http.ResponseWriter, r *http.Request) {
	idHex := r.PathValue("identityHex")
	idBytes, err := hex.DecodeString(idHex)
	if err != nil || (len(idBytes) != 33 && len(idBytes) != 65) {
		s.writeErr(w, &apiError{
			status:  http.StatusBadRequest,
			code:    "bad_identity",
			message: "identityHex must be 33-byte compressed or 65-byte uncompressed secp256k1 hex",
		})
		return
	}
	rows, err := s.queryDeviceGroups(r, idBytes)
	if err != nil {
		s.writeErr(w, &apiError{status: http.StatusInternalServerError, code: "query_failed", message: err.Error()})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"identityPubkeyHex": idHex,
		"items":             rows,
		"count":             len(rows),
	})
}

func (s *Server) queryDeviceGroups(r *http.Request, identity []byte) ([]deviceGroupRow, error) {
	rows := []deviceGroupRow{}
	err := s.store.WithTx(r.Context(), func(tx *sql.Tx) error {
		// group_members has no index on identity_pubkey — a small admin
		// tenant table doesn't need one, the operator hits this rarely
		// and the table size is bounded by the deployment's member count.
		q, qerr := tx.QueryContext(r.Context(),
			`SELECT group_id, member_id, status,
			        COALESCE(
			           (SELECT g.created_at FROM groups g WHERE g.group_id = group_members.group_id),
			           '')
			   FROM group_members
			  WHERE identity_pubkey = ?
			  ORDER BY group_id, member_id`, identity)
		if qerr != nil {
			return qerr
		}
		defer func() { _ = q.Close() }()
		for q.Next() {
			var row deviceGroupRow
			if err := q.Scan(&row.GroupID, &row.MemberID, &row.Status, &row.CreatedAt); err != nil {
				return err
			}
			rows = append(rows, row)
		}
		return q.Err()
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// --- UI ----------------------------------------------------------------

// hDevicesPage renders /devices: a single text input for the identity pub
// (hex) + the rendered result when the operator presses Look up. Query is
// passed via ?id=<hex>. Operator may also click an identity row in the
// /pairing audit (future) to land here pre-filled.
func (h *uiHandler) hDevicesPage(w http.ResponseWriter, r *http.Request) {
	idHex := strings.TrimSpace(r.URL.Query().Get("id"))
	data := map[string]any{
		"Active":      "devices",
		"IdentityHex": idHex,
	}
	if idHex != "" {
		idBytes, err := hex.DecodeString(idHex)
		if err != nil || (len(idBytes) != 33 && len(idBytes) != 65) {
			data["Error"] = "identityHex must be 33-byte (compressed) or 65-byte (uncompressed) secp256k1 hex"
		} else {
			rows, qerr := h.s.queryDeviceGroups(r, idBytes)
			if qerr != nil {
				data["Error"] = qerr.Error()
			} else {
				data["Items"] = rows
			}
		}
	}
	h.render(w, r, "devices.tmpl", data)
}

// b64Identity helps the UI render the raw stored pubkey alongside the hex
// form when querying small audit views. (Currently unused — kept for
// future inline preview.)
var _ = base64.StdEncoding
