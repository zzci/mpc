package coord

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"

	"github.com/zzci/mpc/internal/server/coorddb"
)

// AD-6 B12 — group_derived_addresses (docs/design/contract/api.md B12,
// docs/design/mcp/address-derivation.md §7.bis, Q-A/B/C/D user ruling
// 2026-05-20). Two routes, both memberGate-only (B-side
// owning-member-only, §7.bis.3 / §F1):
//
//   POST /v1/groups/{groupId}/derived/register  — lazy registration
//   GET  /v1/groups/{groupId}/derived           — owning-member list
//
// Auth: the memberAuth envelope (X-Member-* headers) carries ts+nonce+sig
// per api.md B1 / D, so the JSON body for register only needs the address
// fields and an optional child_pubkey. There is no A-side route (§F1
// strict): the table is never exposed externally; coord does not
// re-derive — only records what the client reports.

// derivedRegisterBody is the POST body for B12 (api.md: index, evmAddress,
// tronAddress, childPubkeyHex?). The memberId+ts+nonce+sig live in the
// X-Member-* headers (parsed by memberGate), so they are not body fields.
// Index is uint32 with the non-hardened [0, 2^31) constraint enforced by
// coorddb.RegisterDerivedAddress (and by the schema CHECK in 00005).
type derivedRegisterBody struct {
	Index          uint32 `json:"index"`
	EVMAddress     string `json:"evmAddress"`
	TronAddress    string `json:"tronAddress"`
	ChildPubkeyHex string `json:"childPubkeyHex,omitempty"`
}

// derivedItem is one element of the GET response (api.md B12). Hex is used
// for child_pubkey to mirror the request shape (childPubkeyHex); the
// derived addresses are already string-typed.
type derivedItem struct {
	Index          uint32 `json:"index"`
	EVMAddress     string `json:"evmAddress"`
	TronAddress    string `json:"tronAddress"`
	ChildPubkeyHex string `json:"childPubkeyHex,omitempty"`
	CreatedAt      int64  `json:"createdAt"`
}

// derivedListResponse is the GET shape (api.md B12). serverTime is the
// coord clock in unix ms (matches B3 / B7 conventions).
type derivedListResponse struct {
	Items      []derivedItem `json:"items"`
	ServerTime int64         `json:"serverTime"`
}

// hDerivedRegister serves POST /v1/groups/{groupId}/derived/register (B12).
// memberGate verifies the member identity over (method, groupId, body); the
// body is then translated to a DerivedAddressRecord and persisted via the
// repo. coorddb.ErrDerivedGroupMissing -> 404 (the path's groupId has no
// parent groups row); coorddb.ErrDerivedAddressConflict -> 409 STATE_CONFLICT
// (api.md B12: existing (groupId, index) with a different address pair);
// idempotent match returns 200 with the same shape as a fresh insert.
func (c *Coord) hDerivedRegister(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupId")
	memberID := r.Header.Get("X-Member-Id")
	var b derivedRegisterBody
	raw, ok := c.readJSON(w, r, &b)
	if !ok {
		return
	}
	if !c.memberGate(w, r, groupID, memberID, "B12:derived:register", raw) {
		return
	}
	if b.EVMAddress == "" || b.TronAddress == "" {
		c.writeErr(w, errInvalidEnvelope("evmAddress and tronAddress are required"))
		return
	}
	var childPub []byte
	if b.ChildPubkeyHex != "" {
		decoded, err := hex.DecodeString(b.ChildPubkeyHex)
		if err != nil {
			c.writeErr(w, errInvalidEnvelope("childPubkeyHex is not hex"))
			return
		}
		// SEC1 compressed = 33B (address-derivation.md §7.bis.1). Reject any
		// other length up front so an out-of-shape audit aid cannot land in
		// the table.
		if len(decoded) != 33 {
			c.writeErr(w, errInvalidEnvelope("childPubkeyHex must be 33 bytes (SEC1 compressed)"))
			return
		}
		childPub = decoded
	}

	rec := coorddb.DerivedAddressRecord{
		GroupID:     groupID,
		ChildIndex:  b.Index,
		EVMAddress:  b.EVMAddress,
		TronAddress: b.TronAddress,
		ChildPubkey: childPub,
		CreatedAt:   c.clock.Now().Unix(),
	}
	if err := c.store.RegisterDerivedAddress(r.Context(), rec); err != nil {
		c.writeErr(w, mapDerivedErr(err))
		return
	}
	c.writeJSON(w, http.StatusOK, map[string]any{"registered": true})
}

// hDerivedList serves GET /v1/groups/{groupId}/derived?since=… (B12). The
// `since` cursor is unix seconds (matches the table's created_at column)
// and pages strictly-greater so a client can resume by passing the last
// createdAt back; sinceSec=0 returns the full set. memberGate enforces
// owning-member-only access (B-side, §7.bis.3): A-side exposure is forbidden
// by §F1, so this route is registered behind memberGate exclusively.
func (c *Coord) hDerivedList(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupId")
	memberID := r.Header.Get("X-Member-Id")
	if !c.memberGate(w, r, groupID, memberID, "B12:derived:list", []byte(r.URL.RawQuery)) {
		return
	}
	var sinceSec int64
	if s := r.URL.Query().Get("since"); s != "" {
		var err error
		if sinceSec, err = strconv.ParseInt(s, 10, 64); err != nil {
			c.writeErr(w, errInvalidEnvelope("since is not an integer"))
			return
		}
	}
	rows, err := c.store.ListDerivedAddresses(r.Context(), groupID, sinceSec)
	if err != nil {
		c.writeErr(w, asAPIError(err))
		return
	}
	items := make([]derivedItem, 0, len(rows))
	for _, rec := range rows {
		item := derivedItem{
			Index:       rec.ChildIndex,
			EVMAddress:  rec.EVMAddress,
			TronAddress: rec.TronAddress,
			CreatedAt:   rec.CreatedAt,
		}
		if len(rec.ChildPubkey) != 0 {
			item.ChildPubkeyHex = hex.EncodeToString(rec.ChildPubkey)
		}
		items = append(items, item)
	}
	c.writeJSON(w, http.StatusOK, derivedListResponse{
		Items:      items,
		ServerTime: unixMillis(c.clock.Now()),
	})
}

// mapDerivedErr maps the repo sentinels to the api.md C-table codes.
// ErrDerivedGroupMissing -> 404 NOT_FOUND (the groupId in the URL has no
// parent in `groups`); ErrDerivedAddressConflict -> 409 STATE_CONFLICT
// (api.md B12); anything else flows through asAPIError (LOCKED, INTERNAL).
func mapDerivedErr(err error) *apiError {
	switch {
	case errors.Is(err, coorddb.ErrDerivedGroupMissing):
		return errNotFound("unknown groupId")
	case errors.Is(err, coorddb.ErrDerivedAddressConflict):
		return errStateConflict("derived address conflicts with existing (groupId, index)")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return errInternal()
	default:
		return asAPIError(err)
	}
}
