package mobileapi

import (
	"fmt"

	"github.com/royqta/mcp-wallet/internal/keystore"
	"github.com/royqta/mcp-wallet/internal/mpc"
)

// ExportShare produces a portable, passphrase-encrypted backup of one held
// share, named by moniker. The backup deliberately omits the device factor so
// it can be restored on another device (keystore.ExportShare); per
// docs/design/mcp/sdk.md §6 a permanently lost member is rebuilt via resharing, not
// from a plaintext backup. moniker is taken explicitly (a device may hold
// more than one share in the in-process model); it is a flat string, so the
// gomobile constraint still holds.
func (s *SDK) ExportShare(moniker string, passphrase string) ([]byte, error) {
	s.mu.Lock()
	sh, ok := s.shares[moniker]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%s: no held share named %q", CodeNoShares, moniker)
	}
	blob, err := keystore.ExportShare(keystore.Share{
		Moniker:  sh.Moniker,
		SaveData: sh.SaveData,
	}, passphrase)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", CodeInternal, err)
	}
	return blob, nil
}

// ImportShare restores a share from an ExportShare backup into the process's
// in-memory committee and returns its moniker. It does not by itself rebuild
// group metadata (a single backup carries no threshold); a full restore path
// is keygen/reshare-driven (docs/design/mcp/sdk.md §6/§7).
func (s *SDK) ImportShare(blob []byte, passphrase string) (string, error) {
	sh, err := keystore.ImportShare(blob, passphrase)
	if err != nil {
		return "", fmt.Errorf("%s: %w", CodeInternal, err)
	}
	s.mu.Lock()
	s.shares[sh.Moniker] = mpc.Share{Moniker: sh.Moniker, SaveData: sh.SaveData}
	s.mu.Unlock()
	return sh.Moniker, nil
}
