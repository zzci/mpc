package coord

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/zzci/mpc/internal/addr"
)

// XA-001 (L1 (b) split ruling): api.md A1 GET /v1/groups/{groupId}/public — a
// physically separate external-auth route returning a minimal addresses-only
// view, isolated from the §5.1 member route so no privileged group state can
// leak. These tests pin the new route only; the existing member route
// (hGroupPublic) is covered by recovery_test / security_test /
// group_chain_addresses_test and must stay green untouched.

// extView decodes the api.md A1 slim response (snake_case, ecdsa_pubkey b64).
type extView struct {
	GroupID     string `json:"groupId"`
	ECDSAPubkey []byte `json:"ecdsa_pubkey"`
	EVMAddress  string `json:"evm_address"`
	TronAddress string `json:"tron_address"`
	ThresholdT  int    `json:"threshold_t"`
	PartiesN    int    `json:"parties_n"`
}

func TestGroupPublicExt_ReturnsSlimAddresses(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-ext", 2, 3)

	// h.do supplies X-API-Key (cfg ExternalAuth=api_key) — the external chain.
	r := h.do(t, http.MethodGet, "/v1/groups/"+g.groupID+"/public", nil, nil)
	if r.code != http.StatusOK {
		t.Fatalf("ext public: %d %s", r.code, r.text())
	}

	var v extView
	if err := json.Unmarshal(r.body, &v); err != nil {
		t.Fatalf("decode ext view: %v", err)
	}
	wantEVM, err := addr.ETHAddress(g.mainPub)
	if err != nil {
		t.Fatalf("addr.ETHAddress: %v", err)
	}
	wantTron, err := addr.TronAddress(g.mainPub)
	if err != nil {
		t.Fatalf("addr.TronAddress: %v", err)
	}
	if v.EVMAddress != wantEVM {
		t.Fatalf("evm_address = %q want %q (EIP-55)", v.EVMAddress, wantEVM)
	}
	if v.TronAddress != wantTron {
		t.Fatalf("tron_address = %q want %q (Base58Check)", v.TronAddress, wantTron)
	}
	if v.GroupID != g.groupID || v.ThresholdT != 2 || v.PartiesN != 3 {
		t.Fatalf("scalar mismatch: %+v", v)
	}
	if !bytes.Equal(v.ECDSAPubkey, g.mainPub) {
		t.Fatalf("ecdsa_pubkey b64 != stored main pubkey")
	}

	// api.md:16 — the external surface must leak no member/privileged state.
	var raw map[string]any
	if err := json.Unmarshal(r.body, &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	for _, leaked := range []string{
		"members", "groupPubkey", "epoch", "activeMembers", "degraded", "ecdsaPubkey",
	} {
		if _, ok := raw[leaked]; ok {
			t.Fatalf("ext view leaked privileged field %q: %v", leaked, raw)
		}
	}
	if len(raw) != 6 {
		t.Fatalf("ext view must be exactly 6 fields, got %d: %v", len(raw), raw)
	}
}

func TestGroupPublicExt_UnknownGroup404(t *testing.T) {
	h := newHarness(t)
	// Valid external auth (h.do sets X-API-Key); group never provisioned.
	// Unlike the memberGate §5.1 route (403 fail-closed), this external path
	// surfaces a true 404 — the purpose of the L1 (b) split.
	r := h.do(t, http.MethodGet, "/v1/groups/no-such-group/public", nil, nil)
	if r.code != http.StatusNotFound {
		t.Fatalf("unknown group: want 404 got %d %s", r.code, r.text())
	}
	var e map[string]map[string]string
	_ = json.Unmarshal(r.body, &e)
	if e["error"]["code"] != codeNotFound {
		t.Fatalf("unknown group: want NOT_FOUND got %v", e)
	}
}

func TestGroupPublicExt_LockedReturns503(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-ext-lock", 2, 3)
	if err := h.store.Relock(); err != nil {
		t.Fatalf("relock: %v", err)
	}
	r := h.do(t, http.MethodGet, "/v1/groups/"+g.groupID+"/public", nil, nil)
	if r.code != http.StatusServiceUnavailable {
		t.Fatalf("locked: want 503 got %d %s", r.code, r.text())
	}
	var e map[string]map[string]string
	_ = json.Unmarshal(r.body, &e)
	if e["error"]["code"] != codeLocked {
		t.Fatalf("locked: want LOCKED got %v", e)
	}
}

// TestGroupPublicExt_AuthRequired bypasses the api-key-injecting h.do helper to
// assert the external auth chain fail-closes without coord.external.auth.
func TestGroupPublicExt_AuthRequired(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-ext-auth", 2, 3)

	req, err := http.NewRequestWithContext(context.Background(),
		http.MethodGet, h.srv.URL+"/v1/groups/"+g.groupID+"/public", nil)
	if err != nil {
		t.Fatalf("req: %v", err)
	}
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no auth: want 401 got %d", resp.StatusCode)
	}
	var e map[string]map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&e)
	if e["error"]["code"] != codeUnauthenticated {
		t.Fatalf("no auth: want UNAUTHENTICATED got %v", e)
	}
}
