package mpcnet

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"time"

	"github.com/bnb-chain/tss-lib/v3/ecdsa/keygen"

	"github.com/zzci/mpc/internal/mpc"
)

// Engine entry points: one Go-level function per protocol (keygen, signing,
// rotate-mode reshare), each driving THIS device's view against a caller-
// supplied transport. The engine never holds a peer's share — only this
// device's share_i flows in (Sign / Reshare) or out (Keygen / Reshare).
//
// All three Run* functions share the lifecycle pattern:
//   1. Construct the corresponding mpc.* party (DM-1 layer).
//   2. Launch the pump goroutine: it starts the party, drains Out() onto the
//      transport and feeds tr.Inbound() back through party.Update*.
//   3. Block on party.Done(ctx) to collect the device's result.
//   4. Signal the pump to drain residuals + flush in-flight sends, then return.
//
// The pump's stop signal is closed AFTER party.Done returns so the final
// round messages (which tss-lib emits to Out() *before* the end signal) are
// dispatched before the device closes the transport. cli/mpcnet.go enforces
// the same ordering invariant for E2E zero-regression.

// KeygenConfig parameterises this device's view of an n-party distributed
// ECDSA threshold keygen run over the transport.
type KeygenConfig struct {
	// PartyIndex is this device's 0-based index in the committee.
	PartyIndex int
	// Parties is n.
	Parties int
	// Threshold is t in t-of-n.
	Threshold int
	// PreParams optionally supplies this device's pre-computed Paillier /
	// safe-prime parameters. When nil, generated locally on this device.
	// Custody invariant: never server-supplied (mpc.SinglePartyKeygenConfig).
	PreParams *keygen.LocalPreParams
	// PreParamTimeout bounds local PreParams generation when PreParams is nil.
	PreParamTimeout time.Duration
}

// SignConfig parameterises this device's view of one threshold-signing
// session: it carries the share, the digest to sign, and the participant set.
//
// When ChildPub and KeyDerivationDelta are both non-nil, this session signs
// the non-hardened HD child key per address-derivation.md §6 (KDD path); the
// caller is responsible for having computed (IL, Q_child) offline via
// internal/hd. When both are nil the session signs against the group master
// key; mixing one nil with one set is rejected as a caller bug by
// mpc.NewSignParty.
type SignConfig struct {
	// SessionID strongly isolates this signing session (= requestId; sdk.md
	// §5, protocol.md §3). Bound into senderAuth via the transport.
	SessionID string
	// PartyIndex is this device's 0-based index in the original n-party
	// committee that produced the share.
	PartyIndex int
	// Threshold is t the shares were generated for.
	Threshold int
	// Participants are the 0-based indices of every signer in this session.
	// Must contain PartyIndex and at least Threshold+1 distinct entries.
	Participants []int
	// Share is this device's keygen share (the only secret material).
	Share mpc.Share
	// Digest is the 32-byte message digest to sign.
	Digest []byte
	// ChildPub / KeyDerivationDelta opt this session into HD-child signing.
	ChildPub           *ecdsa.PublicKey
	KeyDerivationDelta *big.Int
}

// ReshareConfig parameterises this device's view of an in-place rotate-mode
// reshare: this device participates in BOTH the old and new committees at
// the same 0-based index, with the same committee size (n = n').
type ReshareConfig struct {
	// PartyIndex is this device's 0-based index in both committees.
	PartyIndex int
	// Parties is n = n', the size of both the old and new committees.
	Parties int
	// OldThreshold is t the OldShare was created under.
	OldThreshold int
	// NewThreshold is t' the regenerated committee will use.
	NewThreshold int
	// OldShare is this device's existing keygen share.
	OldShare mpc.Share
	// PreParams optionally supplies this device's pre-computed parameters
	// for its NEW-committee party. When nil, generated locally.
	PreParams *keygen.LocalPreParams
	// PreParamTimeout bounds local PreParams generation when PreParams is nil.
	PreParamTimeout time.Duration
}

// RunKeygen drives this device's keygen party over tr until the share is
// ready. peers must include every OTHER device's libp2p peer.ID indexed by
// the 1-based decimal party tag ("1".."n"); cfg.PartyIndex's own tag may
// be absent or present — sendSingle skips self either way.
func RunKeygen(ctx context.Context, tr Transport, peers PeerTable, cfg KeygenConfig) (mpc.Share, error) {
	party, err := mpc.NewKeygenParty(ctx, mpc.SinglePartyKeygenConfig{
		Threshold:       cfg.Threshold,
		Parties:         cfg.Parties,
		PartyIndex:      cfg.PartyIndex,
		PreParams:       cfg.PreParams,
		PreParamTimeout: cfg.PreParamTimeout,
	})
	if err != nil {
		return mpc.Share{}, fmt.Errorf("mpcnet: new keygen party: %w", err)
	}
	return runSingleKeygen(ctx, tr, peers, party)
}

func runSingleKeygen(ctx context.Context, tr Transport, peers PeerTable, party *mpc.KeygenParty) (mpc.Share, error) {
	stop := make(chan struct{})
	done := make(chan struct{})
	var pumpErr error
	go func() {
		pumpErr = pumpSingle(ctx, tr, party, peers, stop)
		close(done)
	}()

	share, doneErr := party.Done(ctx)
	close(stop)
	<-done

	if doneErr != nil {
		return mpc.Share{}, fmt.Errorf("mpcnet: keygen: %w", doneErr)
	}
	if pumpErr != nil {
		return mpc.Share{}, fmt.Errorf("mpcnet: keygen pump: %w", pumpErr)
	}
	return share, nil
}

// RunSign drives this device's signing party over tr until the joint {R,S,V}
// is produced. peers must include every OTHER signer's libp2p peer.ID indexed
// by the 1-based decimal party tag.
func RunSign(ctx context.Context, tr Transport, peers PeerTable, cfg SignConfig) (mpc.Signature, error) {
	party, err := mpc.NewSignParty(ctx, mpc.SinglePartySignConfig{
		SessionID:          cfg.SessionID,
		Threshold:          cfg.Threshold,
		PartyIndex:         cfg.PartyIndex,
		Participants:       cfg.Participants,
		Share:              cfg.Share,
		Digest:             cfg.Digest,
		ChildPub:           cfg.ChildPub,
		KeyDerivationDelta: cfg.KeyDerivationDelta,
	})
	if err != nil {
		return mpc.Signature{}, fmt.Errorf("mpcnet: new sign party: %w", err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	var pumpErr error
	go func() {
		pumpErr = pumpSingle(ctx, tr, party, peers, stop)
		close(done)
	}()

	sig, doneErr := party.Done(ctx)
	close(stop)
	<-done

	if doneErr != nil {
		return mpc.Signature{}, fmt.Errorf("mpcnet: signing: %w", doneErr)
	}
	if pumpErr != nil {
		return mpc.Signature{}, fmt.Errorf("mpcnet: signing pump: %w", pumpErr)
	}
	return sig, nil
}

// RunReshare drives this device's in-place rotate-mode reshare over tr.
// mpc.ReshareParty.Done() enforces the custody invariant (sdk.md §7): the new
// committee's master public key must equal the old one — any drift fails the
// session.
func RunReshare(ctx context.Context, tr Transport, peers PeerTable, cfg ReshareConfig) (mpc.Share, error) {
	party, err := mpc.NewReshareParty(ctx, mpc.SinglePartyReshareConfig{
		OldThreshold:    cfg.OldThreshold,
		NewThreshold:    cfg.NewThreshold,
		Parties:         cfg.Parties,
		PartyIndex:      cfg.PartyIndex,
		OldShare:        cfg.OldShare,
		PreParams:       cfg.PreParams,
		PreParamTimeout: cfg.PreParamTimeout,
	})
	if err != nil {
		return mpc.Share{}, fmt.Errorf("mpcnet: new reshare party: %w", err)
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	var pumpErr error
	go func() {
		pumpErr = pumpReshare(ctx, tr, party, peers, cfg.PartyIndex, cfg.Parties, stop)
		close(done)
	}()

	share, doneErr := party.Done(ctx)
	close(stop)
	<-done

	if doneErr != nil {
		return mpc.Share{}, fmt.Errorf("mpcnet: resharing: %w", doneErr)
	}
	if pumpErr != nil {
		return mpc.Share{}, fmt.Errorf("mpcnet: resharing pump: %w", pumpErr)
	}
	return share, nil
}
