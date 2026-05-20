package mpc

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/bnb-chain/tss-lib/v3/common"
	"github.com/bnb-chain/tss-lib/v3/ecdsa/keygen"
	"github.com/bnb-chain/tss-lib/v3/ecdsa/signing"
	"github.com/bnb-chain/tss-lib/v3/tss"
)

// digestLen is the fixed byte length of the message digest the threshold
// signer accepts. The chain-specific 32-byte digest is derived upstream
// (tx-decode, docs/design/mcp/sdk.md §3); mpc-core signs that opaque digest as-is.
const digestLen = 32

// Signature is a threshold ECDSA (secp256k1) signature in {R,S,V} form.
//
// S is already low-S normalized and V (the recovery id) is adjusted to match
// by tss-lib's finalization round, so a consumer can run secp256k1 ecrecover
// over (digest, R, S, V) and recover the group master public key. V is the raw
// recovery id in {0,1,2,3}; Ethereum-style consumers add 27 (or 27+chainId*2).
type Signature struct {
	R [32]byte
	S [32]byte
	V byte
}

// Compact returns the 65-byte [V+27 || R || S] form consumed by
// btcec/secp256k1 ecdsa.RecoverCompact.
func (s Signature) Compact() []byte {
	out := make([]byte, 65)
	out[0] = s.V + 27
	copy(out[1:33], s.R[:])
	copy(out[33:65], s.S[:])
	return out
}

// SignConfig parameterizes one threshold-signing session.
type SignConfig struct {
	// SessionID strongly isolates this session (= requestId for a signing
	// request; docs/design/mcp/sdk.md §5, contract/protocol.md §3). Each session
	// runs on its own party set and channels, so concurrent sessions never
	// cross-talk. Must be non-empty.
	SessionID string
	// Threshold is t in the t-of-n scheme the shares were generated for.
	Threshold int
	// Shares are the keygen shares of the participating signers. There must
	// be at least Threshold+1 of them; every provided share signs.
	Shares []Share
	// Digest is the 32-byte message digest to sign.
	Digest []byte

	// ChildPub and KeyDerivationDelta together opt this session into HD-child
	// signing per address-derivation.md §6 (tss-lib KDD path, no reshare, no
	// child-private-key reconstruction). Compute them once via
	// internal/hd.Derive(masterPub, chaincode, index).
	//
	// When both are nil the session signs against the group master key (the
	// default, pre-AD-1 behaviour). When both are set the produced signature
	// recovers to ChildPub; the in-memory keygen shares are adjusted for this
	// session only (UpdatePublicKeyAndAdjustBigXj mutates copies, never the
	// persisted Shares slice). Mixing (one nil, one not) is rejected — a
	// half-set delta is always a caller bug.
	ChildPub           *ecdsa.PublicKey
	KeyDerivationDelta *big.Int
}

// Sign runs an in-process threshold ECDSA signing session over secp256k1: the
// signers communicate only over Go channels (no network or coordinator — that
// lands in later tasks), reusing the M-001 runProtocol[E] harness. It returns
// the low-S normalized {R,S,V} signature.
func Sign(ctx context.Context, cfg SignConfig) (Signature, error) {
	if cfg.SessionID == "" {
		return Signature{}, fmt.Errorf("signing requires a non-empty SessionID")
	}
	if len(cfg.Digest) != digestLen {
		return Signature{}, fmt.Errorf("digest must be %d bytes, got %d", digestLen, len(cfg.Digest))
	}
	if cfg.Threshold < 1 {
		return Signature{}, fmt.Errorf("threshold must be >= 1, got %d", cfg.Threshold)
	}
	if (cfg.ChildPub == nil) != (cfg.KeyDerivationDelta == nil) {
		return Signature{}, fmt.Errorf("signing: ChildPub and KeyDerivationDelta must both be set or both nil")
	}
	signers := len(cfg.Shares)
	if signers < cfg.Threshold+1 {
		return Signature{}, fmt.Errorf("signing needs at least t+1=%d shares, got %d", cfg.Threshold+1, signers)
	}

	keysByShareID := make(map[string]keygen.LocalPartySaveData, signers)
	unsorted := make(tss.UnSortedPartyIDs, signers)
	for i, sh := range cfg.Shares {
		sd, err := UnmarshalSaveData(sh.SaveData)
		if err != nil {
			return Signature{}, fmt.Errorf("share %d: %w", i, err)
		}
		keysByShareID[sd.ShareID.String()] = *sd
		unsorted[i] = tss.NewPartyID(sh.Moniker, sh.Moniker, sd.ShareID)
	}
	pids := tss.SortPartyIDs(unsorted)

	// SortPartyIDs reorders by share key, so pair each sorted party with the
	// save data carrying its ShareID (its own secret Xi).
	keys := make([]keygen.LocalPartySaveData, signers)
	for i, pid := range pids {
		sd, ok := keysByShareID[pid.KeyInt().String()]
		if !ok {
			return Signature{}, fmt.Errorf("no share for signer %s", pid.Moniker)
		}
		keys[i] = sd
	}

	// HD-child wiring (address-derivation.md §6): apply the keyDerivationDelta
	// to every signer's in-memory share so each party signs against Q_child.
	// The mutation is on `keys` (a fresh slice produced above from Unmarshal);
	// the caller's persisted Share blobs are never touched, so the same shares
	// can sign indefinitely many child indices without ever reshare-rotating.
	if cfg.KeyDerivationDelta != nil {
		if err := signing.UpdatePublicKeyAndAdjustBigXj(cfg.KeyDerivationDelta, keys, cfg.ChildPub, tss.S256()); err != nil {
			return Signature{}, fmt.Errorf("apply key derivation delta: %w", err)
		}
	}

	p2pCtx := tss.NewPeerContext(pids)
	msg := new(big.Int).SetBytes(cfg.Digest)

	outCh := make(chan tss.Message, signers)
	endCh := make(chan *common.SignatureData, signers)

	parties := make([]tss.Party, signers)
	for i := 0; i < signers; i++ {
		params := tss.NewParameters(tss.S256(), p2pCtx, pids[i], signers, cfg.Threshold)
		// NewLocalPartyWithKDD with a nil delta is exactly NewLocalParty (see
		// tss-lib local_party.go), so the master-signing path is unchanged.
		parties[i] = signing.NewLocalPartyWithKDD(msg, params, keys[i], cfg.KeyDerivationDelta, outCh, endCh, digestLen)
	}

	results, err := runProtocol(ctx, parties, outCh, endCh)
	if err != nil {
		return Signature{}, err
	}

	sd := results[0]
	if len(sd.SignatureRecovery) == 0 {
		return Signature{}, fmt.Errorf("signing produced no recovery id")
	}
	var sig Signature
	copy(sig.R[:], sd.R)
	copy(sig.S[:], sd.S)
	sig.V = sd.SignatureRecovery[0]
	return sig, nil
}
