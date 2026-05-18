package relay

import (
	"errors"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zzci/mpc/internal/contract"
)

func grantTok(group, nonce string, scope contract.CapScope, notAfter int64) *contract.CapToken {
	return &contract.CapToken{
		GroupID: group, MemberID: "m", Scope: scope,
		Nonce: []byte(nonce), NotAfter: notAfter,
	}
}

func TestAuthzReservationQuota(t *testing.T) {
	now := time.Now()
	future := now.Add(time.Hour).UnixMilli()
	az := newAuthz(2, 3) // per-token=2, per-group=3

	// Two peers share one token (same nonce) → per-token cap = 2.
	p1, p2, p3 := peer.ID("p1"), peer.ID("p2"), peer.ID("p3")
	az.record(p1, grantTok("g", "tokA", contract.ScopeRelayReserve, future))
	az.record(p2, grantTok("g", "tokA", contract.ScopeRelayReserve, future))
	az.record(p3, grantTok("g", "tokA", contract.ScopeRelayReserve, future))

	if err := az.allowReserve(p1, now); err != nil {
		t.Fatalf("p1 reserve: %v", err)
	}
	if err := az.allowReserve(p1, now); err != nil {
		t.Fatalf("refresh must be idempotent: %v", err)
	}
	if err := az.allowReserve(p2, now); err != nil {
		t.Fatalf("p2 reserve (token count 2): %v", err)
	}
	if err := az.allowReserve(p3, now); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("p3 must hit per-token quota, got %v", err)
	}

	// Release p1 frees a token slot; p3 then admitted (token=2 again, group ok).
	az.release(p1)
	if err := az.allowReserve(p3, now); err != nil {
		t.Fatalf("p3 after release: %v", err)
	}
}

func TestAuthzPerGroupQuota(t *testing.T) {
	now := time.Now()
	future := now.Add(time.Hour).UnixMilli()
	az := newAuthz(0, 2) // per-token unlimited, per-group=2

	p1, p2, p3 := peer.ID("p1"), peer.ID("p2"), peer.ID("p3")
	az.record(p1, grantTok("g", "t1", contract.ScopeRelayReserve, future))
	az.record(p2, grantTok("g", "t2", contract.ScopeRelayReserve, future))
	az.record(p3, grantTok("g", "t3", contract.ScopeRelayReserve, future))

	if err := az.allowReserve(p1, now); err != nil {
		t.Fatal(err)
	}
	if err := az.allowReserve(p2, now); err != nil {
		t.Fatal(err)
	}
	if err := az.allowReserve(p3, now); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("p3 must hit per-group quota, got %v", err)
	}
}

func TestAuthzScopeAndExpiry(t *testing.T) {
	now := time.Now()
	az := newAuthz(4, 8)

	p := peer.ID("p")
	az.record(p, grantTok("g", "n", contract.ScopeRendezvousRegister, now.Add(time.Hour).UnixMilli()))

	// relay-reserve scope must not be satisfied by a rendezvous-register grant.
	if err := az.allowReserve(p, now); !errors.Is(err, ErrNoGrant) {
		t.Fatalf("wrong scope must be ErrNoGrant, got %v", err)
	}
	if !az.hasScope(p, contract.ScopeRendezvousRegister, now) {
		t.Fatal("rendezvous-register scope expected")
	}

	// Expired grant is rejected and purged.
	pe := peer.ID("pe")
	az.record(pe, grantTok("g", "n2", contract.ScopeRelayReserve, now.Add(-time.Second).UnixMilli()))
	if err := az.allowReserve(pe, now); !errors.Is(err, ErrNoGrant) {
		t.Fatalf("expired grant must be ErrNoGrant, got %v", err)
	}
	if az.hasScope(pe, contract.ScopeRelayReserve, now) {
		t.Fatal("expired grant must not satisfy hasScope")
	}
}
