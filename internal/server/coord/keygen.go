package coord

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"

	"github.com/zzci/mpc/internal/contract"
)

// distributed-mpc.md §3 / api.md B9: coord-mediated keygen initiation. The
// HTTP body is signed by the initiator at the memberGate (B-side) layer
// over the full request bytes; an in-body proposerSig is additionally
// required by api.md so the request carries an audit-grade binding signed
// over the canonical preimage (independent from transport ts+nonce).
//
// coord's role here is strictly event-orchestration (R3): validate the
// strict-set / dedup / no-existing-pubkey preconditions, then dispatch a
// keygen-START event to each member. coord never sees a share or
// participates in the tss-lib ceremony; the actual key generation runs on
// member devices over relay+Noise. The R7 append-only commit happens in
// the DM-6 收尾 phase via the cross-member attestation aggregation; this
// endpoint only refuses re-keygen if a pubkey is already on file.

// keygenRequest is the api.md B9 payload (sessionID, t, n, memberSet hex,
// deadline, proposerSig). MemberSet contains identity pubkeys as hex
// strings; the canonical preimage uses the decoded bytes so the same
// content yields the same signature regardless of hex casing.
type keygenRequest struct {
	SessionID   string   `json:"sessionID"`
	ThresholdT  int      `json:"t"`
	PartiesN    int      `json:"n"`
	MemberSet   []string `json:"memberSet"`
	Deadline    int64    `json:"deadline"`
	ProposerSig []byte   `json:"proposerSig"`
}

// keygenStartEvent is the coord→member START event for a distributed keygen
// (dispatchHub keygen-START extension). The Type discriminator lets a
// member distinguish event kinds when they share the B6 dispatch channel.
// MemberSet carries the identity pubkeys (hex) in the canonical order; the
// recipient locates its own PartyIndex by comparing its identity pubkey.
type keygenStartEvent struct {
	Type       string   `json:"type"`
	SessionID  string   `json:"sessionID"`
	GroupID    string   `json:"groupId"`
	MemberSet  []string `json:"memberSet"`
	ThresholdT int      `json:"t"`
	PartiesN   int      `json:"n"`
	Deadline   int64    `json:"deadline"`
}

var keygenRequestDomain = append([]byte("TSS-COORD-KEYGEN-REQUEST-CANONICAL-v1"), 0x00)

// keygenRequestDigest is the canonical preimage signed by the initiator's
// identity private key (in-body proposerSig). MemberSet is hashed as raw
// decoded bytes in given order; (sessionID, t, n, memberSet, deadline) are
// fully bound. Domain prefix prevents cross-protocol reuse.
func keygenRequestDigest(r *keygenRequest, memberSet [][]byte) [32]byte {
	var b []byte
	b = append(b, keygenRequestDomain...)
	b = putStr(b, r.SessionID)
	b = putI64(b, int64(r.ThresholdT))
	b = putI64(b, int64(r.PartiesN))
	b = binary.BigEndian.AppendUint32(b, uint32(len(memberSet)))
	for _, m := range memberSet {
		b = putLP(b, m)
	}
	b = putI64(b, r.Deadline)
	return sha256.Sum256(b)
}

// hKeygen handles POST /v1/groups/{groupId}/keygen (api.md B9). memberGate
// has already verified the X-Member-* identity signature; identity-pubkey
// strict-set membership is enforced here against
// coord.external.expected_members. A group that already has an
// ecdsa_pubkey on file is locked from re-keygen (only reshare is
// permitted; R7 append-only spirit + design §3.2 step (d)).
func (c *Coord) hKeygen(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupId")
	memberID := r.Header.Get("X-Member-Id")
	var b keygenRequest
	raw, ok := c.readJSON(w, r, &b)
	if !ok {
		return
	}
	if !c.memberGate(w, r, groupID, memberID, "B9:keygen", raw) {
		return
	}

	if b.SessionID == "" {
		c.writeErr(w, errInvalidEnvelope("missing sessionID"))
		return
	}
	if b.ThresholdT <= 0 || b.PartiesN <= 0 || b.ThresholdT > b.PartiesN {
		c.writeErr(w, errInvalidEnvelope("invalid thresholdT/partiesN"))
		return
	}
	if len(b.MemberSet) != b.PartiesN {
		c.writeErr(w, errInvalidEnvelope("memberSet count != partiesN"))
		return
	}
	if b.Deadline <= unixMillis(c.clock.Now()) {
		c.writeErr(w, errInvalidEnvelope("deadline must be in the future"))
		return
	}

	memberSet, err := decodeIdentityHexSet(b.MemberSet)
	if err != nil {
		c.writeErr(w, errInvalidEnvelope(err.Error()))
		return
	}

	// Pre-cond (a): proposer identity in strict set. memberGate already
	// proved the request was signed by member memberID; we look up that
	// member's identity_pubkey and check the strict set.
	proposerKey, found, derr := c.db.activeMember(r.Context(), groupID, memberID)
	if derr != nil {
		c.writeErr(w, asAPIError(derr))
		return
	}
	if !found {
		c.writeErr(w, errForbidden("proposer is not an active member of this group"))
		return
	}
	if !c.expectedHas(groupID, proposerKey) {
		c.writeErr(w, errExpectedMemberMismatch("proposer identity not in expected_members"))
		return
	}
	// Pre-cond (b): memberSet ⊆ expected_members.
	if err := c.expectedSubset(groupID, memberSet); err != nil {
		c.writeErr(w, errExpectedMemberMismatch("memberSet not subset of expected_members"))
		return
	}
	// Pre-cond (c): in-body proposerSig over canonical preimage.
	digest := keygenRequestDigest(&b, memberSet)
	if err := contract.VerifyDigest(proposerKey, digest, b.ProposerSig); err != nil {
		c.writeErr(w, errUnauthenticated("invalid proposerSig"))
		return
	}
	// Pre-cond (d): group has no ecdsa_pubkey yet (re-keygen forbidden).
	if g, err := c.db.group(r.Context(), groupID); err == nil {
		if len(g.ECDSAPubkey) > 0 {
			c.writeErr(w, errStateConflict("group already has an ecdsa_pubkey; only reshare is permitted"))
			return
		}
	} else if !errors.Is(err, errGroupNotFound) {
		c.writeErr(w, asAPIError(err))
		return
	}

	// Map memberSet (identity pubkeys) to memberIds for dispatch fan-out.
	recipients, aerr := c.identityToMemberIDs(r.Context(), groupID, memberSet)
	if aerr != nil {
		c.writeErr(w, aerr)
		return
	}

	event := keygenStartEvent{
		Type:       "keygen-START",
		SessionID:  b.SessionID,
		GroupID:    groupID,
		MemberSet:  b.MemberSet,
		ThresholdT: b.ThresholdT,
		PartiesN:   b.PartiesN,
		Deadline:   b.Deadline,
	}
	c.hub.publish(groupID, recipients, event)
	c.log.Info("keygen dispatched", "groupId", groupID, "sessionId", b.SessionID, "n", b.PartiesN, "t", b.ThresholdT)
	c.writeJSON(w, http.StatusAccepted, map[string]any{
		"sessionID": b.SessionID,
		"accepted":  true,
		"startsAt":  unixMillis(c.clock.Now()),
	})
}

// identityToMemberIDs resolves identity pubkeys to active memberIds in
// canonical order. An identity that is not an active member of groupID is
// a strict-set / membership mismatch (R3: only declared+active members
// participate); the caller maps the apiError to a 409
// EXPECTED_MEMBER_MISMATCH response.
func (c *Coord) identityToMemberIDs(ctx context.Context, groupID string, ids [][]byte) ([]string, *apiError) {
	rows, err := c.db.members(ctx, groupID)
	if err != nil {
		return nil, asAPIError(err)
	}
	out := make([]string, 0, len(ids))
	for i, idPub := range ids {
		matched := ""
		for _, mr := range rows {
			if mr.Status == "active" && bytes.Equal(mr.IdentityPubkey, idPub) {
				matched = mr.MemberID
				break
			}
		}
		if matched == "" {
			return nil, errExpectedMemberMismatch(fmt.Sprintf("memberSet[%d] is not an active member of this group", i))
		}
		out = append(out, matched)
	}
	return out, nil
}
