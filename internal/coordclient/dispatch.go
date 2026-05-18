package coordclient

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/zzci/mpc/internal/contract"
)

// maxDispatchWait is the coord's per-call long-poll ceiling
// (internal/server/coord api.go parseWait caps at 60s).
const maxDispatchWait = 60 * time.Second

// ReceiveStart long-polls for a START signal
// (api.md B6, GET /v1/groups/{groupId}/dispatch?wait=…). Push (FCM/APNs) is
// the primary wake; this is the long-poll compensation (api.md:57). wait is
// clamped to (0, 60s]; on timeout the coord returns an empty object and this
// returns ok=false with a nil START (not an error) so the caller simply polls
// again. A returned START still MUST be re-verified before MPC (protocol.md
// §5): call (*StartSigning wrapper).Verify or contract directly.
func (c *Client) ReceiveStart(ctx context.Context, wait time.Duration) (*contract.StartSigning, bool, error) {
	if wait <= 0 || wait > maxDispatchWait {
		wait = 25 * time.Second
	}
	rawQuery := "wait=" + wait.String()
	raw, err := c.do(ctx, request{
		method:   http.MethodGet,
		path:     "/v1/groups/" + c.groupID + "/dispatch",
		rawQuery: rawQuery,
		authVerb: "B6:dispatch",
	})
	if err != nil {
		return nil, false, err
	}
	var st contract.StartSigning
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, false, fmt.Errorf("coordclient: decode START: %w", err)
	}
	// Empty object => no dispatch within the wait window (api.md:57 long-poll
	// timeout). RequestID is the unambiguous presence marker.
	if st.RequestID == "" {
		return nil, false, nil
	}
	return &st, true, nil
}

// VerifyStart runs the device pre-MPC checks contract owns over a received
// START (protocol.md §5 → §1): envelope version support, metaHash, proposerSig
// over the canonical preimage, and consistency of the outer RequestID with the
// envelope. proposerPub is the proposer's secp256k1 public key. tx-decode /
// digest re-derivation and human review remain the caller's responsibility
// (docs/design/mcp/sdk.md §3-§4); coord is never in st.Signers (protocol.md §5).
func VerifyStart(st *contract.StartSigning, proposerPub []byte) error {
	if st == nil {
		return fmt.Errorf("coordclient: nil START")
	}
	if st.RequestID != st.Envelope.RequestID {
		return fmt.Errorf("%w: START requestId %q != envelope requestId %q",
			contract.ErrInvalidEnvelope, st.RequestID, st.Envelope.RequestID)
	}
	if err := contract.CheckEnvelopeVersion(&st.Envelope); err != nil {
		return err
	}
	if err := contract.VerifyMetaHash(&st.Envelope); err != nil {
		return err
	}
	return contract.VerifyProposerSig(&st.Envelope, proposerPub)
}

// VerifyStartSelfDescribing is VerifyStart with the coord default
// proposer-key convention (proposer identifier == hex secp256k1 public key;
// internal/server/coord auth.go defaultProposerKey).
func VerifyStartSelfDescribing(st *contract.StartSigning) error {
	if st == nil {
		return fmt.Errorf("coordclient: nil START")
	}
	pub, err := hex.DecodeString(st.Envelope.Proposer)
	if err != nil {
		return fmt.Errorf("coordclient: proposer is not a hex public key: %w", err)
	}
	return VerifyStart(st, pub)
}

// NotExpired reports whether the START is still within both the envelope
// expiry and the dispatch deadline at time now (unix ms). The device re-checks
// before entering MPC and again before signing (PLAN.md §7, sdk.md §3); a
// false result MUST hard-reject (no downgrade).
func NotExpired(st *contract.StartSigning, nowUnixMs int64) bool {
	if st == nil {
		return false
	}
	if st.Envelope.Expiry > 0 && nowUnixMs >= st.Envelope.Expiry {
		return false
	}
	if st.Deadline > 0 && nowUnixMs >= st.Deadline {
		return false
	}
	return true
}

// SignerSet returns st.Signers as a lookup set for the caller to test
// self-membership without rescanning the slice.
func SignerSet(st *contract.StartSigning) map[string]struct{} {
	m := make(map[string]struct{}, len(st.Signers))
	for _, s := range st.Signers {
		m[s] = struct{}{}
	}
	return m
}
