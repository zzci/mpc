package mpc

import (
	"context"
	"fmt"
	"time"

	"github.com/bnb-chain/tss-lib/v3/crypto"
	"github.com/bnb-chain/tss-lib/v3/ecdsa/keygen"
	"github.com/bnb-chain/tss-lib/v3/ecdsa/resharing"
	"github.com/bnb-chain/tss-lib/v3/tss"
)

// ReshareConfig parameterizes an ECDSA (secp256k1) threshold resharing: the
// old committee's existing shares are redistributed onto a new committee with
// a possibly different (t, n) — covering member-set changes, expansion, and
// reset. The wallet's master public key, and therefore every derived chain
// address, is invariant across resharing (docs/design/mcp/sdk.md §7).
type ReshareConfig struct {
	// OldThreshold is the t the OldShares were created under (any t+1 of the
	// original committee reconstruct the key). The participating OldShares
	// must number at least OldThreshold+1.
	OldThreshold int

	// OldShares are the participating old-committee shares (Keygen or prior
	// Reshare output). Their SaveData carries the secret material consumed to
	// rebuild the new committee. Order is irrelevant: the old committee is
	// reconstructed deterministically from each share's embedded ShareID.
	OldShares []Share

	// NewThreshold is t' for the regenerated committee: any t'+1 of the new
	// shares can sign afterwards.
	NewThreshold int

	// NewParties is n', the number of shares the new committee receives.
	NewParties int

	// PreParams optionally supplies pre-computed Paillier/safe-prime
	// parameters for the NEW parties (len must equal NewParties). When nil,
	// each new party generates its own locally.
	//
	// RED LINE — custody invariant: same as KeygenConfig.PreParams. In
	// production these MUST be generated locally on each new participant's own
	// device; a backend that pre-generates and pushes PreParams would break
	// the self-custody / co-management model. This parameter exists only so a
	// caller can pass values it generated itself.
	PreParams []keygen.LocalPreParams

	// PreParamTimeout bounds internal generation for new parties when
	// PreParams is nil. Zero means defaultPreParamTimeout.
	PreParamTimeout time.Duration
}

// Reshare runs an n-party ECDSA threshold resharing entirely in-process: the
// old and new committees communicate only over Go channels (no network, relay,
// or coordinator — that lands in later tasks). It returns one serialized Share
// per new party, ordered by new-party index.
//
// The returned shares carry the same ECDSAPub as the input; Reshare asserts
// this invariant internally and fails closed if it is ever violated.
func Reshare(ctx context.Context, cfg ReshareConfig) ([]Share, error) {
	saves, pids, err := simulateReshare(ctx, cfg, false)
	if err != nil {
		return nil, err
	}
	shares := make([]Share, len(saves))
	for i, sd := range saves {
		bz, mErr := MarshalSaveData(sd)
		if mErr != nil {
			return nil, fmt.Errorf("serialize new share %d: %w", i, mErr)
		}
		shares[i] = Share{Moniker: pids[i].Moniker, SaveData: bz}
	}
	return shares, nil
}

// simulateReshare builds the old and new committees, wires both through
// in-memory channels, and runs the resharing protocol to completion. It
// returns the new committee's save data (ordered by recovered new-party index)
// and the sorted new party IDs.
//
// fast skips the expensive Paillier modulus / factor zero-knowledge proofs on
// the new committee. This is for the in-process test harness only — keep it
// false on any path that stands in for the real protocol.
func simulateReshare(
	ctx context.Context,
	cfg ReshareConfig,
	fast bool,
) ([]*keygen.LocalPartySaveData, tss.SortedPartyIDs, error) {
	oldN := len(cfg.OldShares)
	if oldN < 2 {
		return nil, nil, fmt.Errorf("resharing needs at least 2 old shares, got %d", oldN)
	}
	if cfg.OldThreshold < 1 || cfg.OldThreshold+1 > oldN {
		return nil, nil, fmt.Errorf(
			"old threshold must satisfy 1 <= t and t+1 <= participating old shares, got t=%d shares=%d",
			cfg.OldThreshold, oldN)
	}
	if cfg.NewParties < 2 {
		return nil, nil, fmt.Errorf("resharing needs at least 2 new parties, got %d", cfg.NewParties)
	}
	if cfg.NewThreshold < 1 || cfg.NewThreshold >= cfg.NewParties {
		return nil, nil, fmt.Errorf(
			"new threshold must satisfy 1 <= t' < n', got t'=%d n'=%d",
			cfg.NewThreshold, cfg.NewParties)
	}
	if len(cfg.PreParams) != 0 && len(cfg.PreParams) != cfg.NewParties {
		return nil, nil, fmt.Errorf(
			"PreParams length %d does not match new parties %d", len(cfg.PreParams), cfg.NewParties)
	}

	oldPIDs, oldKeys, wantPub, err := reconstructOldCommittee(cfg.OldShares)
	if err != nil {
		return nil, nil, err
	}
	if cfg.OldThreshold+1 > len(oldPIDs) {
		return nil, nil, fmt.Errorf(
			"old threshold not satisfied after reconstruction: t+1=%d > old parties=%d",
			cfg.OldThreshold+1, len(oldPIDs))
	}

	newPIDs := tss.GenerateTestPartyIDs(cfg.NewParties)
	oldCtx := tss.NewPeerContext(oldPIDs)
	newCtx := tss.NewPeerContext(newPIDs)

	outCh := make(chan tss.Message, oldN+cfg.NewParties)
	endCh := make(chan *keygen.LocalPartySaveData, oldN+cfg.NewParties)

	oldParties := make([]tss.Party, oldN)
	for i := 0; i < oldN; i++ {
		params := tss.NewReSharingParameters(
			tss.S256(), oldCtx, newCtx, oldPIDs[i],
			oldN, cfg.OldThreshold, cfg.NewParties, cfg.NewThreshold)
		oldParties[i] = resharing.NewLocalParty(params, *oldKeys[i], outCh, endCh)
	}

	newParties := make([]tss.Party, cfg.NewParties)
	for i := 0; i < cfg.NewParties; i++ {
		params := tss.NewReSharingParameters(
			tss.S256(), oldCtx, newCtx, newPIDs[i],
			oldN, cfg.OldThreshold, cfg.NewParties, cfg.NewThreshold)
		if fast {
			params.SetNoProofMod()
			params.SetNoProofFac()
		}
		save := keygen.NewLocalPartySaveData(cfg.NewParties)
		pre, pErr := resolvePreParams(ctx, cfg.PreParams, i, cfg.PreParamTimeout)
		if pErr != nil {
			return nil, nil, pErr
		}
		save.LocalPreParams = *pre
		newParties[i] = resharing.NewLocalParty(params, save, outCh, endCh)
	}

	newSaves, err := runReshareProtocol(ctx, oldParties, newParties, outCh, endCh)
	if err != nil {
		return nil, nil, err
	}

	ordered := make([]*keygen.LocalPartySaveData, cfg.NewParties)
	for _, sd := range newSaves {
		idx, oErr := sd.OriginalIndex()
		if oErr != nil {
			return nil, nil, fmt.Errorf("recover new party index: %w", oErr)
		}
		ordered[idx] = sd
	}

	// Custody invariant (docs/design/mcp/sdk.md §7): the master public key — and
	// every address derived from it — must not change across resharing.
	for i, sd := range ordered {
		if sd == nil {
			return nil, nil, fmt.Errorf("new party %d produced no save data", i)
		}
		if sd.ECDSAPub == nil ||
			sd.ECDSAPub.X().Cmp(wantPub.X()) != 0 ||
			sd.ECDSAPub.Y().Cmp(wantPub.Y()) != 0 {
			return nil, nil, fmt.Errorf("new party %d public key changed across resharing", i)
		}
	}
	return ordered, newPIDs, nil
}

// reconstructOldCommittee rebuilds the participating old committee from the
// supplied shares. Each old party's identity key is its embedded ShareID
// (matching how tss-lib derives party IDs from persisted save data), so the
// committee is reconstructed deterministically regardless of input order. It
// returns the sorted party IDs, their full save data aligned to the sorted
// order, and the master public key the new committee must preserve.
func reconstructOldCommittee(
	shares []Share,
) (tss.SortedPartyIDs, []*keygen.LocalPartySaveData, *crypto.ECPoint, error) {
	unsorted := make(tss.UnSortedPartyIDs, 0, len(shares))
	byKey := make(map[string]*keygen.LocalPartySaveData, len(shares))
	var wantPub *crypto.ECPoint
	for i, sh := range shares {
		sd, err := UnmarshalSaveData(sh.SaveData)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("unmarshal old share %d: %w", i, err)
		}
		if sd.ShareID == nil {
			return nil, nil, nil, fmt.Errorf("old share %d has no ShareID", i)
		}
		if sd.ECDSAPub == nil {
			return nil, nil, nil, fmt.Errorf("old share %d has no ECDSAPub", i)
		}
		k := sd.ShareID.String()
		if _, dup := byKey[k]; dup {
			return nil, nil, nil, fmt.Errorf("old share %d duplicates an existing committee member", i)
		}
		if wantPub == nil {
			wantPub = sd.ECDSAPub
		} else if sd.ECDSAPub.X().Cmp(wantPub.X()) != 0 || sd.ECDSAPub.Y().Cmp(wantPub.Y()) != 0 {
			return nil, nil, nil, fmt.Errorf("old share %d belongs to a different key (ECDSAPub mismatch)", i)
		}
		byKey[k] = sd
		moniker := sh.Moniker
		if moniker == "" {
			moniker = k
		}
		unsorted = append(unsorted, tss.NewPartyID(moniker, moniker, sd.ShareID))
	}

	sorted := tss.SortPartyIDs(unsorted)
	keys := make([]*keygen.LocalPartySaveData, len(sorted))
	for i, pid := range sorted {
		keys[i] = byKey[pid.KeyInt().String()]
	}
	return sorted, keys, wantPub, nil
}

// runReshareProtocol drives the in-process resharing round to completion using
// only Go channels as the transport. Unlike keygen/signing, resharing spans
// two committees with independent index spaces, and every message is directed
// to the old committee, the new committee, or both — so it cannot reuse the
// single-slice routeMessage; it mirrors the upstream resharing test's dispatch
// while reusing the same wire-round-trip applier (updateParty).
//
// Both committees emit one end result: the new committee carries the rebuilt
// shares; the old committee's end has a nil Xi (its input share is zeroed) and
// is discarded. The function returns the new committee's save data once every
// party (old and new) has finished.
func runReshareProtocol(
	ctx context.Context,
	oldParties, newParties []tss.Party,
	outCh <-chan tss.Message,
	endCh <-chan *keygen.LocalPartySaveData,
) ([]*keygen.LocalPartySaveData, error) {
	total := len(oldParties) + len(newParties)
	errCh := make(chan *tss.Error, total)

	// Start the new parties first so they are waiting before the old
	// committee begins sending, matching the upstream reference ordering.
	for _, p := range newParties {
		go func(p tss.Party) {
			if err := p.Start(); err != nil {
				errCh <- err
			}
		}(p)
	}
	for _, p := range oldParties {
		go func(p tss.Party) {
			if err := p.Start(); err != nil {
				errCh <- err
			}
		}(p)
	}

	newSaves := make([]*keygen.LocalPartySaveData, 0, len(newParties))
	ended := 0
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("resharing cancelled: %w", ctx.Err())
		case err := <-errCh:
			return nil, fmt.Errorf("tss resharing error: %w", err)
		case msg := <-outCh:
			if err := routeReshareMessage(oldParties, newParties, msg, errCh); err != nil {
				return nil, err
			}
		case save := <-endCh:
			if save.Xi != nil {
				newSaves = append(newSaves, save)
			}
			ended++
			if ended == total {
				if len(newSaves) != len(newParties) {
					return nil, fmt.Errorf(
						"expected %d new committee results, got %d", len(newParties), len(newSaves))
				}
				return newSaves, nil
			}
		}
	}
}

// routeReshareMessage delivers one outbound resharing message to the old
// committee, the new committee, or both, mirroring the dispatch logic of the
// tss-lib resharing reference test loop.
func routeReshareMessage(
	oldParties, newParties []tss.Party,
	msg tss.Message,
	errCh chan<- *tss.Error,
) error {
	dest := msg.GetTo()
	if dest == nil {
		return fmt.Errorf("resharing message from party %d has no destination", msg.GetFrom().Index)
	}
	if msg.IsToOldCommittee() || msg.IsToOldAndNewCommittees() {
		for _, destP := range dest[:len(oldParties)] {
			go updateParty(oldParties[destP.Index], msg, errCh)
		}
	}
	if !msg.IsToOldCommittee() || msg.IsToOldAndNewCommittees() {
		for _, destP := range dest {
			go updateParty(newParties[destP.Index], msg, errCh)
		}
	}
	return nil
}
