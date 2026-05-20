package coord

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"

	"github.com/zzci/mpc/internal/contract"
)

// distributed-mpc.md §3.ter / api.md B11: client-attestation status
// reporting. Each member device periodically (or on a state change)
// reports its local view to coord: do I hold a share, and if so, what
// groupPubkey/chaincode pair did I derive? coord aggregates the
// per-identity attestations and surfaces a group-level groupState so the
// rest of the system can decide what to do (REGISTERED / NEEDS_KEYGEN /
// NEEDS_RESHARE / INCONSISTENT, design §3.ter).
//
// DM-4 scope: accept, validate (memberGate + strict-set + replay), store
// the latest attestation per (groupId, identity), and compute the
// derived groupState.
//
// DM-6 closure-gate addition: after a successful upsert, the handler
// invokes commitAttestationQuorum (coord/provisioning.go). When every
// expected_members identity has reported holdsShare=true with a
// consistent (groupPubkey, chaincode), that orchestrator commits the
// groups + group_members rows + one audit event in ONE transaction
// (coorddb.CommitAttestationQuorum). The R7 invariant is enforced by
// the same primitive + 00006 SQLite triggers (impl §E).
//
// Storage: in-memory map only (R3 says coord may record attestation
// *metadata*; persistence is not a correctness requirement and a restart
// re-collects attestations as members re-poll). The R3 fail-safe — coord
// never sees a share — is preserved: only metadata flows through here.

// attestationBody is the api.md B11 payload.
type attestationBody struct {
	IdentityPubkey string `json:"identityPubkey"`
	HoldsShare     bool   `json:"holdsShare"`
	GroupPubkeyHex string `json:"groupPubkeyHex,omitempty"`
	ChaincodeHex   string `json:"chaincodeHex,omitempty"`
	TS             int64  `json:"ts"`
	Sig            []byte `json:"sig"`
}

// attestationView is the per-identity record coord keeps.
type attestationView struct {
	IdentityPubkey []byte
	HoldsShare     bool
	GroupPubkey    []byte
	Chaincode      []byte
	TS             int64
}

// attestationCache holds the latest attestation per (groupId, identity
// hex). A monotonic TS per identity guards against replay: a TS less than
// or equal to the recorded value is rejected, even within the
// memberGate-nonce window (a malicious member device cannot replay an old
// "holdsShare:false" to roll the group back to NEEDS_RESHARE).
type attestationCache struct {
	mu sync.Mutex
	by map[string]map[string]*attestationView // groupId -> idHex -> view
}

func newAttestationCache() *attestationCache {
	return &attestationCache{by: map[string]map[string]*attestationView{}}
}

// upsert records a fresh attestation; returns false (and does not store)
// when ts is not strictly greater than the existing entry's ts (replay).
func (a *attestationCache) upsert(groupID, idHex string, v *attestationView) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	bucket, ok := a.by[groupID]
	if !ok {
		bucket = map[string]*attestationView{}
		a.by[groupID] = bucket
	}
	if existing, ok := bucket[idHex]; ok {
		if v.TS <= existing.TS {
			return false
		}
	}
	bucket[idHex] = v
	return true
}

// snapshot copies the per-group attestations (for read-only aggregation).
func (a *attestationCache) snapshot(groupID string) []*attestationView {
	a.mu.Lock()
	defer a.mu.Unlock()
	bucket := a.by[groupID]
	out := make([]*attestationView, 0, len(bucket))
	for _, v := range bucket {
		cp := *v
		out = append(out, &cp)
	}
	return out
}

// Attestation-derived groupState values (api.md B11).
const (
	groupStateRegistered   = "REGISTERED"
	groupStateNeedsKeygen  = "NEEDS_KEYGEN"
	groupStateNeedsReshare = "NEEDS_RESHARE"
	groupStateInconsistent = "INCONSISTENT"
	groupStateUnattested   = "UNATTESTED"
)

// attestationDomain separates attestation signatures from every other
// coord-side preimage (R3, replay across protocols forbidden).
var attestationDomain = append([]byte("TSS-COORD-ATTESTATION-CANONICAL-v1"), 0x00)

// attestationDigest binds (groupId, identity, holdsShare, groupPubkey,
// chaincode, ts) so a stolen partial cannot be replayed across groups or
// flipped to a different (holdsShare, pubkey) tuple.
func attestationDigest(groupID string, v *attestationView) [32]byte {
	var b []byte
	b = append(b, attestationDomain...)
	b = putStr(b, groupID)
	b = putLP(b, v.IdentityPubkey)
	var hs byte
	if v.HoldsShare {
		hs = 1
	}
	b = append(b, hs)
	b = putLP(b, v.GroupPubkey)
	b = putLP(b, v.Chaincode)
	b = putI64(b, v.TS)
	return sha256.Sum256(b)
}

// hAttestation handles PUT /v1/groups/{groupId}/attestation (api.md B11).
func (c *Coord) hAttestation(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupId")
	memberID := r.Header.Get("X-Member-Id")
	var b attestationBody
	raw, ok := c.readJSON(w, r, &b)
	if !ok {
		return
	}
	if !c.memberGate(w, r, groupID, memberID, "B11:attestation", raw) {
		return
	}

	idPub, err := decodeIdentityHex(b.IdentityPubkey)
	if err != nil {
		c.writeErr(w, errInvalidEnvelope(err.Error()))
		return
	}
	if !c.expectedHas(groupID, idPub) {
		c.writeErr(w, errExpectedMemberMismatch("attestor identity not in expected_members"))
		return
	}
	memberKey, found, derr := c.db.activeMember(r.Context(), groupID, memberID)
	if derr != nil {
		c.writeErr(w, asAPIError(derr))
		return
	}
	if !found {
		c.writeErr(w, errForbidden("attestor is not an active member of this group"))
		return
	}
	if !bytes.Equal(memberKey, idPub) {
		c.writeErr(w, errForbidden("attestation identityPubkey does not match memberId"))
		return
	}

	var groupPubkey, chaincode []byte
	if b.HoldsShare {
		if b.GroupPubkeyHex == "" || b.ChaincodeHex == "" {
			c.writeErr(w, errInvalidEnvelope("holdsShare=true requires groupPubkeyHex and chaincodeHex"))
			return
		}
		if groupPubkey, err = hex.DecodeString(b.GroupPubkeyHex); err != nil {
			c.writeErr(w, errInvalidEnvelope("groupPubkeyHex: "+err.Error()))
			return
		}
		if chaincode, err = hex.DecodeString(b.ChaincodeHex); err != nil {
			c.writeErr(w, errInvalidEnvelope("chaincodeHex: "+err.Error()))
			return
		}
		if len(groupPubkey) != 33 && len(groupPubkey) != 65 {
			c.writeErr(w, errInvalidEnvelope("groupPubkey length not 33/65"))
			return
		}
		if len(chaincode) != 32 {
			c.writeErr(w, errInvalidEnvelope("chaincode length not 32"))
			return
		}
	}

	view := &attestationView{
		IdentityPubkey: idPub,
		HoldsShare:     b.HoldsShare,
		GroupPubkey:    groupPubkey,
		Chaincode:      chaincode,
		TS:             b.TS,
	}
	digest := attestationDigest(groupID, view)
	if err := contract.VerifyDigest(idPub, digest, b.Sig); err != nil {
		c.writeErr(w, errUnauthenticated("invalid attestation signature"))
		return
	}

	if ok := c.attestations.upsert(groupID, b.IdentityPubkey, view); !ok {
		c.writeErr(w, errStateConflict("attestation ts is not strictly greater than the last recorded ts"))
		return
	}

	// DM-6 closure gate: with the fresh attestation in the cache, try
	// to commit the group row + group_members in one transaction. A
	// not-yet-quorum / inconsistent set is a noop (committed=false,
	// err=nil); an R7 violation surfaces here.
	if _, cerr := c.commitAttestationQuorum(r.Context(), groupID); cerr != nil {
		c.writeErr(w, asAPIError(cerr))
		return
	}

	// Emit an attestation-ACK event on the dispatchHub so the attester's
	// own long-poll learns the post-aggregation groupState without an
	// extra HTTP roundtrip (impl §B DM-4: dispatchHub 扩 attestation-ACK).
	state, ours := c.computeGroupState(r.Context(), groupID)
	c.hub.publish(groupID, []string{memberID}, attestationACKEvent{
		Type:       "attestation-ACK",
		GroupID:    groupID,
		GroupState: state,
	})
	c.writeJSON(w, http.StatusOK, map[string]any{
		"groupState": state,
		"ourView":    ours,
	})
}

// attestationACKEvent acknowledges the attestation receipt and surfaces
// the post-aggregation groupState (mirroring the HTTP response). The Type
// discriminator places it on the same multi-event-type bus as
// sign-START / keygen-START / reshare-START.
type attestationACKEvent struct {
	Type       string `json:"type"`
	GroupID    string `json:"groupId"`
	GroupState string `json:"groupState"`
}

// computeGroupState derives the groupState from the attestation cache +
// the stored group record. See api.md B11 / design §3.ter.
func (c *Coord) computeGroupState(ctx context.Context, groupID string) (string, map[string]any) {
	views := c.attestations.snapshot(groupID)
	expected := c.expectedSet(groupID)
	ours := map[string]any{
		"nMembersReporting": len(views),
		"nHoldingShare":     countHolding(views),
	}

	storedPub, storedCC, storedExists := c.lookupGroupPubkeyChaincode(ctx, groupID)
	if storedExists {
		ours["ecdsaPubkey"] = storedPub
		ours["chaincode"] = storedCC
	}

	if len(expected) > 0 && len(views) < len(expected) {
		return groupStateUnattested, ours
	}
	if len(views) == 0 {
		return groupStateUnattested, ours
	}

	var (
		anyHolds   bool
		allHolds   = true
		firstPub   []byte
		firstCC    []byte
		mismatches bool
	)
	for _, v := range views {
		if v.HoldsShare {
			anyHolds = true
			if firstPub == nil {
				firstPub = v.GroupPubkey
				firstCC = v.Chaincode
			} else if !bytes.Equal(v.GroupPubkey, firstPub) || !bytes.Equal(v.Chaincode, firstCC) {
				mismatches = true
			}
		} else {
			allHolds = false
		}
	}
	if mismatches {
		return groupStateInconsistent, ours
	}
	if storedExists && firstPub != nil && !bytes.Equal(firstPub, storedPub) {
		return groupStateInconsistent, ours
	}

	switch {
	case !anyHolds && !storedExists:
		return groupStateNeedsKeygen, ours
	case allHolds && (!storedExists || bytes.Equal(firstPub, storedPub)):
		return groupStateRegistered, ours
	default:
		return groupStateNeedsReshare, ours
	}
}

func countHolding(views []*attestationView) int {
	n := 0
	for _, v := range views {
		if v.HoldsShare {
			n++
		}
	}
	return n
}

// lookupGroupPubkeyChaincode reads the stored (pubkey, chaincode) without
// failing the request when the group does not exist (NEEDS_KEYGEN path).
// The third return is true when a row was found.
func (c *Coord) lookupGroupPubkeyChaincode(ctx context.Context, groupID string) ([]byte, []byte, bool) {
	pub, cc, has, err := c.db.xpub(ctx, groupID)
	if err != nil {
		if errors.Is(err, errGroupNotFound) {
			return nil, nil, false
		}
		return nil, nil, false
	}
	if !has {
		return pub, nil, true
	}
	return pub, cc, true
}
