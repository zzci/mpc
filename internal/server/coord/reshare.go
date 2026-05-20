package coord

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net/http"
	"sort"

	"github.com/zzci/mpc/internal/contract"
)

// distributed-mpc.md §3.bis / api.md B10: coord-mediated reshare. The
// reshare ceremony rotates shares between (or within) committees while
// the master pubkey + chaincode stay invariant (R1 weak-form + xpub
// stability). coord's job here is purely event-orchestration:
//
//   - Validate that the request was signed by an active member of the
//     group (memberGate handles this transparently).
//   - Validate that every new-committee identity is in expected_members,
//     and each oldMemberSig+newMemberSig is a valid co-sig over the
//     canonical reshare preimage (R3 strict-set).
//   - Dispatch reshare-START to the union (old ∪ new) so every party can
//     run the tss-lib reshare ceremony.
//
// As with B9, the actual reshare ceremony runs on member devices over
// relay+Noise; coord never sees shares. DM-6 will commit the post-reshare
// state via the cross-member attestation aggregation; R7 append-only
// guarantees groups.ecdsa_pubkey does not change across reshare.

// reshareRequest is the api.md B10 payload. oldMemberSig is the
// old-committee proposer's signature over the canonical preimage;
// newMemberSet/newMemberSig list the identity pubkeys of the new committee
// and each new member's pre-commitment signature (so a new member cannot
// silently be added without their own consent).
type reshareRequest struct {
	SessionID    string   `json:"sessionID"`
	OldMemberSig []byte   `json:"oldMemberSig"`
	NewMemberSet []string `json:"newMemberSet"`
	NewMemberSig [][]byte `json:"newMemberSig"`
	Deadline     int64    `json:"deadline"`
}

// reshareStartEvent is the coord→member START event for a reshare. It
// carries both committees so each party can compute its (oldIndex,
// newIndex) pair for the tss-lib reshare driver.
type reshareStartEvent struct {
	Type         string   `json:"type"`
	SessionID    string   `json:"sessionID"`
	GroupID      string   `json:"groupId"`
	OldMemberSet []string `json:"oldMemberSet"`
	NewMemberSet []string `json:"newMemberSet"`
	ThresholdT   int      `json:"t"`
	PartiesN     int      `json:"n"`
	Deadline     int64    `json:"deadline"`
}

var reshareRequestDomain = append([]byte("TSS-COORD-RESHARE-REQUEST-CANONICAL-v1"), 0x00)

// reshareRequestDigest binds (sessionID, oldMemberSet bytes, newMemberSet
// bytes, deadline). Both committees are part of the preimage so any
// substitution breaks signatures.
func reshareRequestDigest(r *reshareRequest, oldSet, newSet [][]byte) [32]byte {
	var b []byte
	b = append(b, reshareRequestDomain...)
	b = putStr(b, r.SessionID)
	b = binary.BigEndian.AppendUint32(b, uint32(len(oldSet)))
	for _, m := range oldSet {
		b = putLP(b, m)
	}
	b = binary.BigEndian.AppendUint32(b, uint32(len(newSet)))
	for _, m := range newSet {
		b = putLP(b, m)
	}
	b = putI64(b, r.Deadline)
	return sha256.Sum256(b)
}

// hReshare handles POST /v1/groups/{groupId}/reshare (api.md B10).
func (c *Coord) hReshare(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupId")
	memberID := r.Header.Get("X-Member-Id")
	var b reshareRequest
	raw, ok := c.readJSON(w, r, &b)
	if !ok {
		return
	}
	if !c.memberGate(w, r, groupID, memberID, "B10:reshare", raw) {
		return
	}
	if b.SessionID == "" {
		c.writeErr(w, errInvalidEnvelope("missing sessionID"))
		return
	}
	if b.Deadline <= unixMillis(c.clock.Now()) {
		c.writeErr(w, errInvalidEnvelope("deadline must be in the future"))
		return
	}
	if len(b.NewMemberSet) == 0 || len(b.NewMemberSig) != len(b.NewMemberSet) {
		c.writeErr(w, errInvalidEnvelope("newMemberSet/newMemberSig length mismatch"))
		return
	}

	newSet, err := decodeIdentityHexSet(b.NewMemberSet)
	if err != nil {
		c.writeErr(w, errInvalidEnvelope("newMemberSet: "+err.Error()))
		return
	}
	if err := c.expectedSubset(groupID, newSet); err != nil {
		c.writeErr(w, errExpectedMemberMismatch("newMemberSet not subset of expected_members"))
		return
	}

	// Group must already exist (reshare is post-keygen).
	g, gerr := c.db.group(r.Context(), groupID)
	if gerr != nil {
		if errors.Is(gerr, errGroupNotFound) {
			c.writeErr(w, errNotFound("group not provisioned"))
			return
		}
		c.writeErr(w, asAPIError(gerr))
		return
	}
	if len(g.ECDSAPubkey) == 0 {
		c.writeErr(w, errStateConflict("group has no ecdsa_pubkey yet; run keygen first"))
		return
	}

	// Old committee = current active members, sorted by memberId for a
	// canonical wire order both coord and clients can reconstruct.
	rows, derr := c.db.members(r.Context(), groupID)
	if derr != nil {
		c.writeErr(w, asAPIError(derr))
		return
	}
	type activeMember struct {
		memberID string
		idPub    []byte
	}
	var actives []activeMember
	for _, mr := range rows {
		if mr.Status == "active" {
			actives = append(actives, activeMember{memberID: mr.MemberID, idPub: mr.IdentityPubkey})
		}
	}
	sort.Slice(actives, func(i, j int) bool { return actives[i].memberID < actives[j].memberID })
	oldSet := make([][]byte, len(actives))
	oldRecipients := make([]string, len(actives))
	for i, a := range actives {
		oldSet[i] = a.idPub
		oldRecipients[i] = a.memberID
	}

	proposerKey, pfound, perr := c.db.activeMember(r.Context(), groupID, memberID)
	if perr != nil {
		c.writeErr(w, asAPIError(perr))
		return
	}
	if !pfound {
		c.writeErr(w, errForbidden("proposer is not an active member of this group"))
		return
	}
	if !c.expectedHas(groupID, proposerKey) {
		c.writeErr(w, errExpectedMemberMismatch("proposer identity not in expected_members"))
		return
	}

	digest := reshareRequestDigest(&b, oldSet, newSet)
	if err := contract.VerifyDigest(proposerKey, digest, b.OldMemberSig); err != nil {
		c.writeErr(w, errUnauthenticated("invalid oldMemberSig"))
		return
	}
	for i, sig := range b.NewMemberSig {
		if err := contract.VerifyDigest(newSet[i], digest, sig); err != nil {
			c.writeErr(w, errUnauthenticated("invalid newMemberSig"))
			return
		}
	}

	// Recipients = old ∪ already-active-new (a brand-new identity not yet
	// in group_members has no B6 long-poll target and consumes its START
	// through the out-of-band notification webhook).
	newRecipients := c.activeIdentitiesToMemberIDs(r.Context(), groupID, newSet)
	recipients := mergeUnique(oldRecipients, newRecipients)

	event := reshareStartEvent{
		Type:         "reshare-START",
		SessionID:    b.SessionID,
		GroupID:      groupID,
		OldMemberSet: identitySetHex(oldSet),
		NewMemberSet: b.NewMemberSet,
		ThresholdT:   g.ThresholdT,
		PartiesN:     len(newSet),
		Deadline:     b.Deadline,
	}
	c.hub.publish(groupID, recipients, event)
	c.log.Info("reshare dispatched", "groupId", groupID, "sessionId", b.SessionID,
		"oldN", len(oldSet), "newN", len(newSet))
	c.writeJSON(w, http.StatusAccepted, map[string]any{
		"sessionID": b.SessionID,
		"accepted":  true,
		"startsAt":  unixMillis(c.clock.Now()),
	})
}

// activeIdentitiesToMemberIDs returns the memberIds of identity pubkeys
// that are already active members of groupID. Unknown identities are
// silently skipped (they receive START via the out-of-band notification
// path; coord's B6 longpoll only reaches existing members).
func (c *Coord) activeIdentitiesToMemberIDs(ctx context.Context, groupID string, ids [][]byte) []string {
	rows, err := c.db.members(ctx, groupID)
	if err != nil {
		return nil
	}
	var out []string
	for _, idPub := range ids {
		for _, mr := range rows {
			if mr.Status == "active" && bytes.Equal(mr.IdentityPubkey, idPub) {
				out = append(out, mr.MemberID)
				break
			}
		}
	}
	return out
}

func mergeUnique(a, b []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(a)+len(b))
	for _, x := range a {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	for _, x := range b {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// identitySetHex re-encodes a raw-byte identity-pubkey set back to hex for
// inclusion in the dispatched event JSON.
func identitySetHex(set [][]byte) []string {
	out := make([]string, len(set))
	for i, b := range set {
		out[i] = hex.EncodeToString(b)
	}
	return out
}
