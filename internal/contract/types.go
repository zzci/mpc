package contract

// This file defines the authoritative wire/logic types of
// docs/design/contract/protocol.md §1/§3/§5/§6. Per docs/spec/envelope-canonical.md
// (S-001) signatures and hashes are NOT taken over any wire serialization of
// these structs: each party decodes its wire form (JSON api.md / protobuf
// START) into these logical values, then derives canonical bytes from the
// values (see canonical.go). JSON tags follow docs/design/contract/api.md A2 with
// the S-001 §5 closure (version/createdAt required, time as int64 unix ms).

// CapScope is the capability a CapToken grants at the relay
// (docs/design/contract/protocol.md:76).
type CapScope string

const (
	// ScopeRelayReserve authorizes a circuit-relay v2 reservation.
	ScopeRelayReserve CapScope = "relay-reserve"
	// ScopeRendezvousRegister authorizes a rendezvous namespace registration.
	ScopeRendezvousRegister CapScope = "rendezvous-register"
)

// BusinessInfo is the optional structured human-review payload
// (docs/design/contract/api.md:17, docs/design/server/server.md:188-190). Its canonical
// byte form for metaHash is RFC 8785 JCS over this object
// (docs/spec/envelope-canonical.md §4.1); field order and whitespace here are
// irrelevant because JCS re-canonicalizes. Absent BusinessInfo (nil) hashes to
// EmptyMetaHash, never to an empty JSON object.
type BusinessInfo struct {
	Title        string            `json:"title,omitempty"`
	Summary      string            `json:"summary,omitempty"`
	Items        []string          `json:"items,omitempty"`
	Refs         map[string]string `json:"refs,omitempty"`
	Requester    string            `json:"requester,omitempty"`
	Memo         string            `json:"memo,omitempty"`
	DisplayHints map[string]string `json:"displayHints,omitempty"`
}

// SigningRequest is the authoritative signing envelope
// (docs/design/contract/protocol.md:10-23). MetaHash MUST equal
// MetaHash(BusinessInfo) and ProposerSig MUST cover the canonical
// preimage of all other fields (see canonical.go, docs/spec/envelope-canonical.md
// §2/§3). The device verifies proposerSig, metaHash, expiry and tx-decode
// before entering MPC (docs/design/contract/protocol.md:25).
type SigningRequest struct {
	Version      uint64        `json:"version"`
	RequestID    string        `json:"requestId"`
	GroupID      string        `json:"groupId"`
	Chain        string        `json:"chain"`
	UnsignedTx   []byte        `json:"unsignedTx"`
	Digest32     []byte        `json:"digest32"`
	Proposer     string        `json:"proposer"`
	CreatedAt    int64         `json:"createdAt"` // unix ms (S-001 §5)
	Expiry       int64         `json:"expiry"`    // unix ms, absolute
	BusinessInfo *BusinessInfo `json:"businessInfo,omitempty"`
	MetaHash     []byte        `json:"metaHash"`
	ProposerSig  []byte        `json:"proposerSig"`
}

// MpcMessage wraps a tss-lib WireBytes payload
// (docs/design/contract/protocol.md:45-55). SessionID strongly isolates sessions:
// cross-session messages are dropped (see session.go). SenderAuth binds the
// sender's member identity to (SessionID, Round, Payload) above the tss-lib
// layer even though Noise already authenticates the peer.
type MpcMessage struct {
	Version     uint64   `json:"version"`
	SessionID   string   `json:"sessionId"`
	From        string   `json:"from"`         // tss PartyID
	To          []string `json:"to,omitempty"` // empty == broadcast
	IsBroadcast bool     `json:"isBroadcast"`
	Round       uint32   `json:"round"`
	Payload     []byte   `json:"payload"` // tss WireBytes
	SenderAuth  []byte   `json:"senderAuth"`
}

// CapToken is the relay access-control capability token
// (docs/design/contract/protocol.md:76-77), issued by the wallet group key
// (self-sovereign trust anchor); short TTL, per-group, revocable via TTL.
type CapToken struct {
	GroupID   string   `json:"groupId"`
	MemberID  string   `json:"memberId"`
	Scope     CapScope `json:"scope"`
	NotBefore int64    `json:"notBefore"` // unix ms
	NotAfter  int64    `json:"notAfter"`  // unix ms
	Nonce     []byte   `json:"nonce"`
	GroupSig  []byte   `json:"groupSig"` // group-key signature over the preimage
}

// StartSigning is the coord→device START message
// (docs/design/contract/protocol.md:65-72). Envelope is the full SigningRequest the
// device re-verifies per protocol.md:25 before running /tss/mpc with the
// other signers; coord is never in Signers and never on /tss/mpc.
type StartSigning struct {
	RequestID  string         `json:"requestId"`
	Envelope   SigningRequest `json:"envelope"`
	Signers    []string       `json:"signers"`    // memberIds
	SelfRole   bool           `json:"selfRole"`   // is this device a signer
	RelayHints []string       `json:"relayHints"` // multiaddrs
	Deadline   int64          `json:"deadline"`   // unix ms
}
