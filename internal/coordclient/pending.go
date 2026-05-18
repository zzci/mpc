package coordclient

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/zzci/mpc/internal/contract"
)

// PendingItem is one entry of the B3 pending list (api.md:43-46). It embeds
// the authoritative contract.SigningRequest so contract's canonicalization /
// proposerSig / metaHash checks apply directly.
//
// api.md implementation closure (docs/design/ unchanged): the coord hPending JSON
// map does NOT carry the envelope `version` field, while only
// EnvelopeVersionV1 is ever accepted at ingestion and rebuilt for START
// (internal/server/coord coord.go rebuildEnvelope fixes Version=V1). The
// client therefore restores Version=contract.EnvelopeVersionV1 on each item
// before verification, mirroring the server's own reconstruction so the
// proposerSig canonical preimage (which includes version) matches
// bit-identically.
type PendingItem struct {
	contract.SigningRequest
	Status       string `json:"status"`
	RemainingTTL int64  `json:"remainingTTL"` // seconds
}

// pendingResponse mirrors the coord hPending body (api.md:43-46).
type pendingResponse struct {
	Items      []PendingItem `json:"items"`
	ServerTime int64         `json:"serverTime"` // unix ms
}

// Pending pulls the member-visible, unexpired requests for this group
// (api.md B3, GET /v1/groups/{groupId}/pending?since=…). since is a unix-ms
// cursor: pass 0 for the full visible set, or the previous call's serverTime
// to page forward. It returns the items, the coord serverTime to use as the
// next cursor, and any error. Each item already had Version restored to V1.
func (c *Client) Pending(ctx context.Context, since int64) ([]PendingItem, int64, error) {
	rawQuery := ""
	if since > 0 {
		rawQuery = "since=" + strconv.FormatInt(since, 10)
	}
	raw, err := c.do(ctx, request{
		method:   http.MethodGet,
		path:     "/v1/groups/" + c.groupID + "/pending",
		rawQuery: rawQuery,
		authVerb: "B3:pending",
	})
	if err != nil {
		return nil, 0, err
	}
	var resp pendingResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, 0, fmt.Errorf("coordclient: decode pending: %w", err)
	}
	for i := range resp.Items {
		resp.Items[i].Version = contract.EnvelopeVersionV1
	}
	return resp.Items, resp.ServerTime, nil
}

// IsExpired reports whether the item's TTL has elapsed. coord already excludes
// expired items from B3 (api.md:46), but a member device re-checks before
// acting (TTL is a first-class invariant, PLAN.md §7): a non-positive
// remaining TTL means do not approve or sign.
func (it *PendingItem) IsExpired() bool { return it.RemainingTTL <= 0 }

// Verify runs the pre-MPC envelope checks contract owns (protocol.md §1):
// version support, metaHash == H(businessInfo), and proposerSig over the
// canonical preimage. proposerPub is the proposer's secp256k1 public key. It
// does NOT perform tx-decode/digest re-derivation (that is the tx-decode
// module's responsibility, docs/design/mcp/sdk.md §4).
func (it *PendingItem) Verify(proposerPub []byte) error {
	if err := contract.CheckEnvelopeVersion(&it.SigningRequest); err != nil {
		return err
	}
	if err := contract.VerifyMetaHash(&it.SigningRequest); err != nil {
		return err
	}
	return contract.VerifyProposerSig(&it.SigningRequest, proposerPub)
}

// VerifySelfDescribing is Verify with the coord default proposer-key
// convention (internal/server/coord auth.go defaultProposerKey): the proposer
// identifier is its hex-encoded secp256k1 public key. Use Verify with an
// explicit key if a registry resolver is in use instead.
func (it *PendingItem) VerifySelfDescribing() error {
	pub, err := hex.DecodeString(it.Proposer)
	if err != nil {
		return fmt.Errorf("coordclient: proposer is not a hex public key: %w", err)
	}
	return it.Verify(pub)
}
