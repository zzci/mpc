package relay

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/royqta/mcp-wallet/internal/contract"
)

// RendezvousProtocolID is the group-namespace discovery protocol (server.md
// R3): members register {peerID, multiaddrs} under the group namespace
// (= base32(HMAC(groupSecret,"tss-group")), computed client-side and opaque to
// the relay) and discover peers by namespace. A minimal in-memory,
// TTL-expired, stateless-across-restart service is used rather than the heavy
// unmaintained go-libp2p-rendezvous module (which pulls a conflicting libp2p
// version and a SQLite server): the relay is stateless and horizontally
// replicable (server.md R5), clients fail over, so durable rendezvous state is
// neither required nor desirable here. Registration is gated by a
// rendezvous-register CapToken (server.md R4); discovery requires any live
// grant so only authorized members of a deployment can query. INTEROP: the
// M-005 transport client speaks this protocol/schema; reconciled by L2 at
// merge (N-002 task contract).
const RendezvousProtocolID protocol.ID = "/tss/rendezvous/1.0.0"

// maxRendezvousBytes bounds a rendezvous request read (anti-DoS).
const maxRendezvousBytes = 16 << 10

// rendezvousReadTimeout bounds a stalled rendezvous stream.
const rendezvousReadTimeout = 10 * time.Second

// defaultRegisterTTL caps how long a registration is served absent an explicit
// (and smaller) client TTL; short by design so stale members age out and the
// relay stays effectively stateless.
const defaultRegisterTTL = 2 * time.Minute

// H-003 hardening (server.md R4 layer 3 — anti-DoS, orthogonal to authz):
// even an authorized member (a valid rendezvous-register grant) must not be
// able to exhaust relay memory by registering an unbounded number of
// multiaddrs or fanning out across unbounded namespaces (security.md §5
// "单一恶意/被控成员"). A wallet member advertises a handful of transport
// addresses and belongs to very few groups, so these caps are generous in
// practice while bounding the registry. The 16 KiB request frame
// (maxRendezvousBytes) and the short TTL already bound a single request and
// staleness; these add the per-peer breadth bound the registry lacked.
const (
	maxRegisterAddrs     = 16
	maxNamespacesPerPeer = 8
)

// Registration rejection reasons (logged with detail; the wire reply stays
// generic per security.md §7 "relay 仅计数/拒绝原因,不记 peer 间载荷").
var (
	errNoAddrs           = errors.New("rendezvous: registration advertises no addrs")
	errTooManyAddrs      = errors.New("rendezvous: registration exceeds addr cap")
	errTooManyNamespaces = errors.New("rendezvous: peer exceeds namespace cap")
)

// rendezvousOp is the request discriminator.
type rendezvousOp string

const (
	opRegister rendezvousOp = "register"
	opDiscover rendezvousOp = "discover"
)

// rendezvousRequest is the client→relay message.
type rendezvousRequest struct {
	Op        rendezvousOp `json:"op"`
	Namespace string       `json:"namespace"`
	Addrs     []string     `json:"addrs,omitempty"` // register: self multiaddrs
	TTLMillis int64        `json:"ttlMillis,omitempty"`
}

// rendezvousPeer is one discovered registration.
type rendezvousPeer struct {
	PeerID string   `json:"peerId"`
	Addrs  []string `json:"addrs"`
}

// rendezvousResponse is the relay→client message.
type rendezvousResponse struct {
	OK    bool             `json:"ok"`
	Error string           `json:"error,omitempty"`
	Peers []rendezvousPeer `json:"peers,omitempty"`
}

type registration struct {
	addrs   []string
	expires time.Time
}

// rendezvousService is the in-memory namespace registry.
type rendezvousService struct {
	az  *authz
	log *slog.Logger

	mu   sync.Mutex
	regs map[string]map[peer.ID]registration // namespace -> peer -> registration
}

func newRendezvousService(az *authz, log *slog.Logger) *rendezvousService {
	return &rendezvousService{az: az, log: log, regs: make(map[string]map[peer.ID]registration)}
}

func (rv *rendezvousService) register(h host.Host) {
	h.SetStreamHandler(RendezvousProtocolID, rv.handle)
}

func (rv *rendezvousService) handle(s network.Stream) {
	defer func() { _ = s.Close() }()
	_ = s.SetDeadline(time.Now().Add(rendezvousReadTimeout))
	p := s.Conn().RemotePeer()

	var req rendezvousRequest
	if err := json.NewDecoder(io.LimitReader(s, maxRendezvousBytes)).Decode(&req); err != nil {
		rv.respond(s, rendezvousResponse{Error: "bad request"})
		return
	}
	if req.Namespace == "" {
		rv.respond(s, rendezvousResponse{Error: "empty namespace"})
		return
	}
	now := time.Now()

	switch req.Op {
	case opRegister:
		if !rv.az.hasScope(p, contract.ScopeRendezvousRegister, now) {
			rv.log.Warn("relay: rendezvous register denied (no grant)",
				slog.String("peer", p.String()))
			rv.respond(s, rendezvousResponse{Error: "unauthorized"})
			return
		}
		if err := rv.put(req.Namespace, p, req.Addrs, now, req.TTLMillis); err != nil {
			rv.log.Warn("relay: rendezvous register rejected",
				slog.String("peer", p.String()), slog.String("err", err.Error()))
			rv.respond(s, rendezvousResponse{Error: "register rejected"})
			return
		}
		rv.respond(s, rendezvousResponse{OK: true})
	case opDiscover:
		if !rv.anyGrant(p, now) {
			rv.log.Warn("relay: rendezvous discover denied (no grant)",
				slog.String("peer", p.String()))
			rv.respond(s, rendezvousResponse{Error: "unauthorized"})
			return
		}
		rv.respond(s, rendezvousResponse{OK: true, Peers: rv.list(req.Namespace, p, now)})
	default:
		rv.respond(s, rendezvousResponse{Error: "unknown op"})
	}
}

// anyGrant reports whether p holds a live grant of either scope.
func (rv *rendezvousService) anyGrant(p peer.ID, now time.Time) bool {
	return rv.az.hasScope(p, contract.ScopeRendezvousRegister, now) ||
		rv.az.hasScope(p, contract.ScopeRelayReserve, now)
}

// put validates and stores p's registration in ns. It enforces the H-003
// anti-DoS bounds before mutating state: an empty/oversized addr set is
// refused, and a peer may not occupy more than maxNamespacesPerPeer distinct
// namespaces (re-registering an already-held namespace never counts as new,
// so refreshes are unaffected). The namespace-count check and the insert are
// done under one lock so the bound holds against concurrent registrations.
func (rv *rendezvousService) put(ns string, p peer.ID, addrs []string, now time.Time, ttlMS int64) error {
	if len(addrs) == 0 {
		return errNoAddrs
	}
	if len(addrs) > maxRegisterAddrs {
		return errTooManyAddrs
	}
	ttl := defaultRegisterTTL
	if ttlMS > 0 && time.Duration(ttlMS)*time.Millisecond < ttl {
		ttl = time.Duration(ttlMS) * time.Millisecond
	}
	rv.mu.Lock()
	defer rv.mu.Unlock()
	if _, held := rv.regs[ns][p]; !held && rv.namespaceCountLocked(p) >= maxNamespacesPerPeer {
		return errTooManyNamespaces
	}
	m := rv.regs[ns]
	if m == nil {
		m = make(map[peer.ID]registration)
		rv.regs[ns] = m
	}
	m[p] = registration{addrs: addrs, expires: now.Add(ttl)}
	return nil
}

// namespaceCountLocked counts the distinct namespaces p currently holds a
// registration in. Caller holds rv.mu.
func (rv *rendezvousService) namespaceCountLocked(p peer.ID) int {
	n := 0
	for _, m := range rv.regs {
		if _, ok := m[p]; ok {
			n++
		}
	}
	return n
}

// list returns live registrations in ns excluding the caller, expiring stale
// entries lazily.
func (rv *rendezvousService) list(ns string, self peer.ID, now time.Time) []rendezvousPeer {
	rv.mu.Lock()
	defer rv.mu.Unlock()
	m := rv.regs[ns]
	if m == nil {
		return nil
	}
	out := make([]rendezvousPeer, 0, len(m))
	for id, r := range m {
		if now.After(r.expires) {
			delete(m, id)
			continue
		}
		if id == self {
			continue
		}
		out = append(out, rendezvousPeer{PeerID: id.String(), Addrs: r.addrs})
	}
	if len(m) == 0 {
		delete(rv.regs, ns)
	}
	return out
}

// drop removes all registrations for p across namespaces (host Disconnected).
func (rv *rendezvousService) drop(p peer.ID) {
	rv.mu.Lock()
	defer rv.mu.Unlock()
	for ns, m := range rv.regs {
		delete(m, p)
		if len(m) == 0 {
			delete(rv.regs, ns)
		}
	}
}

func (rv *rendezvousService) respond(s network.Stream, resp rendezvousResponse) {
	_ = json.NewEncoder(s).Encode(resp)
}
