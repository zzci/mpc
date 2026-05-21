package mobileapi

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"sync"

	"github.com/btcsuite/btcd/btcec/v2"

	"github.com/zzci/mpc/internal/keystore"
	"github.com/zzci/mpc/internal/mpc"

	tsskeygen "github.com/bnb-chain/tss-lib/v3/ecdsa/keygen"
)

// SDK is the device-side handle the RN host obtains once and drives the whole
// MPC lifecycle through. It is returned by pointer and never crosses the
// gomobile bridge by value: the host sees only an opaque reference plus the
// flat string/[]byte/callback methods on it, so no tss-lib type is ever
// exported (docs/design/mcp/sdk.md §2).
//
// Concurrency: every long-running operation (KeyGen/Sign/Reshare) runs on its
// own background goroutine (docs/design/mcp/sdk.md §5 — never the UI thread) and
// reports only through its callback. The mutex guards the in-memory share set,
// group metadata, the signing-session table and the active wire session.
//
// Single-party model (distributed-mpc-impl.md §B DM-3): each device holds
// only its own share_i; the in-memory `shares` map is sized for the small
// case (Export/Import surface keeps a moniker-keyed map for compatibility).
type SDK struct {
	store *keystore.Store

	mu sync.Mutex
	// shares is moniker → unsealed share. Under DM-3 each device holds at
	// most one share per group; the map is the union across all groups
	// the device has joined (user ruling 2026-05-18: "multi-group is how
	// we deliver multi-address; no BIP44 HD"). Keys are share monikers
	// (which by convention double as the keystore filename anchor); the
	// per-group bookkeeping lives in `groups` below and points at the
	// matching moniker.
	shares map[string]mpc.Share
	// groups is groupID → per-group bookkeeping. Each group that this
	// device has successfully completed keygen / reshare against owns
	// one entry. KeyGen/Sign/Reshare are routed by configJSON.GroupID
	// (DM-3 envelope) so multi-group operation does not require a
	// "switch active group" call.
	groups   map[string]*groupMeta
	sessions map[string]*SignSession // sessionId(=requestId) → active signing session
	active   *wireSession            // the currently running wire-bound MPC session

	// preParams is a test-only seam. On a real device it stays nil and mpc
	// generates safe primes locally per party (RED LINE: never server-pushed,
	// see mpc.KeygenConfig.PreParams). Tests inject tss-lib bundled fixtures so
	// the suite does not pay the multi-minute safe-prime search. It is
	// unexported, so it never reaches the gomobile-exported surface.
	//
	// Under DM-3 a single device drives a single party, so test fixtures
	// supply exactly ONE LocalPreParams record per SDK instance (the
	// committee is built by combining N independently-fixtured SDKs).
	preParams []tsskeygen.LocalPreParams
}

// groupMeta is the minimal in-process record of one wallet group needed to
// drive an in-process signing/reshare without re-reading the keystore.
// Multi-group support: every entry holds the moniker of the share belonging
// to this group; the shares map is dereferenced through it.
type groupMeta struct {
	threshold   int
	parties     int
	partyIndex  int    // this device's 0-based index in the committee (DM-3)
	ecdsaPubHex string // compressed secp256k1 group master public key
	moniker     string // keystore moniker of this group's share (key into s.shares)
}

// NewSDK opens (creating if absent) the device keystore rooted at
// keystoreDir, sealed with a software secure-area factor that stands in for
// the iOS Keychain / Android Keystore in this environment
// (docs/design/mcp/sdk.md §6). The returned handle is the single entry point the
// RN host keeps for the wallet's lifetime.
func NewSDK(keystoreDir string) (*SDK, error) {
	area, err := keystore.NewSoftSecureArea()
	if err != nil {
		return nil, fmt.Errorf("mobileapi: secure area: %w", err)
	}
	store, err := keystore.NewStore(keystoreDir, area)
	if err != nil {
		return nil, fmt.Errorf("mobileapi: open keystore: %w", err)
	}
	return &SDK{
		store:    store,
		shares:   map[string]mpc.Share{},
		groups:   map[string]*groupMeta{},
		sessions: map[string]*SignSession{},
	}, nil
}

// setOwnShare records the keygen/reshare outcome for one group on this
// device. Pre-multi-group code called it once with a clobbering map; with
// multi-group it appends — every previously-joined group's share survives
// the call. DM-3 invariant: one share per group, but a device may join many
// groups (user ruling 2026-05-18: multi-group is how we deliver multi-address;
// no BIP44 HD). Concurrency safety: callers hold no SDK locks — this
// mutates under s.mu (the only secret material the device holds).
func (s *SDK) setOwnShare(groupID string, share mpc.Share, threshold, parties, partyIndex int, pubHex string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shares[share.Moniker] = share
	s.groups[groupID] = &groupMeta{
		threshold:   threshold,
		parties:     parties,
		partyIndex:  partyIndex,
		ecdsaPubHex: pubHex,
		moniker:     share.Moniker,
	}
}

// snapshotShareForGroup returns this device's share + threshold/parties/
// partyIndex for one specific group. Sign / Reshare call it with the
// groupID pulled from configJSON (DM-3 envelope). ok=false means no share
// is held for that group — caller surfaces CodeNoShares.
func (s *SDK) snapshotShareForGroup(groupID string) (share mpc.Share, threshold, parties, partyIndex int, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	gm, exists := s.groups[groupID]
	if !exists {
		return mpc.Share{}, 0, 0, 0, false
	}
	sh, has := s.shares[gm.moniker]
	if !has {
		return mpc.Share{}, 0, 0, 0, false
	}
	return sh, gm.threshold, gm.parties, gm.partyIndex, true
}

// snapshotOwnShare is the pre-multi-group helper kept for tests and
// callers that only ever live in a single-group device. It returns the
// share + meta only when exactly one group is held; with zero or many,
// ok=false so the caller must use snapshotShareForGroup(groupID).
func (s *SDK) snapshotOwnShare() (share mpc.Share, threshold, parties, partyIndex int, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.groups) != 1 {
		return mpc.Share{}, 0, 0, 0, false
	}
	for _, gm := range s.groups {
		if sh, has := s.shares[gm.moniker]; has {
			return sh, gm.threshold, gm.parties, gm.partyIndex, true
		}
	}
	return mpc.Share{}, 0, 0, 0, false
}

// groupSummary is the per-row internal projection used to build the
// ListGroupsJSON response. The exported surface keeps gomobile-flat: a
// single JSON string out, no Go struct slices.
type groupSummary struct {
	GroupID     string `json:"groupId"`
	Threshold   int    `json:"threshold"`
	Parties     int    `json:"parties"`
	PartyIndex  int    `json:"partyIndex"`
	ECDSAPubHex string `json:"ecdsaPubHex"`
	Moniker     string `json:"moniker"`
}

// ListGroupsJSON returns a JSON document `{"items":[{...},...]}` listing
// the groups this device has joined. Share material is NOT included — only
// the metadata an operator needs to recognise / route requests against
// each group. Order is unspecified. The gomobile-flat contract is kept:
// one string out, one (nil) error.
func (s *SDK) ListGroupsJSON() (string, error) {
	s.mu.Lock()
	rows := make([]groupSummary, 0, len(s.groups))
	for gid, gm := range s.groups {
		rows = append(rows, groupSummary{
			GroupID:     gid,
			Threshold:   gm.threshold,
			Parties:     gm.parties,
			PartyIndex:  gm.partyIndex,
			ECDSAPubHex: gm.ecdsaPubHex,
			Moniker:     gm.moniker,
		})
	}
	s.mu.Unlock()
	out, err := json.Marshal(map[string]any{"items": rows})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (s *SDK) registerSession(id string, ss *SignSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = ss
}

func (s *SDK) unregisterSession(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func (s *SDK) lookupSession(id string) (*SignSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ss, ok := s.sessions[id]
	return ss, ok
}

// groupPubHex derives the compressed secp256k1 master public key from a share.
// The key is identical across every share of a group (and invariant across
// resharing, docs/design/mcp/sdk.md §7), so share[0] is authoritative.
func groupPubHex(sh mpc.Share) (string, error) {
	sd, err := mpc.UnmarshalSaveData(sh.SaveData)
	if err != nil {
		return "", fmt.Errorf("mobileapi: decode save data: %w", err)
	}
	if sd.ECDSAPub == nil {
		return "", fmt.Errorf("mobileapi: share carries no public key")
	}
	var x, y btcec.FieldVal
	if overflow := x.SetByteSlice(padScalar(sd.ECDSAPub.X())); overflow {
		return "", fmt.Errorf("mobileapi: public key X out of range")
	}
	if overflow := y.SetByteSlice(padScalar(sd.ECDSAPub.Y())); overflow {
		return "", fmt.Errorf("mobileapi: public key Y out of range")
	}
	return hex.EncodeToString(btcec.NewPublicKey(&x, &y).SerializeCompressed()), nil
}

// padScalar left-pads a coordinate to 32 bytes, the fixed width btcec's
// FieldVal.SetByteSlice expects for a secp256k1 field element.
func padScalar(v *big.Int) []byte {
	b := v.Bytes()
	if len(b) >= 32 {
		return b
	}
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}
