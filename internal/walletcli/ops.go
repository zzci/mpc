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

// keygenOp runs a t-of-n keygen and returns the SDK summary JSON. progress
// receives coarse stage labels (terminal stderr for the session; discarded
// for the HTTP server).
func keygenOp(s *sdk.SDK, threshold, parties int, passphrase string, progress io.Writer) (string, error) {
	cfg, _ := json.Marshal(map[string]any{
		"threshold": threshold, "parties": parties, "passphrase": passphrase,
	})
	cb := newProgressCB(progress)
	s.KeyGen(string(cfg), cb)
	o := <-cb.done
	if !o.ok {
		return "", fmt.Errorf("%s: %s", o.code, o.msg)
	}
	return o.payload, nil
}

// reshareOp reshares the in-session committee and returns the summary JSON.
func reshareOp(s *sdk.SDK, oldT, newT, newN int, passphrase string, progress io.Writer) (string, error) {
	cfg, _ := json.Marshal(map[string]any{
		"oldThreshold": oldT, "newThreshold": newT, "newParties": newN, "passphrase": passphrase,
	})
	cb := newProgressCB(progress)
	s.Reshare(string(cfg), cb)
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
