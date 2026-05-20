package mpc

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/bnb-chain/tss-lib/v3/common"
	"github.com/bnb-chain/tss-lib/v3/crypto"
	"github.com/bnb-chain/tss-lib/v3/ecdsa/keygen"
	"github.com/bnb-chain/tss-lib/v3/ecdsa/resharing"
	"github.com/bnb-chain/tss-lib/v3/ecdsa/signing"
	"github.com/bnb-chain/tss-lib/v3/tss"
)

// Single-party MPC entry (distributed-mpc.md §6.1 / distributed-mpc-impl.md
// §B DM-1): each device drives exactly one keygen / signing / resharing party
// against a caller-supplied transport, holding only its own share_i. The
// existing in-process all-n APIs (Keygen / Sign / Reshare) are unchanged and
// remain available as the test simulator the DM-* layers above this one will
// validate against.
//
// Party identity is derived deterministically from public inputs, matching
// internal/cli/ids.go so DM-2 (network engine extraction) and DM-5 (host
// transport) layer on without re-deriving the convention:
//   - keygen committee:   PartyID(tag=i+1, key=i+1) for i in [0, n)
//   - signing committee:  rebuilt from each save data's Ks vector (the share's
//                         own ShareID is publicly carried alongside it)
//   - reshare old committee: rebuilt from the save data's Ks vector
//   - reshare new committee: PartyID(tag=i+1, key=n+i+1) so old/new keys never
//                            collide on the wire

// partyTagFor returns the canonical PartyID.Id for a 0-based party index:
// the 1-based index as decimal text. Matches internal/cli/ids.go.
func partyTagFor(index int) string { return fmt.Sprintf("%d", index+1) }

func deriveKeygenParties(n int) tss.SortedPartyIDs {
	un := make(tss.UnSortedPartyIDs, n)
	for i := 0; i < n; i++ {
		tag := partyTagFor(i)
		un[i] = tss.NewPartyID(tag, tag, big.NewInt(int64(i+1)))
	}
	return tss.SortPartyIDs(un)
}

func deriveSigningParties(sd *keygen.LocalPartySaveData, participants []int) (tss.SortedPartyIDs, error) {
	if len(sd.Ks) == 0 {
		return nil, fmt.Errorf("save data has no Ks vector")
	}
	idx := append([]int(nil), participants...)
	sort.Ints(idx)
	for i := 1; i < len(idx); i++ {
		if idx[i] == idx[i-1] {
			return nil, fmt.Errorf("participants list has duplicate index %d", idx[i])
		}
	}
	un := make(tss.UnSortedPartyIDs, 0, len(idx))
	for _, j := range idx {
		if j < 0 || j >= len(sd.Ks) {
			return nil, fmt.Errorf("signer index %d out of range (Ks=%d)", j, len(sd.Ks))
		}
		tag := partyTagFor(j)
		un = append(un, tss.NewPartyID(tag, tag, sd.Ks[j]))
	}
	return tss.SortPartyIDs(un), nil
}

func deriveOldReshareParties(sd *keygen.LocalPartySaveData) (tss.SortedPartyIDs, error) {
	if len(sd.Ks) == 0 {
		return nil, fmt.Errorf("save data has no Ks vector")
	}
	un := make(tss.UnSortedPartyIDs, len(sd.Ks))
	for i := range sd.Ks {
		tag := partyTagFor(i)
		un[i] = tss.NewPartyID(tag, tag, sd.Ks[i])
	}
	return tss.SortPartyIDs(un), nil
}

func deriveNewReshareParties(n int) tss.SortedPartyIDs {
	un := make(tss.UnSortedPartyIDs, n)
	for i := 0; i < n; i++ {
		tag := partyTagFor(i)
		un[i] = tss.NewPartyID(tag, tag, big.NewInt(int64(i+1+n)))
	}
	return tss.SortPartyIDs(un)
}

func resolveOwnPreParams(ctx context.Context, pre *keygen.LocalPreParams, timeout time.Duration) (*keygen.LocalPreParams, error) {
	if pre != nil {
		return pre, nil
	}
	if timeout <= 0 {
		timeout = defaultPreParamTimeout
	}
	genCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	p, err := keygen.GeneratePreParamsWithContext(genCtx)
	if err != nil {
		return nil, fmt.Errorf("generate pre-params: %w", err)
	}
	return p, nil
}

// --- Single-party keygen ---

// SinglePartyKeygenConfig parameterizes one device's view of a distributed
// n-party ECDSA threshold keygen. The party set is derived deterministically
// from (Parties, PartyIndex) so every device, constructed independently,
// agrees on the committee without exchanging identities first.
type SinglePartyKeygenConfig struct {
	// Threshold is t in t-of-n.
	Threshold int
	// Parties is n.
	Parties int
	// PartyIndex is this device's 0-based party index in the committee.
	PartyIndex int
	// PreParams optionally supplies this party's pre-computed Paillier /
	// safe-prime parameters. When nil, generated locally on this device.
	// Custody invariant: must be local to this device (never server-supplied);
	// see KeygenConfig.PreParams.
	PreParams *keygen.LocalPreParams
	// PreParamTimeout bounds local generation when PreParams is nil.
	PreParamTimeout time.Duration
}

// KeygenParty drives one party's view of an n-party distributed keygen
// against a caller-supplied transport: outbound tss messages are emitted on
// Out(); inbound messages are fed via Update(); Done() blocks until this
// party's share is ready.
type KeygenParty struct {
	party   tss.Party
	self    *tss.PartyID
	pids    tss.SortedPartyIDs
	outCh   chan tss.Message
	endCh   chan *keygen.LocalPartySaveData
	errCh   chan *tss.Error
	started bool
}

// NewKeygenParty constructs one party's keygen session. Paillier
// modulus/factor ZK proofs run by default — security.md invariant #10 keeps
// the no-proof fast path test-only (the package-internal newKeygenPartyFast
// helper, reachable from in-package tests via SimulateSinglePartyKeygen).
func NewKeygenParty(ctx context.Context, cfg SinglePartyKeygenConfig) (*KeygenParty, error) {
	return newKeygenPartyInternal(ctx, cfg, false)
}

func newKeygenPartyInternal(ctx context.Context, cfg SinglePartyKeygenConfig, fast bool) (*KeygenParty, error) {
	if cfg.Parties < 2 {
		return nil, fmt.Errorf("keygen needs at least 2 parties, got %d", cfg.Parties)
	}
	if cfg.Threshold < 1 || cfg.Threshold >= cfg.Parties {
		return nil, fmt.Errorf("threshold must satisfy 1 <= t < n, got t=%d n=%d", cfg.Threshold, cfg.Parties)
	}
	if cfg.PartyIndex < 0 || cfg.PartyIndex >= cfg.Parties {
		return nil, fmt.Errorf("party index %d out of range [0, %d)", cfg.PartyIndex, cfg.Parties)
	}
	pids := deriveKeygenParties(cfg.Parties)
	// pids are key-sorted; since keys are 1..n the slice index matches the
	// PartyIndex the caller supplied.
	self := pids[cfg.PartyIndex]

	params := tss.NewParameters(tss.S256(), tss.NewPeerContext(pids), self, cfg.Parties, cfg.Threshold)
	if fast {
		params.SetNoProofMod()
		params.SetNoProofFac()
	}

	pre, err := resolveOwnPreParams(ctx, cfg.PreParams, cfg.PreParamTimeout)
	if err != nil {
		return nil, err
	}

	// Generous channel buffers: tss-lib emits several rounds back-to-back
	// before yielding, and a blocked party stalls every peer.
	outCh := make(chan tss.Message, 4*cfg.Parties)
	endCh := make(chan *keygen.LocalPartySaveData, 1)
	party := keygen.NewLocalParty(params, outCh, endCh, *pre)

	return &KeygenParty{
		party: party,
		self:  self,
		pids:  pids,
		outCh: outCh,
		endCh: endCh,
		errCh: make(chan *tss.Error, 1),
	}, nil
}

// PartyID returns this party's tss identity, the canonical wire id callers
// route inbound/outbound messages against.
func (p *KeygenParty) PartyID() *tss.PartyID { return p.self }

// Peers returns the deterministic sorted committee, including this party.
func (p *KeygenParty) Peers() tss.SortedPartyIDs { return p.pids }

// Out is the outbound stream of tss messages this party emits. The caller
// owns transport: it serializes via msg.WireBytes() and routes per
// msg.GetTo() (point-to-point) or broadcast when msg.GetTo() == nil.
func (p *KeygenParty) Out() <-chan tss.Message { return p.outCh }

// Start launches the keygen protocol for this party in a background
// goroutine. Idempotency is not part of the contract — calling Start twice
// returns an error.
func (p *KeygenParty) Start() error {
	if p.started {
		return fmt.Errorf("keygen party already started")
	}
	p.started = true
	go func() {
		if err := p.party.Start(); err != nil {
			p.errCh <- err
		}
	}()
	return nil
}

// Update feeds one peer-sourced parsed tss message into this party. A message
// whose sender matches this party is silently dropped (defence in depth — a
// transport loopback must never trip the protocol).
func (p *KeygenParty) Update(msg tss.ParsedMessage) error {
	if msg.GetFrom().Id == p.self.Id {
		return nil
	}
	if _, err := p.party.Update(msg); err != nil {
		return fmt.Errorf("apply inbound: %w", err)
	}
	return nil
}

// Done blocks until this party finishes or ctx is cancelled; it returns the
// serialized share on success.
func (p *KeygenParty) Done(ctx context.Context) (Share, error) {
	select {
	case <-ctx.Done():
		return Share{}, fmt.Errorf("keygen cancelled: %w", ctx.Err())
	case err := <-p.errCh:
		return Share{}, fmt.Errorf("tss keygen error: %w", err)
	case sd := <-p.endCh:
		bz, mErr := MarshalSaveData(sd)
		if mErr != nil {
			return Share{}, fmt.Errorf("serialize share: %w", mErr)
		}
		return Share{Moniker: p.self.Moniker, SaveData: bz}, nil
	}
}

// --- Single-party signing ---

// SinglePartySignConfig parameterizes one signer's view of a threshold ECDSA
// signing session.
type SinglePartySignConfig struct {
	// SessionID strongly isolates this session (= requestId; sdk.md §5,
	// protocol.md §3). Must be non-empty.
	SessionID string
	// Threshold is t in t-of-n the shares were generated for.
	Threshold int
	// PartyIndex is this device's 0-based index in the original n-party
	// committee that produced the share (matches Share's keygen position).
	PartyIndex int
	// Participants are the 0-based indices of every device that participates
	// in this session. Must contain PartyIndex and have at least Threshold+1
	// distinct entries.
	Participants []int
	// Share is this device's keygen share (its own share_i, the only secret
	// material the device holds).
	Share Share
	// Digest is the 32-byte message digest to sign.
	Digest []byte
	// ChildPub / KeyDerivationDelta opt this session into HD-child signing
	// (address-derivation.md §6, KDD path). Both nil = master-key signing;
	// half-set is rejected as a caller bug. See SignConfig for full notes.
	ChildPub           *ecdsa.PublicKey
	KeyDerivationDelta *big.Int
}

// SignParty drives one signer's view of a threshold ECDSA signing session.
type SignParty struct {
	party   tss.Party
	self    *tss.PartyID
	pids    tss.SortedPartyIDs
	outCh   chan tss.Message
	endCh   chan *common.SignatureData
	errCh   chan *tss.Error
	started bool
}

// NewSignParty constructs one signer's session.
func NewSignParty(_ context.Context, cfg SinglePartySignConfig) (*SignParty, error) {
	if cfg.SessionID == "" {
		return nil, fmt.Errorf("signing requires a non-empty SessionID")
	}
	if len(cfg.Digest) != digestLen {
		return nil, fmt.Errorf("digest must be %d bytes, got %d", digestLen, len(cfg.Digest))
	}
	if cfg.Threshold < 1 {
		return nil, fmt.Errorf("threshold must be >= 1, got %d", cfg.Threshold)
	}
	if (cfg.ChildPub == nil) != (cfg.KeyDerivationDelta == nil) {
		return nil, fmt.Errorf("signing: ChildPub and KeyDerivationDelta must both be set or both nil")
	}
	if len(cfg.Participants) < cfg.Threshold+1 {
		return nil, fmt.Errorf("signing needs at least t+1=%d participants, got %d", cfg.Threshold+1, len(cfg.Participants))
	}
	inSet := false
	for _, p := range cfg.Participants {
		if p == cfg.PartyIndex {
			inSet = true
			break
		}
	}
	if !inSet {
		return nil, fmt.Errorf("PartyIndex %d not in Participants", cfg.PartyIndex)
	}

	sd, err := UnmarshalSaveData(cfg.Share.SaveData)
	if err != nil {
		return nil, fmt.Errorf("share: %w", err)
	}
	// HD-child wiring (address-derivation.md §6): adjust this signer's
	// in-memory Xi by IL·G so the local party signs against Q_child. The
	// mutation is on a fresh copy of the unmarshalled save data — the caller's
	// persisted Share blob is never touched.
	if cfg.KeyDerivationDelta != nil {
		keys := []keygen.LocalPartySaveData{*sd}
		if uerr := signing.UpdatePublicKeyAndAdjustBigXj(cfg.KeyDerivationDelta, keys, cfg.ChildPub, tss.S256()); uerr != nil {
			return nil, fmt.Errorf("apply key derivation delta: %w", uerr)
		}
		sd = &keys[0]
	}

	pids, err := deriveSigningParties(sd, cfg.Participants)
	if err != nil {
		return nil, err
	}
	selfTag := partyTagFor(cfg.PartyIndex)
	var self *tss.PartyID
	for _, p := range pids {
		if p.Id == selfTag {
			self = p
			break
		}
	}
	if self == nil {
		return nil, fmt.Errorf("internal: self %s not found in derived signer set", selfTag)
	}

	params := tss.NewParameters(tss.S256(), tss.NewPeerContext(pids), self, len(pids), cfg.Threshold)
	outCh := make(chan tss.Message, 4*len(pids))
	endCh := make(chan *common.SignatureData, 1)
	// NewLocalPartyWithKDD with a nil delta is exactly NewLocalParty.
	party := signing.NewLocalPartyWithKDD(new(big.Int).SetBytes(cfg.Digest), params, *sd, cfg.KeyDerivationDelta, outCh, endCh, digestLen)

	return &SignParty{
		party: party, self: self, pids: pids,
		outCh: outCh, endCh: endCh,
		errCh: make(chan *tss.Error, 1),
	}, nil
}

// PartyID returns this signer's tss identity.
func (p *SignParty) PartyID() *tss.PartyID { return p.self }

// Peers returns the deterministic sorted signer committee, including self.
func (p *SignParty) Peers() tss.SortedPartyIDs { return p.pids }

// Out is the outbound stream of tss signing messages this signer emits.
func (p *SignParty) Out() <-chan tss.Message { return p.outCh }

// Start launches the signing protocol for this signer.
func (p *SignParty) Start() error {
	if p.started {
		return fmt.Errorf("sign party already started")
	}
	p.started = true
	go func() {
		if err := p.party.Start(); err != nil {
			p.errCh <- err
		}
	}()
	return nil
}

// Update feeds one peer-sourced parsed tss message into this signer.
func (p *SignParty) Update(msg tss.ParsedMessage) error {
	if msg.GetFrom().Id == p.self.Id {
		return nil
	}
	if _, err := p.party.Update(msg); err != nil {
		return fmt.Errorf("apply inbound: %w", err)
	}
	return nil
}

// Done blocks until this signer produces the joint {R,S,V} signature.
func (p *SignParty) Done(ctx context.Context) (Signature, error) {
	select {
	case <-ctx.Done():
		return Signature{}, fmt.Errorf("signing cancelled: %w", ctx.Err())
	case err := <-p.errCh:
		return Signature{}, fmt.Errorf("tss signing error: %w", err)
	case sd := <-p.endCh:
		if len(sd.SignatureRecovery) == 0 {
			return Signature{}, fmt.Errorf("signing produced no recovery id")
		}
		var sig Signature
		copy(sig.R[:], sd.R)
		copy(sig.S[:], sd.S)
		sig.V = sd.SignatureRecovery[0]
		return sig, nil
	}
}

// --- Single-party resharing ---

// SinglePartyReshareConfig parameterizes one device's view of an in-place
// reshare: this device participates in BOTH the old and new committees at
// the same 0-based index, with the same committee size (n = n'). That matches
// distributed-mpc-impl.md §G's required reshare-3to3-rotate; expansion
// (3→4) is not yet exposed at this layer (Phase 2 §G optional case).
type SinglePartyReshareConfig struct {
	// OldThreshold is t the OldShare was created under.
	OldThreshold int
	// NewThreshold is t' the regenerated committee will use.
	NewThreshold int
	// Parties is n = n', the size of both the old and new committees.
	Parties int
	// PartyIndex is this device's 0-based index in both committees.
	PartyIndex int
	// OldShare is this device's existing keygen share (its own share_i),
	// consumed by the old-committee party.
	OldShare Share
	// PreParams optionally supplies this device's pre-computed parameters
	// for its NEW-committee party. When nil, generated locally.
	PreParams *keygen.LocalPreParams
	// PreParamTimeout bounds local generation when PreParams is nil.
	PreParamTimeout time.Duration
}

// ReshareParty drives one device's view of an in-place reshare. The device
// runs an old-committee party (using OldShare) and a new-committee party
// (which receives the refreshed share_i) concurrently against the same
// caller-supplied transport.
type ReshareParty struct {
	oldParty tss.Party
	newParty tss.Party
	oldSelf  *tss.PartyID
	newSelf  *tss.PartyID
	oldPIDs  tss.SortedPartyIDs
	newPIDs  tss.SortedPartyIDs
	wantPub  *crypto.ECPoint
	outCh    chan tss.Message
	endCh    chan *keygen.LocalPartySaveData
	errCh    chan *tss.Error
	started  bool
}

// NewReshareParty constructs one device's rotate-mode reshare session.
func NewReshareParty(ctx context.Context, cfg SinglePartyReshareConfig) (*ReshareParty, error) {
	return newReshareInternal(ctx, cfg, false)
}

func newReshareInternal(ctx context.Context, cfg SinglePartyReshareConfig, fast bool) (*ReshareParty, error) {
	if cfg.Parties < 2 {
		return nil, fmt.Errorf("resharing needs at least 2 parties, got %d", cfg.Parties)
	}
	if cfg.OldThreshold < 1 || cfg.OldThreshold >= cfg.Parties {
		return nil, fmt.Errorf("old threshold must satisfy 1 <= t < n, got t=%d n=%d", cfg.OldThreshold, cfg.Parties)
	}
	if cfg.NewThreshold < 1 || cfg.NewThreshold >= cfg.Parties {
		return nil, fmt.Errorf("new threshold must satisfy 1 <= t' < n', got t'=%d n'=%d", cfg.NewThreshold, cfg.Parties)
	}
	if cfg.PartyIndex < 0 || cfg.PartyIndex >= cfg.Parties {
		return nil, fmt.Errorf("party index %d out of range [0, %d)", cfg.PartyIndex, cfg.Parties)
	}

	oldSD, err := UnmarshalSaveData(cfg.OldShare.SaveData)
	if err != nil {
		return nil, fmt.Errorf("old share: %w", err)
	}
	if oldSD.ECDSAPub == nil {
		return nil, fmt.Errorf("old share has no ECDSAPub")
	}
	oldPIDs, err := deriveOldReshareParties(oldSD)
	if err != nil {
		return nil, err
	}
	if len(oldPIDs) != cfg.Parties {
		return nil, fmt.Errorf("old share Ks length %d disagrees with Parties %d", len(oldPIDs), cfg.Parties)
	}
	newPIDs := deriveNewReshareParties(cfg.Parties)

	// The 0-based PartyIndex must agree with the share's own ShareID so the
	// device contributes its own secret to the right old-committee slot.
	if oldPIDs[cfg.PartyIndex].KeyInt().Cmp(oldSD.ShareID) != 0 {
		return nil, fmt.Errorf("party index %d does not match old share's ShareID", cfg.PartyIndex)
	}

	oldCtx := tss.NewPeerContext(oldPIDs)
	newCtx := tss.NewPeerContext(newPIDs)
	oldSelf := oldPIDs[cfg.PartyIndex]
	newSelf := newPIDs[cfg.PartyIndex]

	outCh := make(chan tss.Message, 8*cfg.Parties)
	endCh := make(chan *keygen.LocalPartySaveData, 2)

	oldParams := tss.NewReSharingParameters(tss.S256(), oldCtx, newCtx, oldSelf, cfg.Parties, cfg.OldThreshold, cfg.Parties, cfg.NewThreshold)
	oldParty := resharing.NewLocalParty(oldParams, *oldSD, outCh, endCh)

	newParams := tss.NewReSharingParameters(tss.S256(), oldCtx, newCtx, newSelf, cfg.Parties, cfg.OldThreshold, cfg.Parties, cfg.NewThreshold)
	if fast {
		newParams.SetNoProofMod()
		newParams.SetNoProofFac()
	}
	newSave := keygen.NewLocalPartySaveData(cfg.Parties)
	pre, err := resolveOwnPreParams(ctx, cfg.PreParams, cfg.PreParamTimeout)
	if err != nil {
		return nil, err
	}
	newSave.LocalPreParams = *pre
	newParty := resharing.NewLocalParty(newParams, newSave, outCh, endCh)

	return &ReshareParty{
		oldParty: oldParty,
		newParty: newParty,
		oldSelf:  oldSelf,
		newSelf:  newSelf,
		oldPIDs:  oldPIDs,
		newPIDs:  newPIDs,
		wantPub:  oldSD.ECDSAPub,
		outCh:    outCh,
		endCh:    endCh,
		errCh:    make(chan *tss.Error, 2),
	}, nil
}

// OldPartyID is this device's identity in the old committee context.
func (p *ReshareParty) OldPartyID() *tss.PartyID { return p.oldSelf }

// NewPartyID is this device's identity in the new committee context.
func (p *ReshareParty) NewPartyID() *tss.PartyID { return p.newSelf }

// OldPeers is the deterministic old committee.
func (p *ReshareParty) OldPeers() tss.SortedPartyIDs { return p.oldPIDs }

// NewPeers is the deterministic new committee.
func (p *ReshareParty) NewPeers() tss.SortedPartyIDs { return p.newPIDs }

// Out is the outbound stream of tss messages from BOTH local committee parties.
// The caller inspects msg.GetFrom() to know which committee the sender belongs
// to, then routes per msg.IsToOldCommittee() / msg.IsToOldAndNewCommittees().
// Same-device cross-committee messages (this device's own old -> new) appear
// here too — the caller delivers them back via UpdateNew / UpdateOld in
// process (libp2p cannot dial self).
func (p *ReshareParty) Out() <-chan tss.Message { return p.outCh }

// Start launches both the old- and new-committee parties.
func (p *ReshareParty) Start() error {
	if p.started {
		return fmt.Errorf("reshare party already started")
	}
	p.started = true
	// Start the new-committee party first so it's waiting before the old
	// committee begins sending, matching the upstream resharing reference.
	go func() {
		if err := p.newParty.Start(); err != nil {
			p.errCh <- err
		}
	}()
	go func() {
		if err := p.oldParty.Start(); err != nil {
			p.errCh <- err
		}
	}()
	return nil
}

// UpdateOld feeds one parsed message into this device's old-committee party.
// The caller is responsible for parsing the wire bytes against the SENDER's
// old-committee PartyID (tss.ParseWireMessage strips committee routing flags;
// the transport carries an out-of-band committee marker — see DM-2).
func (p *ReshareParty) UpdateOld(msg tss.ParsedMessage) error {
	if samePID(msg.GetFrom(), p.oldSelf) {
		return nil
	}
	if _, err := p.oldParty.Update(msg); err != nil {
		return fmt.Errorf("apply inbound to old committee: %w", err)
	}
	return nil
}

// UpdateNew feeds one parsed message into this device's new-committee party.
func (p *ReshareParty) UpdateNew(msg tss.ParsedMessage) error {
	if samePID(msg.GetFrom(), p.newSelf) {
		return nil
	}
	if _, err := p.newParty.Update(msg); err != nil {
		return fmt.Errorf("apply inbound to new committee: %w", err)
	}
	return nil
}

// Done blocks until BOTH local parties (old and new) finish, then returns
// the new committee's refreshed share. The master ECDSAPub invariant
// (sdk.md §7) is asserted before returning; any drift fails closed.
func (p *ReshareParty) Done(ctx context.Context) (Share, error) {
	var newSave *keygen.LocalPartySaveData
	ended := 0
	for ended < 2 {
		select {
		case <-ctx.Done():
			return Share{}, fmt.Errorf("resharing cancelled: %w", ctx.Err())
		case err := <-p.errCh:
			return Share{}, fmt.Errorf("tss resharing error: %w", err)
		case save := <-p.endCh:
			if save.Xi != nil {
				newSave = save
			}
			ended++
		}
	}
	if newSave == nil {
		return Share{}, fmt.Errorf("new committee party produced no share")
	}
	if newSave.ECDSAPub == nil ||
		newSave.ECDSAPub.X().Cmp(p.wantPub.X()) != 0 ||
		newSave.ECDSAPub.Y().Cmp(p.wantPub.Y()) != 0 {
		return Share{}, fmt.Errorf("reshared share public key drift detected")
	}
	bz, err := MarshalSaveData(newSave)
	if err != nil {
		return Share{}, fmt.Errorf("serialize new share: %w", err)
	}
	return Share{Moniker: p.newSelf.Moniker, SaveData: bz}, nil
}

func samePID(a, b *tss.PartyID) bool {
	return a != nil && b != nil && a.KeyInt().Cmp(b.KeyInt()) == 0
}
