package mobileapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zzci/mpc/internal/contract"
	"github.com/zzci/mpc/internal/coordclient"
	"github.com/zzci/mpc/internal/txdecode"
)

// fetchRequest is the FetchTransactions reqJSON (docs/design/mcp/sdk.md §2.1).
//
// memberKeyHex is the device member identity private key (32-byte secp256k1
// scalar, the keystore export form) the RN host supplies from the device
// keychain: it is exactly the B1 signing key coordclient reproduces
// byte-for-byte. docs/design/mcp/sdk.md §2.1 names the keystore member
// identity key as the signing identity; this build has no in-keystore
// member-identity facility yet (it seals TSS shares only), so the host passes
// the key through the flat reqJSON rather than the SDK reading it — a
// deliberate, additive choice that keeps the crypto kernel and keystore
// untouched.
type fetchRequest struct {
	CoordBaseURL string `json:"coordBaseURL"`
	GroupID      string `json:"groupId"`
	MemberID     string `json:"memberId"`
	MemberKeyHex string `json:"memberKeyHex"`
	Since        int64  `json:"since"`
	RequestID    string `json:"requestId"`
}

// fetchItem is one entry of the FetchTransactions result. aFacts is the
// device-recomputed, digest32-bound A-zone — never the coord display fields.
// It is null when the device could not establish the WYSIWYS binding
// (proposerSig / metaHash / ==digest32), so the host can never render
// unverified coord data as authoritative (docs/design/mcp/sdk.md §2.1/§4).
type fetchItem struct {
	RequestID    string                   `json:"requestId"`
	Status       string                   `json:"status"`
	RemainingTTL int64                    `json:"remainingTTL"`
	Envelope     *contract.SigningRequest `json:"envelope"`
	AFacts       *txdecode.Facts          `json:"aFacts"`
	ABMismatch   []txdecode.Mismatch      `json:"abMismatch"`
}

// fetchResponse is the FetchTransactions return JSON (docs/design/mcp/sdk.md
// §2.1): { serverTime, items:[ … ] }.
type fetchResponse struct {
	ServerTime int64       `json:"serverTime"`
	Items      []fetchItem `json:"items"`
}

// FetchTransactions queries transaction information through the coord member
// API for App listing/detail (docs/design/mcp/sdk.md §2.1). It never enters
// MPC. With reqJSON.requestId set it queries the single-request status
// (api.md A3); otherwise it pulls the group's pending list (api.md B3). The
// A-zone is recomputed device-side (tx-decode, == digest32 double-binding,
// §4) and the coord display fields are never trusted. A coord error — LOCKED
// (503), auth, not-found, expired — is returned verbatim so the host branches
// on the contract; a missing/invalid member key is rejected before any
// network call. gomobile-flat: string in, string + error out.
func (s *SDK) FetchTransactions(reqJSON string) (string, error) {
	var req fetchRequest
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

	ctx := context.Background()
	if req.RequestID != "" {
		return fetchSingle(ctx, cl, req.RequestID)
	}
	return fetchPending(ctx, cl, req.Since)
}

// fetchSingle serves the requestId branch (api.md A3 single-request status).
// The A3 payload has no envelope, so only the status is carried; the coord
// error (LOCKED/expired/not-found) is surfaced verbatim.
func fetchSingle(ctx context.Context, cl *coordclient.Client, requestID string) (string, error) {
	st, err := cl.Status(ctx, requestID)
	if err != nil {
		return "", err
	}
	return marshalResponse(fetchResponse{
		ServerTime: time.Now().UnixMilli(),
		Items:      []fetchItem{{RequestID: st.RequestID, Status: st.Status}},
	})
}

// fetchPending serves the list branch (api.md B3). Each item's A-zone is
// recomputed device-side: proposerSig/metaHash must verify AND the
// re-derived chain digest must equal digest32, else aFacts stays null so the
// host never renders coord-supplied display fields as authoritative
// (docs/design/mcp/sdk.md §2.1/§4 anti-blind-sign invariant).
func fetchPending(ctx context.Context, cl *coordclient.Client, since int64) (string, error) {
	items, serverTime, err := cl.Pending(ctx, since)
	if err != nil {
		return "", err
	}
	out := fetchResponse{ServerTime: serverTime, Items: make([]fetchItem, 0, len(items))}
	dec := txdecode.New()
	for i := range items {
		it := &items[i]
		env := it.SigningRequest
		fi := fetchItem{
			RequestID:    it.RequestID,
			Status:       it.Status,
			RemainingTTL: it.RemainingTTL,
			Envelope:     &env,
		}
		if it.VerifySelfDescribing() == nil {
			if res, derr := dec.Decode(&it.SigningRequest); derr == nil {
				fi.AFacts = res.Facts
				fi.ABMismatch = res.Mismatches
			}
		}
		out.Items = append(out.Items, fi)
	}
	return marshalResponse(out)
}

func marshalResponse(r fetchResponse) (string, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("%s: marshal result: %w", CodeInternal, err)
	}
	return string(b), nil
}
