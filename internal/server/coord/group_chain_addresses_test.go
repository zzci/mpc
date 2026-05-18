package coord

import (
	"testing"

	"github.com/zzci/mpc/internal/addr"
)

// G-001: the already-exposed GET /v1/groups/{groupId} path surfaces the
// persisted derived chain addresses (public data, not key material; no new
// endpoint). They must tie to internal/addr over the group's ecdsa_pubkey.
func TestGroupView_ExposesDerivedChainAddresses(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-addr", 2, 3)

	wantEVM, err := addr.ETHAddress(g.mainPub)
	if err != nil {
		t.Fatalf("addr.ETHAddress: %v", err)
	}
	wantTron, err := addr.TronAddress(g.mainPub)
	if err != nil {
		t.Fatalf("addr.TronAddress: %v", err)
	}

	v := h.groupView(t, g, "m0")
	if v.EVMAddress != wantEVM {
		t.Errorf("evmAddress = %q, want %q", v.EVMAddress, wantEVM)
	}
	if v.TronAddress != wantTron {
		t.Errorf("tronAddress = %q, want %q", v.TronAddress, wantTron)
	}
}
