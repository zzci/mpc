package mobileapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/zzci/mpc/internal/coordclient"
)

// xpubFetchRequest is the FetchXpub reqJSON (docs/design/mcp/address-
// derivation.md §7 — CLI/SDK xpub fetch). Same flat shape as
// FetchTransactions's reqJSON (coordfetch.go fetchRequest) so the host
// pipeline that already supplies coordBaseURL/groupId/memberId/memberKeyHex
// for fetching transactions reuses the very same inputs to fetch the xpub
// — no host-side schema divergence between the two coord member-API calls.
type xpubFetchRequest struct {
	CoordBaseURL string `json:"coordBaseURL"`
	GroupID      string `json:"groupId"`
	MemberID     string `json:"memberId"`
	MemberKeyHex string `json:"memberKeyHex"`
}

// xpubFetchResponse is the FetchXpub result. Bytes are hex-encoded so the
// flat (string-only) gomobile surface stays exact and the consumer can feed
// the hex straight into the offline derive path without parsing back to []byte
// first (docs/design/mcp/address-derivation.md §7: xpub is owning-member-only
// but the client may cache and use it offline).
type xpubFetchResponse struct {
	ECDSAPubkeyHex string `json:"ecdsaPubkeyHex"`
	ChaincodeHex   string `json:"chaincodeHex"`
}

// FetchXpub pulls the HD extended public key for the caller's group through
// the coord member API (api.md B8). It signs the request with the device
// member identity key and returns the verbatim B8 hex view as JSON:
// `{ecdsaPubkeyHex, chaincodeHex}`. The chaincode is owning-member-only by
// contract (F1) — never expose it on any A-surface or to an external caller.
//
// Errors are surfaced verbatim from coord: 409 LEGACY_NO_HD means the group
// predates HD and the caller MUST fall back to the multi-group multi-address
// path (F5); 503 LOCKED is the same transient backoff signal the rest of the
// member API uses. A missing or malformed member key is rejected before any
// network call. gomobile-flat: string in, string + error out (mirrors
// FetchTransactions).
func (s *SDK) FetchXpub(reqJSON string) (string, error) {
	var req xpubFetchRequest
	if err := json.Unmarshal([]byte(reqJSON), &req); err != nil {
		return "", fmt.Errorf("%s: invalid reqJSON: %w", CodeBadConfig, err)
	}
	if req.CoordBaseURL == "" || req.GroupID == "" || req.MemberID == "" {
		return "", fmt.Errorf("%s: coordBaseURL, groupId and memberId are required", CodeBadConfig)
	}
	keyBytes, err := hex.DecodeString(req.MemberKeyHex)
	if err != nil || len(keyBytes) != 32 {
		return "", fmt.Errorf("%s: memberKeyHex must be a 32-byte hex secp256k1 key", CodeBadConfig)
	}
	cl, err := coordclient.NewFromKeyBytes(req.CoordBaseURL, req.GroupID, req.MemberID, keyBytes)
	if err != nil {
		return "", fmt.Errorf("%s: %w", CodeInternal, err)
	}
	xp, err := cl.Xpub(context.Background())
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(xpubFetchResponse{
		ECDSAPubkeyHex: hex.EncodeToString(xp.ECDSAPubkey),
		ChaincodeHex:   hex.EncodeToString(xp.Chaincode),
	})
	if err != nil {
		return "", fmt.Errorf("%s: marshal xpub: %w", CodeInternal, err)
	}
	return string(out), nil
}
