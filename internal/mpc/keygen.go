package mpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bnb-chain/tss-lib/v3/ecdsa/keygen"
	"github.com/bnb-chain/tss-lib/v3/tss"
)

// defaultPreParamTimeout bounds internal safe-prime generation when the caller
// does not supply pre-computed parameters. Safe-prime search is slow, so this
// is deliberately generous.
const defaultPreParamTimeout = 5 * time.Minute

// Share is one party's keygen output in a transport-neutral, serialized form.
// The tss-lib LocalPartySaveData (and all of its nested cryptographic types)
// stays encapsulated inside this package; callers receive only Moniker plus an
// opaque SaveData blob to hand to MPC-003 (signing) and the keystore.
type Share struct {
	// Moniker is the stable party identity within the keygen run.
	Moniker string
	// SaveData is the JSON serialization of that party's keygen save data.
	SaveData []byte
}

// KeygenConfig parameterizes an ECDSA (secp256k1) threshold keygen.
type KeygenConfig struct {
	// Threshold is t in a t-of-n scheme: any t+1 parties can later sign.
	Threshold int
	// Parties is n, the total number of key shares produced.
	Parties int

	// PreParams optionally supplies pre-computed Paillier/safe-prime
	// parameters, one per party (len must equal Parties). When nil, each
	// party generates its own locally.
	//
	// RED LINE — custody invariant: in production these PreParams MUST be
	// generated locally on each participant's own device. A backend or
	// coordinator that pre-generates and pushes PreParams to clients would
	// break the self-custody / co-management model. This parameter exists
	// only so a caller can pass values it generated *itself* (e.g. via
	// keygen.GeneratePreParams ahead of time); it is NOT a hook for
	// server-side provisioning.
	PreParams []keygen.LocalPreParams

	// PreParamTimeout bounds internal generation when PreParams is nil.
	// Zero means defaultPreParamTimeout.
	PreParamTimeout time.Duration
}

// Keygen runs an n-party ECDSA threshold keygen entirely in-process: the
// parties communicate only over Go channels (no network, relay, or
// coordinator — that lands in later tasks). It returns one serialized Share
// per party, ordered by party index.
func Keygen(ctx context.Context, cfg KeygenConfig) ([]Share, error) {
	saves, pids, err := simulateKeygen(ctx, cfg.Threshold, cfg.Parties, cfg.PreParams, cfg.PreParamTimeout, false)
	if err != nil {
		return nil, err
	}
	shares := make([]Share, len(saves))
	for i, sd := range saves {
		bz, mErr := MarshalSaveData(sd)
		if mErr != nil {
			return nil, fmt.Errorf("serialize share %d: %w", i, mErr)
		}
		shares[i] = Share{Moniker: pids[i].Moniker, SaveData: bz}
	}
	return shares, nil
}

// MarshalSaveData serializes keygen save data to JSON for storage / handoff.
func MarshalSaveData(sd *keygen.LocalPartySaveData) ([]byte, error) {
	bz, err := json.Marshal(sd)
	if err != nil {
		return nil, fmt.Errorf("marshal save data: %w", err)
	}
	return bz, nil
}

// UnmarshalSaveData reverses MarshalSaveData. It re-attaches the secp256k1
// curve to the deserialized public points (which JSON cannot carry), matching
// how tss-lib itself reloads persisted save data.
func UnmarshalSaveData(bz []byte) (*keygen.LocalPartySaveData, error) {
	var sd keygen.LocalPartySaveData
	if err := json.Unmarshal(bz, &sd); err != nil {
		return nil, fmt.Errorf("unmarshal save data: %w", err)
	}
	for _, pt := range sd.BigXj {
		if pt != nil {
			pt.SetCurve(tss.S256())
		}
	}
	if sd.ECDSAPub != nil {
		sd.ECDSAPub.SetCurve(tss.S256())
	}
	return &sd, nil
}

// simulateKeygen builds n keygen parties, wires them through in-memory
// channels, and runs the protocol to completion. It returns the raw save data
// (ordered by recovered party index) and the sorted party IDs, so it can be
// reused by tests and by the multi-process E2E carrier (MPC-009).
//
// fast skips the expensive Paillier modulus / factor zero-knowledge proofs.
// This is for the in-process test harness only — keep it false on any path
// that stands in for the real protocol.
func simulateKeygen(
	ctx context.Context,
	threshold, parties int,
	preParams []keygen.LocalPreParams,
	preParamTimeout time.Duration,
	fast bool,
) ([]*keygen.LocalPartySaveData, tss.SortedPartyIDs, error) {
	if parties < 2 {
		return nil, nil, fmt.Errorf("keygen needs at least 2 parties, got %d", parties)
	}
	if threshold < 1 || threshold >= parties {
		return nil, nil, fmt.Errorf("threshold must satisfy 1 <= t < n, got t=%d n=%d", threshold, parties)
	}
	if len(preParams) != 0 && len(preParams) != parties {
		return nil, nil, fmt.Errorf("PreParams length %d does not match parties %d", len(preParams), parties)
	}

	pids := tss.GenerateTestPartyIDs(parties)
	p2pCtx := tss.NewPeerContext(pids)

	outCh := make(chan tss.Message, parties)
	endCh := make(chan *keygen.LocalPartySaveData, parties)

	tssParties := make([]tss.Party, parties)
	for i := 0; i < parties; i++ {
		params := tss.NewParameters(tss.S256(), p2pCtx, pids[i], parties, threshold)
		if fast {
			params.SetNoProofMod()
			params.SetNoProofFac()
		}

		pre, err := resolvePreParams(ctx, preParams, i, preParamTimeout)
		if err != nil {
			return nil, nil, err
		}
		tssParties[i] = keygen.NewLocalParty(params, outCh, endCh, *pre)
	}

	saves, err := runProtocol(ctx, tssParties, outCh, endCh)
	if err != nil {
		return nil, nil, err
	}

	ordered := make([]*keygen.LocalPartySaveData, parties)
	for _, sd := range saves {
		idx, oErr := sd.OriginalIndex()
		if oErr != nil {
			return nil, nil, fmt.Errorf("recover party index: %w", oErr)
		}
		ordered[idx] = sd
	}
	return ordered, pids, nil
}

// resolvePreParams returns the i-th party's pre-params: a caller-supplied value
// when present, otherwise freshly generated locally for that party.
func resolvePreParams(
	ctx context.Context,
	preParams []keygen.LocalPreParams,
	i int,
	timeout time.Duration,
) (*keygen.LocalPreParams, error) {
	if len(preParams) != 0 {
		return &preParams[i], nil
	}
	if timeout <= 0 {
		timeout = defaultPreParamTimeout
	}
	genCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	pre, err := keygen.GeneratePreParamsWithContext(genCtx)
	if err != nil {
		return nil, fmt.Errorf("generate pre-params for party %d: %w", i, err)
	}
	return pre, nil
}
