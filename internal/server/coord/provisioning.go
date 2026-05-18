package coord

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/royqta/mcp-wallet/internal/contract"
	"github.com/royqta/mcp-wallet/internal/server/coorddb"
)

// S-002 group/membership provisioning (docs/spec/group-provisioning.md). The
// trust anchor is established here, so authorization is the self-sovereign
// model of §6 — group-key signature ⊕ ≥t member identity co-signatures — with
// no admin/service issuance. Transport auth is NOT required (§6.3: the payload
// self-attests); rate limiting against registration storms is layered in
// api.go.

// verifyCoSigs checks ≥ minT distinct member co-signatures over digest, each
// against the identity pubkey resolved by keyOf (§6.2). Returns the set of
// signer memberIds on success.
func verifyCoSigs(cosigs []coSig, digest [32]byte, minT int, keyOf func(memberID string) ([]byte, bool)) (map[string]bool, error) {
	signers := map[string]bool{}
	for _, cs := range cosigs {
		if signers[cs.MemberID] {
			continue // co-signers must be pairwise distinct; ignore dups
		}
		pub, ok := keyOf(cs.MemberID)
		if !ok {
			return nil, errForbidden("co-signer is not an authorized member")
		}
		if err := contract.VerifyDigest(pub, digest, cs.Sig); err != nil {
			return nil, errUnauthenticated("invalid member co-signature")
		}
		signers[cs.MemberID] = true
	}
	if len(signers) < minT {
		return nil, errInvalidEnvelope("insufficient member co-signatures")
	}
	return signers, nil
}

// provisionGroup handles POST /v1/groups (§3). First valid self-attested
// registration wins the groupId; a same-groupId/same-ecdsaPubkey repeat is
// idempotent, a different binding is 409 (§3.3).
func (c *Coord) provisionGroup(ctx context.Context, raw []byte) (string, *apiError) {
	var p groupProvisioning
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", errInvalidEnvelope("malformed JSON body")
	}
	if p.Version != contract.EnvelopeVersionV1 {
		return "", errInvalidEnvelope("unsupported payload version")
	}
	if p.ThresholdT <= 0 || p.PartiesN <= 0 || p.ThresholdT > p.PartiesN {
		return "", errInvalidEnvelope("invalid thresholdT/partiesN")
	}
	if len(p.Members) != p.PartiesN {
		return "", errInvalidEnvelope("members count != partiesN")
	}
	digest, err := groupProvisionDigest(&p)
	if err != nil {
		return "", errInvalidEnvelope("payload not canonicalizable")
	}
	// §6.1 group-key signature over H(payload) (provisioning: key from payload,
	// TOFU first establishment §6.2).
	if err := contract.VerifyDigest(p.GroupPubkey, digest, p.GroupSig); err != nil {
		return "", errUnauthenticated("invalid groupSig")
	}
	// §6.2 ≥t distinct member co-signatures over the same H(payload), keyed by
	// the payload-declared identity pubkeys.
	idx := map[string][]byte{}
	for _, m := range p.Members {
		idx[m.MemberID] = m.IdentityPubkey
	}
	if _, aerr := verifyCoSigs(p.MemberCoSigs, digest, p.ThresholdT,
		func(id string) ([]byte, bool) { k, ok := idx[id]; return k, ok }); aerr != nil {
		return "", asAPIError(aerr)
	}

	// §3.3 binding: idempotent on identical ecdsaPubkey, else 409.
	if g, err := c.db.group(ctx, p.GroupID); err == nil {
		if bytes.Equal(g.ECDSAPubkey, p.ECDSAPubkey) {
			return "PROVISIONED", nil
		}
		return "", errStateConflict("groupId already bound to a different ecdsaPubkey")
	} else if !errors.Is(err, errGroupNotFound) {
		return "", asAPIError(err)
	}

	members := make([]coorddb.MemberRecord, len(p.Members))
	for i, m := range p.Members {
		members[i] = coorddb.MemberRecord{MemberID: m.MemberID, IdentityPubkey: m.IdentityPubkey}
	}
	rec := coorddb.GroupRecord{
		GroupID:     p.GroupID,
		ECDSAPubkey: p.ECDSAPubkey,
		GroupPubkey: p.GroupPubkey,
		ThresholdT:  p.ThresholdT,
		PartiesN:    p.PartiesN,
		Epoch:       0,
		CreatedAt:   p.CreatedAt,
	}
	if err := c.store.ProvisionGroup(ctx, rec, members); err != nil {
		return "", asAPIError(err)
	}
	c.log.Info("group provisioned", "groupId", p.GroupID, "n", p.PartiesN, "t", p.ThresholdT)
	return "PROVISIONED", nil
}

// updateMembership handles POST /v1/groups/{groupId}/membership (§4). Reshare
// authorization is the *stored* group key ⊕ ≥(old t) *stored active* member
// co-signatures (§4.2 anti-malicious-modification), main-pubkey-invariant
// assertion, and a strictly-monotonic epoch (anti-rollback). It is applied in
// one BEGIN IMMEDIATE transaction (§7); D-001 has no membership helper and is
// unmodifiable, so the raw SQL lives here.
func (c *Coord) updateMembership(ctx context.Context, groupID string, raw []byte) (int64, string, *apiError) {
	var p membershipUpdate
	if err := json.Unmarshal(raw, &p); err != nil {
		return 0, "", errInvalidEnvelope("malformed JSON body")
	}
	if p.Version != contract.EnvelopeVersionV1 {
		return 0, "", errInvalidEnvelope("unsupported payload version")
	}
	if p.GroupID != groupID {
		return 0, "", errInvalidEnvelope("path/payload groupId mismatch")
	}
	if p.ThresholdT <= 0 || p.PartiesN <= 0 || p.ThresholdT > p.PartiesN {
		return 0, "", errInvalidEnvelope("invalid thresholdT/partiesN")
	}
	g, err := c.db.group(ctx, groupID)
	if err != nil {
		if errors.Is(err, errGroupNotFound) {
			return 0, "", errNotFound("group not provisioned")
		}
		return 0, "", asAPIError(err)
	}
	// Main-pubkey-invariant assertion (§4.1, sdk.md:74).
	if !bytes.Equal(p.ECDSAPubkeyAssert, g.ECDSAPubkey) {
		return 0, "", errStateConflict("ecdsaPubkey assertion failed (main pubkey is invariant)")
	}
	// Monotonic epoch (anti-rollback). epoch <= current is a conflict unless
	// the request is an exact replay of the already-applied state (§7
	// idempotency), in which case it returns the original result.
	if p.Epoch <= g.Epoch {
		if p.Epoch == g.Epoch && c.membershipMatches(ctx, &p) {
			return g.Epoch, "UPDATED", nil
		}
		return 0, "", errStateConflict("epoch is not strictly greater than current")
	}
	digest, derr := membershipUpdateDigest(&p)
	if derr != nil {
		return 0, "", errInvalidEnvelope("payload not canonicalizable")
	}
	// §6.1 group-key signature: reshare uses the *stored* group_pubkey
	// (authorizes this rotation), not the payload's.
	if err := contract.VerifyDigest(g.GroupPubkey, digest, p.GroupSig); err != nil {
		return 0, "", errUnauthenticated("invalid groupSig (not signed by current group key)")
	}
	// §4.2 co-signers must be *stored active* members and number >= old t.
	stored, err := c.db.members(ctx, groupID)
	if err != nil {
		return 0, "", asAPIError(err)
	}
	activeKey := map[string][]byte{}
	for _, m := range stored {
		if m.Status == "active" {
			activeKey[m.MemberID] = m.IdentityPubkey
		}
	}
	if _, aerr := verifyCoSigs(p.MemberCoSigs, digest, g.ThresholdT,
		func(id string) ([]byte, bool) { k, ok := activeKey[id]; return k, ok }); aerr != nil {
		return 0, "", asAPIError(aerr)
	}

	if err := c.applyMembership(ctx, &p); err != nil {
		return 0, "", asAPIError(err)
	}
	c.log.Info("membership updated", "groupId", groupID, "epoch", p.Epoch)
	return p.Epoch, "UPDATED", nil
}

// membershipMatches reports whether the requested state is already in effect
// (idempotent replay detection for §7).
func (c *Coord) membershipMatches(ctx context.Context, p *membershipUpdate) bool {
	ms, err := c.db.members(ctx, p.GroupID)
	if err != nil {
		return false
	}
	status := map[string]string{}
	for _, m := range ms {
		status[m.MemberID] = m.Status
	}
	for _, a := range p.AddedMembers {
		if status[a.MemberID] != "active" {
			return false
		}
	}
	for _, r := range p.RemovedMemberIDs {
		if status[r] != "removed" {
			return false
		}
	}
	return true
}

// applyMembership applies the reshare in one transaction (§4.1/§7): added
// upsert to active, removed set to removed (never physically deleted —
// audit/history retention), groups t/n/group_pubkey/epoch/updated_at bumped,
// and one update audit event.
func (c *Coord) applyMembership(ctx context.Context, p *membershipUpdate) error {
	return c.store.WithTx(ctx, func(tx *sql.Tx) error {
		for _, m := range p.AddedMembers {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO group_members (group_id, member_id, identity_pubkey, status)
				 VALUES (?, ?, ?, 'active')
				 ON CONFLICT(group_id, member_id)
				 DO UPDATE SET identity_pubkey = excluded.identity_pubkey, status = 'active'`,
				p.GroupID, m.MemberID, m.IdentityPubkey); err != nil {
				return fmt.Errorf("coord: upsert added member: %w", err)
			}
		}
		for _, id := range p.RemovedMemberIDs {
			if _, err := tx.ExecContext(ctx,
				`UPDATE group_members SET status = 'removed'
				  WHERE group_id = ? AND member_id = ?`, p.GroupID, id); err != nil {
				return fmt.Errorf("coord: remove member: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE groups
			    SET threshold_t = ?, parties_n = ?, group_pubkey = ?, epoch = ?, updated_at = ?
			  WHERE group_id = ?`,
			p.ThresholdT, p.PartiesN, p.GroupPubkey, p.Epoch, p.UpdatedAt, p.GroupID); err != nil {
			return fmt.Errorf("coord: update group: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO request_events (request_id, from_status, to_status, actor, detail, at)
			 VALUES (?, NULL, 'MEMBERSHIP_UPDATED', 'coord', ?, ?)`,
			p.GroupID, fmt.Sprintf("epoch=%d", p.Epoch), p.UpdatedAt); err != nil {
			return fmt.Errorf("coord: membership audit: %w", err)
		}
		return nil
	})
}

// groupPublicView is the GET /v1/groups/{id} response (§5.1) — public columns
// only, never any share/private key. ActiveMembers/Degraded are derived (not
// stored) so the member SDK can surface lost-member recovery urgency
// (docs/design/mcp/sdk.md §7: "窗口期冗余下降需提示尽快完成").
type groupPublicView struct {
	GroupID       string            `json:"groupId"`
	ECDSAPubkey   []byte            `json:"ecdsaPubkey"`
	GroupPubkey   []byte            `json:"groupPubkey"`
	ThresholdT    int               `json:"thresholdT"`
	PartiesN      int               `json:"partiesN"`
	Epoch         int64             `json:"epoch"`
	EVMAddress    string            `json:"evmAddress"`
	TronAddress   string            `json:"tronAddress"`
	ActiveMembers int               `json:"activeMembers"`
	Degraded      bool              `json:"degraded"`
	Members       []groupMemberView `json:"members"`
}

type groupMemberView struct {
	MemberID       string `json:"memberId"`
	IdentityPubkey []byte `json:"identityPubkey"`
	Status         string `json:"status"`
}

func (c *Coord) groupPublic(ctx context.Context, groupID string) (*groupPublicView, *apiError) {
	g, err := c.db.group(ctx, groupID)
	if err != nil {
		if errors.Is(err, errGroupNotFound) {
			return nil, errNotFound("group not provisioned")
		}
		return nil, asAPIError(err)
	}
	ms, err := c.db.members(ctx, groupID)
	if err != nil {
		return nil, asAPIError(err)
	}
	v := &groupPublicView{
		GroupID: g.GroupID, ECDSAPubkey: g.ECDSAPubkey, GroupPubkey: g.GroupPubkey,
		ThresholdT: g.ThresholdT, PartiesN: g.PartiesN, Epoch: g.Epoch,
		EVMAddress: g.EVMAddress, TronAddress: g.TronAddress,
	}
	for _, m := range ms {
		if m.Status == "active" {
			v.ActiveMembers++
		}
		v.Members = append(v.Members, groupMemberView(m))
	}
	// Degraded: the committee is below its provisioned strength — a share is
	// missing (lost-member window), so recovery resharing should complete
	// soon. ActiveMembers >= PartiesN means full redundancy (sdk.md §7).
	v.Degraded = v.ActiveMembers < g.PartiesN
	return v, nil
}
