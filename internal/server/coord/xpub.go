package coord

import (
	"encoding/hex"
	"errors"
	"net/http"
)

// hXpub serves api.md B8 GET /v1/groups/{groupId}/xpub.
//
// Owning-member-only release of the HD extended public key (Q_master,
// chaincode) per docs/design/mcp/address-derivation.md §7 (F1):
//
//   - memberGate (B-side member-signed authn) gates the route, so the response
//     reaches only the owning device of an active member of the group.
//   - The chaincode is **never** exposed on the A surface or any external path
//     (§7 / F1 hard constraint).
//   - Legacy groups (chaincode IS NULL, F5): the handler returns 409
//     LEGACY_NO_HD; clients must continue to use the multi-group multi-address
//     path. Legacy → HD migration is intentionally not offered (would require
//     reshare, forbidden by Q3 / address-derivation.md §8).
//
// On success the response is `{ ecdsaPubkeyHex, chaincodeHex }` per api.md B8:
// hex-encoded bytes — `ecdsa_pubkey` is returned exactly as provisioned by
// S-002 (uncompressed 65B), the 32-byte chaincode as the commit-reveal HKDF
// output (§3 / §4).
func (c *Coord) hXpub(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupId")
	memberID := r.Header.Get("X-Member-Id")
	// memberGate enforces B1 (member-signed authn) + groupId isolation; an
	// inactive / unknown member, a cross-group attempt, a stale ts, a replayed
	// nonce or a bad EC signature all fail-close here without touching the DB
	// chaincode column. The signed params for this GET are the raw query
	// string (empty), matching the server↔client byte-identical contract used
	// by every other B-GET endpoint.
	if !c.memberGate(w, r, groupID, memberID, "B8:xpub", []byte(r.URL.RawQuery)) {
		return
	}
	pubkey, chaincode, hasChaincode, err := c.db.xpub(r.Context(), groupID)
	if errors.Is(err, errGroupNotFound) {
		// Reachable only after memberGate accepted a signed request claiming
		// this group, yet the row disappeared in the same window — surface as
		// 404 (consistent with api.md C-table) rather than leaking detail.
		c.writeErr(w, errNotFound("unknown groupId"))
		return
	}
	if err != nil {
		c.writeErr(w, asAPIError(err))
		return
	}
	if !hasChaincode {
		// F5: legacy group, single-address path; no HD release.
		c.writeErr(w, errLegacyNoHD())
		return
	}
	c.writeJSON(w, http.StatusOK, &xpubView{
		ECDSAPubkeyHex: hex.EncodeToString(pubkey),
		ChaincodeHex:   hex.EncodeToString(chaincode),
	})
}

// xpubView is the api.md B8 response body. Fields are explicit (no embedded
// types) and hex-encoded so the wire shape stays exactly the contract.
type xpubView struct {
	ECDSAPubkeyHex string `json:"ecdsaPubkeyHex"`
	ChaincodeHex   string `json:"chaincodeHex"`
}
