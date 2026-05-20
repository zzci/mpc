package walletcli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/zzci/mpc/sdk"
)

// ops are the shell-agnostic wallet operations: each delegates 1:1 to the
// shared SDK and returns a machine-readable result string (JSON or hex), so
// the interactive session and the HTTP server are two thin front-ends over
// one implementation. The keystore passphrase is always supplied by the
// caller from the process environment, never from operator/remote input.
//
// DM-3 transitional shape: keygenOp / reshareOp pack the new mandatory
// configJSON envelope (groupId, sessionID, partyIndex, n, t, memberSet,
// relay, role) with CLI-local placeholders and pass a stub WireCallbacks.
// DM-5 will replace these placeholders with a real libp2p host transport
// (distributed-mpc-impl.md §B); the CLI's interactive path therefore cannot
// drive a multi-device keygen by itself until DM-5 lands. The placeholder
// wiring keeps the SDK signature surface honest under hard-cut without
// resurrecting the legacy all-n in-process simulator.

// cliRelay is a placeholder relay block the CLI ships in every configJSON
// so the SDK's hard-cut validation passes. DM-5 replaces this with the
// host's real libp2p relay coordinates resolved from coord metadata.
var cliRelay = map[string]any{
	"peerID": "12D3KooWWalletCliPlaceholder0000000000000000000",
	"addrs":  []string{"/ip4/127.0.0.1/tcp/0"},
}

// cliWire is the placeholder Go→host outbound bridge for the interactive /
// HTTP CLI. Wire messages are discarded — DM-5 replaces this with a real
// transport. Until then, the CLI's keygen/sign/reshare paths cannot make
// progress beyond the configJSON gate without external orchestration.
type cliWire struct{}

func (cliWire) OnWireMessage(_ []byte) {}

// cliMembers fabricates a deterministic memberSet of length n. Real
// deployments derive memberSet from coord group_members; DM-5 will plumb
// that resolution from the coord client.
func cliMembers(n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = fmt.Sprintf("cli-member-%d", i+1)
	}
	return out
}

// wrapSignConfig builds the DM-3 hard-cut Sign configJSON envelope from a
// coord-delivered StartSigning JSON. The CLI's transitional defaults
// (cliMembers, n=2, t=1, partyIndex=0) mirror the keygen / reshare ops:
// the envelope passes the SDK's structural validation but the CLI cannot
// drive a real multi-party signing until DM-5 plumbs the host transport
// and resolves true memberSet / partyIndex from the coord group.
func wrapSignConfig(startJSON []byte) (string, error) {
	var head struct {
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(startJSON, &head); err != nil {
		return "", fmt.Errorf("parse start: %w", err)
	}
	if head.RequestID == "" {
		return "", fmt.Errorf("start.requestId is empty")
	}
	cfg, err := json.Marshal(map[string]any{
		"groupId":    "cli-local",
		"sessionID":  head.RequestID,
		"partyIndex": 0,
		"n":          2,
		"t":          1,
		"memberSet":  cliMembers(2),
		"relay":      cliRelay,
		"role":       "signer",
		"start":      json.RawMessage(startJSON),
	})
	if err != nil {
		return "", err
	}
	return string(cfg), nil
}

// keygenOp runs a t-of-n keygen and returns the SDK summary JSON. progress
// receives coarse stage labels (terminal stderr for the session; discarded
// for the HTTP server).
func keygenOp(s *sdk.SDK, threshold, parties int, passphrase string, progress io.Writer) (string, error) {
	cfg, _ := json.Marshal(map[string]any{
		"groupId":    "cli-local",
		"sessionID":  "cli-keygen-session",
		"partyIndex": 0,
		"n":          parties,
		"t":          threshold,
		"memberSet":  cliMembers(parties),
		"relay":      cliRelay,
		"role":       "keygen",
		"passphrase": passphrase,
	})
	cb := newProgressCB(progress)
	s.KeyGen(string(cfg), cliWire{}, cb)
	o := <-cb.done
	if !o.ok {
		return "", fmt.Errorf("%s: %s", o.code, o.msg)
	}
	return o.payload, nil
}

// reshareOp reshares the in-session committee and returns the summary JSON.
func reshareOp(s *sdk.SDK, oldT, newT, newN int, passphrase string, progress io.Writer) (string, error) {
	cfg, _ := json.Marshal(map[string]any{
		"groupId":    "cli-local",
		"sessionID":  "cli-reshare-session",
		"partyIndex": 0,
		"n":          newN,
		"oldT":       oldT,
		"newT":       newT,
		"memberSet":  cliMembers(newN),
		"relay":      cliRelay,
		"role":       "reshare",
		"passphrase": passphrase,
	})
	cb := newProgressCB(progress)
	s.Reshare(string(cfg), cliWire{}, cb)
	o := <-cb.done
	if !o.ok {
		return "", fmt.Errorf("%s: %s", o.code, o.msg)
	}
	return o.payload, nil
}

// importOp restores a backup blob into the in-memory committee.
func importOp(s *sdk.SDK, blob []byte, passphrase string) (string, error) {
	moniker, err := s.ImportShare(blob, passphrase)
	if err != nil {
		return "", err
	}
	return moniker, nil
}

// exportOp produces a passphrase-encrypted backup of one held share.
func exportOp(s *sdk.SDK, moniker, passphrase string) ([]byte, error) {
	return s.ExportShare(moniker, passphrase)
}

// fetchOp queries coord transaction info (no MPC); reqJSON is passed through.
func fetchOp(s *sdk.SDK, reqJSON string) (string, error) {
	return s.FetchTransactions(reqJSON)
}

// wireOp feeds one received MPC wire message through the SDK receive gate.
func wireOp(s *sdk.SDK, msg []byte) error {
	return s.OnWireMessage(msg)
}

// discard is an io.Writer that drops progress lines (HTTP/non-tty callers).
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
