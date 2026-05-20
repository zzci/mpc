package mobileapi

import (
	"encoding/hex"
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

	mu       sync.Mutex
	shares   map[string]mpc.Share    // moniker → unsealed share (typically one entry under DM-3)
	group    *groupMeta              // set after a successful KeyGen/Reshare
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

// groupMeta is the minimal in-process record of the active wallet group needed
// to drive an in-process signing/reshare without re-reading the keystore.
type groupMeta struct {
	threshold   int
	parties     int
	partyIndex  int    // this device's 0-based index in the committee (DM-3)
	ecdsaPubHex string // compressed secp256k1 group master public key
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
		sessions: map[string]*SignSession{},
	}, nil
}

// setOwnShare records the keygen/reshare outcome for this device: it replaces
// the in-memory share entry plus the group metadata atomically so a concurrent
// Sign sees a consistent committee. share is THIS device's share_i (the only
// secret material the device holds; distributed-mpc-impl.md §B DM-3).
func (s *SDK) setOwnShare(share mpc.Share, threshold, parties, partyIndex int, pubHex string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shares = map[string]mpc.Share{share.Moniker: share}
	s.group = &groupMeta{
		threshold:   threshold,
		parties:     parties,
		partyIndex:  partyIndex,
		ecdsaPubHex: pubHex,
	}
}

// snapshotOwnShare returns a copy of this device's share, plus the group
// (threshold, parties, partyIndex). ok is false when this process holds no
// share.
func (s *SDK) snapshotOwnShare() (share mpc.Share, threshold, parties, partyIndex int, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.group == nil || len(s.shares) == 0 {
		return mpc.Share{}, 0, 0, 0, false
	}
	for _, sh := range s.shares {
		// Single-party invariant: exactly one entry under DM-3. We pick the
		// first; ExportShare/ImportShare keep the map shape so an explicit
		// moniker-by-moniker accessor stays available on the export surface.
		return sh, s.group.threshold, s.group.parties, s.group.partyIndex, true
	}
	return mpc.Share{}, 0, 0, 0, false
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
