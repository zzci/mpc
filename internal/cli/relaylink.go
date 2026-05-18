package cli

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"time"

	btcec "github.com/btcsuite/btcd/btcec/v2"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/text/unicode/norm"

	"github.com/royqta/mcp-wallet/internal/contract"
	"github.com/royqta/mcp-wallet/internal/server/relay"
)

// Relay link glue (the M-005 transport client <-> N-002 relay INTEROP the
// relay's capservice.go documents as "reconciled by L2 at merge"): a device
// presents a wallet-group-signed CapToken over relay.CapProtocolID, then
// reserves a circuit-relay v2 slot. The CapToken canonical preimage is
// reconstructed here byte-for-byte from contract.CapToken's public shape
// (domain-separated, NFC + uint32-LP, same discipline as N-002's unexported
// capTokenDigest) so the carrier can act as the self-sovereign group-key
// issuer without depending on any unexported relay/contract symbol.

var capTokenDomain = append([]byte("TSS-CAPTOKEN-CANONICAL-v1"), 0x00)

func appendLP(b, v []byte) []byte {
	var lp [4]byte
	binary.BigEndian.PutUint32(lp[:], uint32(len(v)))
	b = append(b, lp[:]...)
	return append(b, v...)
}

// capTokenDigest reproduces N-002's CapToken preimage SHA-256.
func capTokenDigest(t *contract.CapToken) [32]byte {
	var b []byte
	b = append(b, capTokenDomain...)
	b = appendLP(b, norm.NFC.Bytes([]byte(t.GroupID)))
	b = appendLP(b, norm.NFC.Bytes([]byte(t.MemberID)))
	b = appendLP(b, norm.NFC.Bytes([]byte(t.Scope)))
	b = binary.BigEndian.AppendUint64(b, uint64(t.NotBefore))
	b = binary.BigEndian.AppendUint64(b, uint64(t.NotAfter))
	b = appendLP(b, t.Nonce)
	return sha256.Sum256(b)
}

// mintCapToken issues a wallet-group-signed CapToken (the self-sovereign trust
// anchor the relay verifies against, protocol.md §6 / server.md R4).
func mintCapToken(groupKey *btcec.PrivateKey, groupID, memberID string, scope contract.CapScope) *contract.CapToken {
	now := time.Now().UnixMilli()
	t := &contract.CapToken{
		GroupID:   groupID,
		MemberID:  memberID,
		Scope:     scope,
		NotBefore: now - 5_000,
		NotAfter:  now + 600_000,
		Nonce:     []byte(memberID + ":" + string(scope)),
	}
	d := capTokenDigest(t)
	t.GroupSig = contract.SignDigest(groupKey, d)
	return t
}

// presentCap connects h to the relay and presents tok over CapProtocolID,
// returning an error unless the relay accepts it (status byte 0).
func presentCap(ctx context.Context, h host.Host, relayAI peer.AddrInfo, tok *contract.CapToken) error {
	if err := h.Connect(ctx, relayAI); err != nil {
		return fmt.Errorf("cli: connect relay: %w", err)
	}
	s, err := h.NewStream(ctx, relayAI.ID, relay.CapProtocolID)
	if err != nil {
		return fmt.Errorf("cli: open cap stream: %w", err)
	}
	defer func() { _ = s.Close() }()
	raw, err := json.Marshal(tok)
	if err != nil {
		return fmt.Errorf("cli: marshal cap token: %w", err)
	}
	if _, err := s.Write(raw); err != nil {
		return fmt.Errorf("cli: write cap token: %w", err)
	}
	_ = s.CloseWrite()
	buf := make([]byte, 1)
	if _, err := io.ReadFull(s, buf); err != nil {
		return fmt.Errorf("cli: read cap status: %w", err)
	}
	if buf[0] != 0 {
		return fmt.Errorf("cli: relay rejected cap token (status %d)", buf[0])
	}
	return nil
}
