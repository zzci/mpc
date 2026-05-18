package relay

import (
	"log/slog"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/zzci/mpc/internal/contract"
)

// relayACL implements circuit-relay v2's relay.ACLFilter (server.md R4): a
// reservation or relayed connection is admitted only if the requesting peer
// presented a valid relay-reserve CapToken (capservice.go) and is within the
// per-token / per-group reservation quota (authz.go). The ACL sees only the
// libp2p-authenticated peer.ID, which is the member's unforgeable public key
// (server.md R2), so binding authorization to peer.ID is sound.
type relayACL struct {
	az  *authz
	log *slog.Logger
}

// AllowReserve gates circuit-relay reservations: live relay-reserve grant plus
// quota headroom (idempotent across reservation refreshes).
func (r *relayACL) AllowReserve(p peer.ID, _ ma.Multiaddr) bool {
	if err := r.az.allowReserve(p, time.Now()); err != nil {
		r.log.Warn("relay: reservation denied",
			slog.String("peer", p.String()), slog.String("err", err.Error()))
		return false
	}
	return true
}

// AllowConnect gates a relayed connection: the source must still hold a live
// relay-reserve grant. Per-circuit byte/duration caps are enforced by
// circuitv2 Resources/RelayLimit (relay.go). H-003 reviewed the
// connection-count dimension: circuit-relay v2 has no per-circuit release
// callback, so the per-token/per-group reservation cap (authz.go) together
// with circuitv2 MaxCircuits/MaxReservations is the accepted relayed-
// connection bound; no leak-prone per-circuit counter is added here.
func (r *relayACL) AllowConnect(src peer.ID, _ ma.Multiaddr, dest peer.ID) bool {
	if !r.az.hasScope(src, contract.ScopeRelayReserve, time.Now()) {
		r.log.Warn("relay: connect denied",
			slog.String("src", src.String()), slog.String("dest", dest.String()))
		return false
	}
	return true
}
