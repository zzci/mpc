package mobileapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/royqta/mcp-wallet/internal/keystore"
	"github.com/royqta/mcp-wallet/internal/mpc"
)

// KeyGenCallback receives keygen progress and the terminal outcome. Every
// method is a Go→host call carrying only flat types (gomobile constraint,
// docs/design/mcp/sdk.md §2). Exactly one of OnResult / OnError fires, always after
// any OnProgress calls (callback ordering contract).
type KeyGenCallback interface {
	// OnProgress reports a coarse stage label ("preparams", "computing",
	// "sealing"); PreParams/MPC run off the UI thread (docs/design/mcp/sdk.md §5).
	OnProgress(stage string)
	// OnResult delivers a small JSON summary
	// {"threshold","parties","monikers":[...],"groupPubKey":"<hex>"}; the key
	// shares themselves stay Go-side, sealed in the keystore.
	OnResult(summaryJSON string)
	// OnError reports a terminal failure as a stable {code,msg} pair.
	OnError(code string, msg string)
}

// keygenConfig is the configJSON schema for KeyGen.
type keygenConfig struct {
	Threshold  int    `json:"threshold"`
	Parties    int    `json:"parties"`
	Passphrase string `json:"passphrase"`
}

// keygenSummary is the OnResult payload.
type keygenSummary struct {
	Threshold   int      `json:"threshold"`
	Parties     int      `json:"parties"`
	Monikers    []string `json:"monikers"`
	GroupPubKey string   `json:"groupPubKey"`
}

// KeyGen runs a t-of-n ECDSA threshold keygen on a background goroutine and
// persists every produced share to the device keystore, sealed with the user
// passphrase plus the device factor (docs/design/mcp/sdk.md §5/§6). It returns
// immediately; all outcomes arrive via cb. configJSON is
// {"threshold","parties","passphrase"}.
//
// In production each party generates its own PreParams locally (RED LINE: a
// backend MUST NOT pre-generate or push them, mpc.KeygenConfig.PreParams).
func (s *SDK) KeyGen(configJSON string, cb KeyGenCallback) {
	var cfg keygenConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		cb.OnError(CodeBadConfig, fmt.Sprintf("invalid configJSON: %v", err))
		return
	}
	if cfg.Threshold < 1 || cfg.Parties < 2 || cfg.Threshold >= cfg.Parties {
		cb.OnError(CodeBadConfig, fmt.Sprintf("need 1 <= threshold < parties, got t=%d n=%d", cfg.Threshold, cfg.Parties))
		return
	}
	if cfg.Passphrase == "" {
		cb.OnError(CodeBadConfig, "passphrase must not be empty")
		return
	}

	go s.runKeyGen(cfg, cb)
}

func (s *SDK) runKeyGen(cfg keygenConfig, cb KeyGenCallback) {
	cb.OnProgress("preparams")
	shares, err := mpc.Keygen(context.Background(), mpc.KeygenConfig{
		Threshold: cfg.Threshold,
		Parties:   cfg.Parties,
		PreParams: s.preParams,
	})
	if err != nil {
		cb.OnError(CodeInternal, fmt.Sprintf("keygen failed: %v", err))
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
			cb.OnError(CodeInternal, fmt.Sprintf("seal share %q: %v", sh.Moniker, err))
			return
		}
		monikers = append(monikers, sh.Moniker)
	}

	s.setGroup(shares, cfg.Threshold, pubHex)

	out, err := json.Marshal(keygenSummary{
		Threshold:   cfg.Threshold,
		Parties:     cfg.Parties,
		Monikers:    monikers,
		GroupPubKey: pubHex,
	})
	if err != nil {
		cb.OnError(CodeInternal, fmt.Sprintf("marshal summary: %v", err))
		return
	}
	cb.OnResult(string(out))
}
