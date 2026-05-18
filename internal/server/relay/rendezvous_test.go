package relay

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// newRendezvousService is exercised with a nil authz: put/list/namespace
// accounting never touch az (the grant gate lives in handle, covered by the
// integration test TestRendezvousAccessControl).
func testRendezvous() *rendezvousService {
	return newRendezvousService(nil, nil)
}

func TestRendezvousAddrBounds(t *testing.T) {
	rv := testRendezvous()
	now := time.Now()
	p := peer.ID("p")

	if err := rv.put("ns", p, nil, now, 0); !errors.Is(err, errNoAddrs) {
		t.Fatalf("empty addrs must be rejected, got %v", err)
	}

	tooMany := make([]string, maxRegisterAddrs+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", 1000+i)
	}
	if err := rv.put("ns", p, tooMany, now, 0); !errors.Is(err, errTooManyAddrs) {
		t.Fatalf("over-cap addrs must be rejected, got %v", err)
	}

	ok := tooMany[:maxRegisterAddrs]
	if err := rv.put("ns", p, ok, now, 0); err != nil {
		t.Fatalf("addr count at the cap must be accepted: %v", err)
	}
}

func TestRendezvousNamespaceCapPerPeer(t *testing.T) {
	rv := testRendezvous()
	now := time.Now()
	p := peer.ID("p")
	addrs := []string{"/ip4/127.0.0.1/tcp/1"}

	for i := 0; i < maxNamespacesPerPeer; i++ {
		if err := rv.put(fmt.Sprintf("ns-%d", i), p, addrs, now, 0); err != nil {
			t.Fatalf("ns %d within cap must be accepted: %v", i, err)
		}
	}

	// A new namespace beyond the cap is refused.
	if err := rv.put("ns-overflow", p, addrs, now, 0); !errors.Is(err, errTooManyNamespaces) {
		t.Fatalf("namespace beyond cap must be rejected, got %v", err)
	}

	// Refreshing an already-held namespace is not a new namespace.
	if err := rv.put("ns-0", p, addrs, now, 0); err != nil {
		t.Fatalf("re-register of a held namespace must be accepted: %v", err)
	}

	// The cap is per-peer: a different peer is unaffected.
	other := peer.ID("other")
	if err := rv.put("ns-overflow", other, addrs, now, 0); err != nil {
		t.Fatalf("a different peer must not inherit p's namespace count: %v", err)
	}

	// A successful registration is discoverable by another peer.
	got := rv.list("ns-0", other, now)
	if len(got) != 1 || got[0].PeerID != p.String() {
		t.Fatalf("expected to discover p in ns-0, got %+v", got)
	}
}
