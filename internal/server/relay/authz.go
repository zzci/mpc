package relay

import (
	"errors"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/royqta/mcp-wallet/internal/contract"
)

// authz is the relay-side authorization table (server.md R4). A peer becomes
// authorized only by presenting a valid CapToken over the cap-grant stream
// (capservice.go); the circuit-relay v2 ACL (relayacl.go) and the rendezvous
// service (rendezvous.go) then consult this table by the libp2p-authenticated
// peer.ID (= the member's public key, unforgeable per server.md R2).
//
// Quota model (server.md R4 layer 3 — per-token / per-group caps, anti-DoS,
// orthogonal to authorization): a token is identified by its CapToken.Nonce; a
// group by CapToken.GroupID. The relay caps the number of concurrently active
// reservations per token and per group. circuit-relay v2 gives no reservation
// release callback, so "active" is bounded by peer connectivity: a grant is
// counted on its first accepted reservation and released on the host's
// Disconnected notification (relay.go wires the notifiee). One member peer
// makes one reservation (libp2p MaxReservationsPerPeer == 1), so connected
// granted peers per token/group accurately bounds active reservations.
// The other two R4-layer-3 dimensions — relayed-connection count and
// forwarding bandwidth — are enforced by circuitv2 Resources (MaxReservations
// global, MaxCircuits per peer) plus the per-circuit RelayLimit Data/Duration
// budget derived from relay.limits.bandwidth_per_conn (limits.go). H-003
// reviewed this split and deliberately does NOT add a per-token live-circuit
// counter: circuit-relay v2 exposes no per-circuit close callback, so such a
// counter would be leak-prone and imprecise, while the reservation cap above
// already transitively bounds relayed-circuit endpoints. H-003's added
// member-driven anti-DoS bound is on the rendezvous registry (rendezvous.go),
// the one breadth axis the quota model did not yet cover.
type authz struct {
	perToken int // reservation_per_token
	perGroup int // reservation_per_group

	mu         sync.Mutex
	grants     map[peer.ID]*grant
	tokenCount map[string]int // CapToken.Nonce (hex-free: raw string) -> active reservations
	groupCount map[string]int // CapToken.GroupID -> active reservations
}

// grant is a verified CapToken bound to the presenting peer.
type grant struct {
	groupID  string
	memberID string
	nonce    string            // CapToken.Nonce as map key
	scope    contract.CapScope // scope this grant authorizes
	notAfter int64             // unix ms; grant is dead past this
	reserved bool              // counted toward token/group quota
}

// ErrNoGrant is returned by checks when the peer holds no live grant for the
// requested scope.
var ErrNoGrant = errors.New("relay: peer holds no valid capability grant")

// ErrQuotaExceeded is returned when admitting a reservation would breach the
// per-token or per-group cap (server.md R4 layer 3).
var ErrQuotaExceeded = errors.New("relay: reservation quota exceeded")

func newAuthz(perToken, perGroup int) *authz {
	return &authz{
		perToken:   perToken,
		perGroup:   perGroup,
		grants:     make(map[peer.ID]*grant),
		tokenCount: make(map[string]int),
		groupCount: make(map[string]int),
	}
}

// record stores a verified token's grant for p. A later token from the same
// peer (e.g. a rendezvous-register grant after a relay-reserve grant) replaces
// the prior grant; any quota the prior grant held is released first so counts
// never leak across re-presentation.
func (a *authz) record(p peer.ID, t *contract.CapToken) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.releaseLocked(p)
	a.grants[p] = &grant{
		groupID:  t.GroupID,
		memberID: t.MemberID,
		nonce:    string(t.Nonce),
		scope:    t.Scope,
		notAfter: t.NotAfter,
	}
}

// allowReserve admits a circuit-relay reservation from p: a live
// relay-reserve grant must exist and, if not yet counted, the per-token and
// per-group caps must have headroom. Idempotent across reservation refreshes
// (a grant already counted is admitted without re-incrementing).
func (a *authz) allowReserve(p peer.ID, now time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	g, ok := a.grants[p]
	if !ok || g.scope != contract.ScopeRelayReserve {
		return ErrNoGrant
	}
	if now.UnixMilli() > g.notAfter {
		a.releaseLocked(p)
		return ErrNoGrant
	}
	if g.reserved {
		return nil
	}
	if a.perToken > 0 && a.tokenCount[g.nonce] >= a.perToken {
		return ErrQuotaExceeded
	}
	if a.perGroup > 0 && a.groupCount[g.groupID] >= a.perGroup {
		return ErrQuotaExceeded
	}
	g.reserved = true
	a.tokenCount[g.nonce]++
	a.groupCount[g.groupID]++
	return nil
}

// hasScope reports whether p holds a live grant for scope (used by the
// rendezvous service and the connect ACL).
func (a *authz) hasScope(p peer.ID, scope contract.CapScope, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	g, ok := a.grants[p]
	if !ok || g.scope != scope {
		return false
	}
	if now.UnixMilli() > g.notAfter {
		a.releaseLocked(p)
		return false
	}
	return true
}

// release drops p's grant and any quota it held (host Disconnected notifiee).
func (a *authz) release(p peer.ID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.releaseLocked(p)
}

func (a *authz) releaseLocked(p peer.ID) {
	g, ok := a.grants[p]
	if !ok {
		return
	}
	if g.reserved {
		if a.tokenCount[g.nonce]--; a.tokenCount[g.nonce] <= 0 {
			delete(a.tokenCount, g.nonce)
		}
		if a.groupCount[g.groupID]--; a.groupCount[g.groupID] <= 0 {
			delete(a.groupCount, g.groupID)
		}
	}
	delete(a.grants, p)
}
