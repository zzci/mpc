package relay

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/libp2p/go-libp2p/core/pnet"
)

// pnet PSK handling implements server.md R4 layer 1 (private network): a peer
// without the 32-byte swarm key cannot speak the protocol, so the relay is
// invisible on the public internet. Per config framework v2 (user ruling
// 2026-05-19) the value may be a literal or an env:/file: reference; the
// resolved bytes must decode to a 32-byte key. internal/server is out of
// N-002 scope so the same resolution convention is mirrored here, restricted
// to producing the pnet key.

const pskLen = 32

// errSecretMissing is an empty/unset value (literal or resolved reference).
var errSecretMissing = errors.New("relay: required pnet psk missing")

// resolvePSK resolves the relay.pnet_psk value (a literal, or an env:VAR /
// file:/path reference) and decodes it into a 32-byte libp2p private-network
// swarm key. The resolved value must be exactly 32 bytes, accepted as 64 hex
// chars or std base64; anything else fails fast.
func resolvePSK(v string) (pnet.PSK, error) {
	raw, err := resolveSecretRef(v)
	if err != nil {
		return nil, err
	}
	raw = strings.TrimSpace(raw)
	if key, err := hex.DecodeString(raw); err == nil && len(key) == pskLen {
		return key, nil
	}
	if key, err := base64.StdEncoding.DecodeString(raw); err == nil && len(key) == pskLen {
		return key, nil
	}
	return nil, fmt.Errorf("relay: pnet psk must decode to %d bytes (hex or base64)", pskLen)
}

// resolveSecretRef resolves env:VAR / file:/path references and returns any
// other non-empty string as a literal (config framework v2: literals are
// allowed alongside references). Empty / unset = errSecretMissing.
func resolveSecretRef(v string) (string, error) {
	v = strings.TrimSpace(v)
	switch {
	case v == "":
		return "", errSecretMissing
	case strings.HasPrefix(v, "env:"):
		got := os.Getenv(strings.TrimPrefix(v, "env:"))
		if got == "" {
			return "", errSecretMissing
		}
		return got, nil
	case strings.HasPrefix(v, "file:"):
		b, err := os.ReadFile(strings.TrimPrefix(v, "file:"))
		if err != nil {
			return "", fmt.Errorf("relay: read pnet psk file: %w", err)
		}
		got := strings.TrimSpace(string(b))
		if got == "" {
			return "", errSecretMissing
		}
		return got, nil
	default:
		return v, nil
	}
}
