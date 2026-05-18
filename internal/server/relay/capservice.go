package relay

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/royqta/mcp-wallet/internal/contract"
)

// CapProtocolID is the cap-grant presentation protocol. A member opens this
// stream BEFORE requesting a circuit-relay reservation or a rendezvous
// registration and presents its CapToken; the relay verifies groupSig/scope/
// TTL against the trusted group key set and records a grant keyed by the
// libp2p-authenticated peer.ID, which the relay ACL (relayacl.go) and the
// rendezvous service (rendezvous.go) then consult. circuit-relay v2 and
// rendezvous themselves use libp2p standard protocol IDs (server.md R6); this
// presentation step is project-defined. INTEROP: the M-005 transport client
// must open this protocol and write the JSON CapToken before reserving;
// reconciled by L2 at merge (N-002 task contract).
const CapProtocolID protocol.ID = "/tss/relay-cap/1.0.0"

// capSkew is the clock-skew tolerance applied to CapToken validity. The relay
// has no coord (it is coord-independent, server.md R5) and thus no
// coord.ttl.skew_tolerance; a fixed conservative value mirrors that default.
const capSkew = 30 * time.Second

// maxCapTokenBytes bounds a cap-grant stream read (anti-DoS); a CapToken is a
// few hundred bytes of JSON.
const maxCapTokenBytes = 8 << 10

// capReadTimeout bounds how long a half-open cap stream may stall.
const capReadTimeout = 10 * time.Second

// capService handles CapProtocolID streams.
type capService struct {
	anchors *trustAnchors
	az      *authz
	log     *slog.Logger
}

func (c *capService) register(h host.Host) {
	h.SetStreamHandler(CapProtocolID, c.handle)
}

// handle reads one JSON CapToken, verifies it, and on success records a grant.
// The reply is a single status byte (0 = accepted, 1 = rejected) so the member
// learns the outcome before attempting a reservation/registration.
func (c *capService) handle(s network.Stream) {
	defer func() { _ = s.Close() }()
	_ = s.SetDeadline(time.Now().Add(capReadTimeout))

	p := s.Conn().RemotePeer()
	raw, err := io.ReadAll(io.LimitReader(s, maxCapTokenBytes))
	if err != nil {
		c.reject(s, "read cap stream", err, p)
		return
	}
	var tok contract.CapToken
	if err := json.Unmarshal(raw, &tok); err != nil {
		c.reject(s, "decode cap token", err, p)
		return
	}

	// The scope is taken from the token itself; relay-reserve and
	// rendezvous-register are both accepted here, the ACL/rendezvous gate
	// later enforces the scope that matches the actual operation.
	want := tok.Scope
	if want != contract.ScopeRelayReserve && want != contract.ScopeRendezvousRegister {
		c.reject(s, "unknown cap scope", errors.New(string(want)), p)
		return
	}
	if err := c.anchors.verifyCapToken(&tok, want, time.Now(), capSkew); err != nil {
		c.reject(s, "verify cap token", err, p)
		return
	}

	c.az.record(p, &tok)
	c.log.Info("relay: capability granted",
		slog.String("peer", p.String()),
		slog.String("group", tok.GroupID),
		slog.String("scope", string(tok.Scope)))
	_, _ = s.Write([]byte{0})
}

func (c *capService) reject(s network.Stream, stage string, err error, p peer.ID) {
	c.log.Warn("relay: capability rejected",
		slog.String("peer", p.String()),
		slog.String("stage", stage),
		slog.String("err", err.Error()))
	_, _ = s.Write([]byte{1})
}
