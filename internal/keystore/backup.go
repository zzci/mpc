package keystore

import (
	"encoding/json"
	"fmt"
)

// ExportShare produces a portable, passphrase-encrypted backup of a share.
// It deliberately omits the device factor (PassphraseOnly), so the backup can
// be restored on a different device — matching docs/design/mcp/sdk.md §6, where a
// lost member is rebuilt via resharing, not from a device-bound blob.
func ExportShare(share Share, passphrase string) ([]byte, error) {
	if share.Moniker == "" {
		return nil, fmt.Errorf("keystore: share moniker must not be empty")
	}
	plain, err := json.Marshal(share)
	if err != nil {
		return nil, fmt.Errorf("keystore: marshal share: %w", err)
	}
	defer wipe(plain)
	return seal(plain, passphrase, PassphraseOnly{}.ID(), nil)
}

// ImportShare reverses ExportShare. A wrong passphrase or corrupted backup
// surfaces as ErrDecrypt; an unknown format version as ErrVersionMismatch.
func ImportShare(blob []byte, passphrase string) (Share, error) {
	plain, err := open(blob, passphrase, nil)
	if err != nil {
		return Share{}, err
	}
	defer wipe(plain)
	var share Share
	if err := json.Unmarshal(plain, &share); err != nil {
		return Share{}, ErrFormat
	}
	return share, nil
}
