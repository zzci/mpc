package mobileapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/royqta/mcp-wallet/internal/keystore"
	"github.com/royqta/mcp-wallet/internal/mpc"
)

// ReshareCallback mirrors KeyGenCallback for a resharing run. The master
// public key — and therefore every derived chain address — is invariant
// across resharing (docs/design/mcp/sdk.md §7); the summary's groupPubKey MUST
// equal the pre-reshare value.
type ReshareCallback interface {
	// OnProgress reports a coarse stage label.
	OnProgress(stage string)
	// OnResult delivers the new-committee summary JSON (same shape as keygen).
	OnResult(summaryJSON string)
	// OnError reports a terminal failure as a stable {code,msg} pair.
	OnError(code string, msg string)
}

// reshareConfig is the configJSON schema for Reshare.
type reshareConfig struct {
	OldThreshold int    `json:"oldThreshold"`
	NewThreshold int    `json:"newThreshold"`
	NewParties   int    `json:"newParties"`
	Passphrase   string `json:"passphrase"`
}

// Reshare redistributes the in-process committee's shares onto a new (t', n')
// committee on a background goroutine, reseals the new shares to the keystore,
// and keeps the master public key fixed (docs/design/mcp/sdk.md §7). configJSON is
// {"oldThreshold","newThreshold","newParties","passphrase"}.
func (s *SDK) Reshare(configJSON string, cb ReshareCallback) {
	var cfg reshareConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		cb.OnError(CodeBadConfig, fmt.Sprintf("invalid configJSON: %v", err))
		return
	}
	if cfg.OldThreshold < 1 || cfg.NewThreshold < 1 || cfg.NewParties < 2 ||
		cfg.NewThreshold >= cfg.NewParties {
		cb.OnError(CodeBadConfig, fmt.Sprintf("need t>=1 and 1<=t'<n', got oldT=%d newT=%d newN=%d",
			cfg.OldThreshold, cfg.NewThreshold, cfg.NewParties))
		return
	}
	if cfg.Passphrase == "" {
		cb.OnError(CodeBadConfig, "passphrase must not be empty")
		return
	}
	go s.runReshare(cfg, cb)
}

func (s *SDK) runReshare(cfg reshareConfig, cb ReshareCallback) {
	old, _, ok := s.snapshotShares()
	if !ok {
		cb.OnError(CodeNoShares, "no key share held to reshare from")
		return
	}

	cb.OnProgress("computing")
	shares, err := mpc.Reshare(context.Background(), mpc.ReshareConfig{
		OldThreshold: cfg.OldThreshold,
		OldShares:    old,
		NewThreshold: cfg.NewThreshold,
		NewParties:   cfg.NewParties,
		PreParams:    s.preParams,
	})
	if err != nil {
		cb.OnError(CodeInternal, fmt.Sprintf("reshare failed: %v", err))
		return
	}

	cb.OnProgress("sealing")
	pubHex, err := groupPubHex(shares[0])
	if err != nil {
		cb.OnError(CodeInternal, err.Error())
		return
	}
	monikers := make([]string, 0, len(shares))
	for _, sh := range shares {
		if err := s.store.Save(context.Background(), keystore.Share{
			Moniker:  sh.Moniker,
			SaveData: sh.SaveData,
		}, cfg.Passphrase); err != nil {
			cb.OnError(CodeInternal, fmt.Sprintf("seal new share %q: %v", sh.Moniker, err))
			return
		}
		monikers = append(monikers, sh.Moniker)
	}

	s.setGroup(shares, cfg.NewThreshold, pubHex)

	out, err := json.Marshal(keygenSummary{
		Threshold:   cfg.NewThreshold,
		Parties:     cfg.NewParties,
		Monikers:    monikers,
		GroupPubKey: pubHex,
	})
	if err != nil {
		cb.OnError(CodeInternal, fmt.Sprintf("marshal summary: %v", err))
		return
	}
	cb.OnResult(string(out))
}
