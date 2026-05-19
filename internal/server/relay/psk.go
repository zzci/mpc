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
// invisible on the public internet. The key is a secret: server.md "secret handling"
// forbids plaintext in committed config, mandating env:/file: injection. N-001
// validates the reference resolves but does not expose the resolved bytes
// (resolveSecret is unexported and internal/node is out of N-002 scope), so
// the same env:/file: convention is mirrored here, restricted to producing the
// pnet key.

const pskLen = 32

// errSecretMissing mirrors N-001's contract: an empty or unset reference.
var errSecretMissing = errors.New("relay: required secret missing")

// resolvePSK resolves the relay.pnet_psk_ref reference (env:VAR / file:/path)
// and decodes it into a 32-byte libp2p private-network swarm key. The decoded
// secret must be exactly 32 bytes, accepted as 64 hex chars or std base64;
// anything else (including a plaintext literal, rejected by N-001's
// Validate before this is reached) fails fast.
func resolvePSK(ref string) (pnet.PSK, error) {
	raw, err := resolveSecretRef(ref)
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

// resolveSecretRef accepts only env:VAR / file:/path references, mirroring
// N-001 (server.md "secret handling": secrets injected by reference, never plaintext).
func resolveSecretRef(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	switch {
	case ref == "":
		return "", errSecretMissing
	case strings.HasPrefix(ref, "env:"):
		v := os.Getenv(strings.TrimPrefix(ref, "env:"))
		if v == "" {
			return "", errSecretMissing
		}
		return v, nil
	case strings.HasPrefix(ref, "file:"):
		b, err := os.ReadFile(strings.TrimPrefix(ref, "file:"))
		if err != nil {
			return "", fmt.Errorf("relay: read secret file: %w", err)
		}
		v := strings.TrimSpace(string(b))
		if v == "" {
			return "", errSecretMissing
		}
		return v, nil
	default:
		return "", fmt.Errorf("relay: secret must be an env: or file: reference")
	}
}
