package coord

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"

	"golang.org/x/text/unicode/norm"
)

// S-002 group/membership provisioning payloads need a unique canonical byte
// form so a device signature and the coord verification are byte-identical
// (docs/spec/group-provisioning.md §2). This reuses the S-001 §2.3
// canonicalization *discipline* (fixed field order, uint32 big-endian length
// prefixes, NFC strings, fixed-width big-endian integers, a domain-separation
// prefix) — not a competing scheme. C-001 only canonicalizes SigningRequest,
// so the same rules are applied here for GroupProvisioning / MembershipUpdate
// with distinct domains that prevent cross-payload signature reuse. H(payload)
// is SHA-256 over the preimage (group-provisioning.md §2), already in the
// design primitive set.

var groupProvisionDomain = append([]byte("TSS-GROUP-PROVISIONING-CANONICAL-v1"), 0x00)
var membershipUpdateDomain = append([]byte("TSS-MEMBERSHIP-UPDATE-CANONICAL-v1"), 0x00)

// memberEntry is one {memberId, identityPubkey} pair in a provisioning payload.
type memberEntry struct {
	MemberID       string `json:"memberId"`
	IdentityPubkey []byte `json:"identityPubkey"`
}

// groupProvisioning is the POST /v1/groups payload (group-provisioning.md §3.1).
type groupProvisioning struct {
	Version      uint64        `json:"version"`
	GroupID      string        `json:"groupId"`
	ECDSAPubkey  []byte        `json:"ecdsaPubkey"`
	GroupPubkey  []byte        `json:"groupPubkey"`
	ThresholdT   int           `json:"thresholdT"`
	PartiesN     int           `json:"partiesN"`
	Members      []memberEntry `json:"members"`
	CreatedAt    string        `json:"createdAt"` // RFC3339
	GroupSig     []byte        `json:"groupSig"`
	MemberCoSigs []coSig       `json:"memberCoSigs"`
}

// membershipUpdate is the POST /v1/groups/{id}/membership payload (§4.1).
type membershipUpdate struct {
	Version           uint64        `json:"version"`
	GroupID           string        `json:"groupId"`
	Epoch             int64         `json:"epoch"`
	ECDSAPubkeyAssert []byte        `json:"ecdsaPubkeyAssert"`
	GroupPubkey       []byte        `json:"groupPubkey"`
	ThresholdT        int           `json:"thresholdT"`
	PartiesN          int           `json:"partiesN"`
	AddedMembers      []memberEntry `json:"addedMembers"`
	RemovedMemberIDs  []string      `json:"removedMemberIds"`
	UpdatedAt         string        `json:"updatedAt"` // RFC3339
	GroupSig          []byte        `json:"groupSig"`
	MemberCoSigs      []coSig       `json:"memberCoSigs"`
}

// coSig is one member co-signature over H(payload) (§6.2).
type coSig struct {
	MemberID string `json:"memberId"`
	Sig      []byte `json:"sig"`
}

func putU64(b []byte, v uint64) []byte {
	var t [8]byte
	binary.BigEndian.PutUint64(t[:], v)
	return append(b, t[:]...)
}

func putI64(b []byte, v int64) []byte { return putU64(b, uint64(v)) }

func putLP(b, v []byte) []byte {
	var lp [4]byte
	binary.BigEndian.PutUint32(lp[:], uint32(len(v)))
	b = append(b, lp[:]...)
	return append(b, v...)
}

func putStr(b []byte, s string) []byte { return putLP(b, norm.NFC.Bytes([]byte(s))) }

func rfc3339ToMS(s string) (int64, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return 0, fmt.Errorf("coord: bad RFC3339 time %q: %w", s, err)
	}
	return t.UTC().UnixMilli(), nil
}

// groupProvisionDigest returns SHA-256 over the canonical preimage of a
// GroupProvisioning payload, excluding groupSig and memberCoSigs (they sign
// this digest, §6).
func groupProvisionDigest(p *groupProvisioning) ([32]byte, error) {
	ms, err := rfc3339ToMS(p.CreatedAt)
	if err != nil {
		return [32]byte{}, err
	}
	var b []byte
	b = append(b, groupProvisionDomain...)
	b = putU64(b, p.Version)
	b = putStr(b, p.GroupID)
	b = putLP(b, p.ECDSAPubkey)
	b = putLP(b, p.GroupPubkey)
	b = putI64(b, int64(p.ThresholdT))
	b = putI64(b, int64(p.PartiesN))
	b = binary.BigEndian.AppendUint32(b, uint32(len(p.Members)))
	for _, m := range p.Members {
		b = putStr(b, m.MemberID)
		b = putLP(b, m.IdentityPubkey)
	}
	b = putI64(b, ms)
	return sha256.Sum256(b), nil
}

// membershipUpdateDigest returns SHA-256 over the canonical preimage of a
// MembershipUpdate payload, excluding groupSig and memberCoSigs.
func membershipUpdateDigest(p *membershipUpdate) ([32]byte, error) {
	ms, err := rfc3339ToMS(p.UpdatedAt)
	if err != nil {
		return [32]byte{}, err
	}
	var b []byte
	b = append(b, membershipUpdateDomain...)
	b = putU64(b, p.Version)
	b = putStr(b, p.GroupID)
	b = putI64(b, p.Epoch)
	b = putLP(b, p.ECDSAPubkeyAssert)
	b = putLP(b, p.GroupPubkey)
	b = putI64(b, int64(p.ThresholdT))
	b = putI64(b, int64(p.PartiesN))
	b = binary.BigEndian.AppendUint32(b, uint32(len(p.AddedMembers)))
	for _, m := range p.AddedMembers {
		b = putStr(b, m.MemberID)
		b = putLP(b, m.IdentityPubkey)
	}
	b = binary.BigEndian.AppendUint32(b, uint32(len(p.RemovedMemberIDs)))
	for _, id := range p.RemovedMemberIDs {
		b = putStr(b, id)
	}
	b = putI64(b, ms)
	return sha256.Sum256(b), nil
}
