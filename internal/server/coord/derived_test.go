package coord

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// AD-6 B12 — POST /v1/groups/{groupId}/derived/register and GET
// /v1/groups/{groupId}/derived. Coverage:
//   - memberGate-only on both routes (no api-key path; api.md §F1 strict);
//   - register is lazy + idempotent on (groupId, index), 409 STATE_CONFLICT on
//     mismatch (api.md B12);
//   - 404 NOT_FOUND surfaces when the URL groupId is not a registered group
//     (repo's ErrDerivedGroupMissing mapping);
//   - list paginates with the since-cursor (unix seconds, strictly greater);
//   - LOCKED fail-closes both routes through lockGate;
//   - childPubkeyHex round-trips when supplied (33B SEC1 compressed).

const (
	d12EVM0  = "0x7E5F4552091A69125d5DfCb7b8C2659029395Bdf"
	d12Tron0 = "TMVQGm1qAQYVdetCeGRRkTWYYrLXuHK2HC"
	d12EVM1  = "0x0000000000000000000000000000000000000001"
	d12Tron1 = "TLsV52sRDL79HXGGm9yzwKibb6BeruhUzy"
)

func TestB12_RegisterAndList(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-d12", 2, 3)

	body, _ := json.Marshal(derivedRegisterBody{
		Index: 7, EVMAddress: d12EVM0, TronAddress: d12Tron0,
		ChildPubkeyHex: hex.EncodeToString(bytes.Repeat([]byte{0xAB}, 33)),
	})
	hdr := h.memberHdr(g, "m0", "B12:derived:register", g.groupID, body)
	r := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/derived/register", hdr, body)
	if r.code != http.StatusOK {
		t.Fatalf("register: %d %s", r.code, r.text())
	}
	var ack map[string]bool
	if err := json.Unmarshal(r.body, &ack); err != nil || !ack["registered"] {
		t.Fatalf("register ack: %v %s", err, r.text())
	}

	// Idempotent re-register with identical addresses (200, no row duplication).
	hdr2 := h.memberHdr(g, "m0", "B12:derived:register", g.groupID, body)
	r2 := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/derived/register", hdr2, body)
	if r2.code != http.StatusOK {
		t.Fatalf("idempotent re-register: %d %s", r2.code, r2.text())
	}

	// GET list returns exactly one row with the right shape (hex round-trip).
	listHdr := h.memberHdr(g, "m0", "B12:derived:list", g.groupID, []byte(""))
	lr := h.do(t, http.MethodGet, "/v1/groups/"+g.groupID+"/derived", listHdr, nil)
	if lr.code != http.StatusOK {
		t.Fatalf("list: %d %s", lr.code, lr.text())
	}
	var resp derivedListResponse
	if err := json.Unmarshal(lr.body, &resp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("list len = %d, want 1 (idempotent re-register must not duplicate)", len(resp.Items))
	}
	got := resp.Items[0]
	if got.Index != 7 || got.EVMAddress != d12EVM0 || got.TronAddress != d12Tron0 {
		t.Fatalf("row mismatch: %+v", got)
	}
	if got.ChildPubkeyHex != hex.EncodeToString(bytes.Repeat([]byte{0xAB}, 33)) {
		t.Fatalf("childPubkey round-trip = %q", got.ChildPubkeyHex)
	}
}

func TestB12_RegisterConflict409(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-d12-conflict", 2, 3)

	first, _ := json.Marshal(derivedRegisterBody{
		Index: 3, EVMAddress: d12EVM0, TronAddress: d12Tron0,
	})
	hdr := h.memberHdr(g, "m0", "B12:derived:register", g.groupID, first)
	if r := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/derived/register", hdr, first); r.code != http.StatusOK {
		t.Fatalf("first register: %d %s", r.code, r.text())
	}

	// Mismatched evmAddress on the same (groupId, index) -> 409 STATE_CONFLICT.
	clash, _ := json.Marshal(derivedRegisterBody{
		Index: 3, EVMAddress: d12EVM1, TronAddress: d12Tron0,
	})
	hdr2 := h.memberHdr(g, "m0", "B12:derived:register", g.groupID, clash)
	r := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/derived/register", hdr2, clash)
	if r.code != http.StatusConflict {
		t.Fatalf("conflict register: want 409, got %d %s", r.code, r.text())
	}
	var e map[string]map[string]string
	_ = json.Unmarshal(r.body, &e)
	if e["error"]["code"] != codeStateConflict {
		t.Fatalf("conflict code: want STATE_CONFLICT, got %v", e)
	}
}

func TestB12_CrossGroup403(t *testing.T) {
	// §F1 owning-member-only: a member of group A cannot register/list against
	// group B's path. memberGate fails closed at the activeMember check (the
	// caller is not in URL group) -> 403 FORBIDDEN. This makes the repo-side
	// "missing parent group" path (ErrDerivedGroupMissing -> 404) unreachable
	// from a well-behaved client surface; that sentinel mapping is still
	// covered by the coorddb-side derived_test.go for defense-in-depth.
	h := newHarness(t)
	g := h.provision(t, "grp-d12-cg", 2, 3)
	body, _ := json.Marshal(derivedRegisterBody{
		Index: 1, EVMAddress: d12EVM0, TronAddress: d12Tron0,
	})
	hdr := h.memberHdr(g, "m0", "B12:derived:register", "no-such-group", body)
	r := h.do(t, http.MethodPost, "/v1/groups/no-such-group/derived/register", hdr, body)
	if r.code != http.StatusForbidden {
		t.Fatalf("cross-group register: want 403, got %d %s", r.code, r.text())
	}
	var e map[string]map[string]string
	_ = json.Unmarshal(r.body, &e)
	if e["error"]["code"] != codeForbidden {
		t.Fatalf("cross-group code: want FORBIDDEN, got %v", e)
	}
}

func TestB12_ListSinceCursor(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-d12-since", 2, 3)
	clk := h.clk

	// Seed two rows at distinct seconds. createdAt is coord.clock.Unix(), so
	// advancing the test clock yields strictly-greater createdAt values.
	register := func(idx uint32) {
		t.Helper()
		body, _ := json.Marshal(derivedRegisterBody{
			Index: idx, EVMAddress: d12EVM0, TronAddress: d12Tron0,
		})
		hdr := h.memberHdr(g, "m0", "B12:derived:register", g.groupID, body)
		if r := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/derived/register", hdr, body); r.code != http.StatusOK {
			t.Fatalf("register %d: %d %s", idx, r.code, r.text())
		}
	}
	register(1)
	firstSec := clk.Now().Unix()
	// Move the clock past the first second so the next row's created_at is
	// strictly greater. memberAuthWindow is 5 min, so a 1-second advance
	// keeps the X-Member-Ts within window.
	clk.advance(2 * time.Second)
	register(9)

	// since=firstSec drops row 1 and keeps row 9.
	listHdr := h.memberHdr(g, "m0", "B12:derived:list", g.groupID,
		[]byte("since="+strconv.FormatInt(firstSec, 10)))
	lr := h.do(t, http.MethodGet,
		"/v1/groups/"+g.groupID+"/derived?since="+strconv.FormatInt(firstSec, 10), listHdr, nil)
	if lr.code != http.StatusOK {
		t.Fatalf("list since: %d %s", lr.code, lr.text())
	}
	var resp derivedListResponse
	if err := json.Unmarshal(lr.body, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Index != 9 {
		t.Fatalf("since=%d: %+v want only index 9", firstSec, resp.Items)
	}
}

func TestB12_NoAPIKeyPath_AuthRequired(t *testing.T) {
	// §F1 strict / §7.bis.3: B12 is memberGate-only. Without X-Member-*
	// headers the request must fail closed at memberGate (401/403), never
	// succeed via the external auth chain.
	h := newHarness(t)
	g := h.provision(t, "grp-d12-noauth", 2, 3)

	body, _ := json.Marshal(derivedRegisterBody{
		Index: 1, EVMAddress: d12EVM0, TronAddress: d12Tron0,
	})
	// h.do automatically sets X-API-Key (external chain) but we want to
	// assert memberGate is in front, so build a bare request without X-Member-*.
	req, err := http.NewRequestWithContext(context.Background(),
		http.MethodPost, h.srv.URL+"/v1/groups/"+g.groupID+"/derived/register",
		bytes.NewReader(body))
	if err != nil {
		t.Fatalf("req: %v", err)
	}
	req.Header.Set("X-API-Key", "secret-key") // still rejected: memberGate is the auth chain here
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no member auth: want 401, got %d", resp.StatusCode)
	}

	// GET path likewise rejects the unauthenticated caller.
	greq, err := http.NewRequestWithContext(context.Background(),
		http.MethodGet, h.srv.URL+"/v1/groups/"+g.groupID+"/derived", nil)
	if err != nil {
		t.Fatalf("greq: %v", err)
	}
	greq.Header.Set("X-API-Key", "secret-key")
	gresp, err := h.srv.Client().Do(greq)
	if err != nil {
		t.Fatalf("gdo: %v", err)
	}
	_ = gresp.Body.Close()
	if gresp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no member auth (GET): want 401, got %d", gresp.StatusCode)
	}
}

func TestB12_LockedReturns503(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-d12-lock", 2, 3)
	if err := h.store.Relock(); err != nil {
		t.Fatalf("relock: %v", err)
	}

	body, _ := json.Marshal(derivedRegisterBody{
		Index: 1, EVMAddress: d12EVM0, TronAddress: d12Tron0,
	})
	hdr := h.memberHdr(g, "m0", "B12:derived:register", g.groupID, body)
	r := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/derived/register", hdr, body)
	if r.code != http.StatusServiceUnavailable {
		t.Fatalf("locked register: want 503, got %d %s", r.code, r.text())
	}
	listHdr := h.memberHdr(g, "m0", "B12:derived:list", g.groupID, []byte(""))
	lr := h.do(t, http.MethodGet, "/v1/groups/"+g.groupID+"/derived", listHdr, nil)
	if lr.code != http.StatusServiceUnavailable {
		t.Fatalf("locked list: want 503, got %d %s", lr.code, lr.text())
	}
}

func TestB12_InvalidBodyShape(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-d12-bad", 2, 3)

	// Missing evmAddress.
	body, _ := json.Marshal(derivedRegisterBody{Index: 1, TronAddress: d12Tron0})
	hdr := h.memberHdr(g, "m0", "B12:derived:register", g.groupID, body)
	r := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/derived/register", hdr, body)
	if r.code != http.StatusBadRequest {
		t.Fatalf("missing evm: want 400, got %d %s", r.code, r.text())
	}

	// Non-hex childPubkeyHex.
	body2, _ := json.Marshal(derivedRegisterBody{
		Index: 2, EVMAddress: d12EVM0, TronAddress: d12Tron0, ChildPubkeyHex: "ZZ",
	})
	hdr2 := h.memberHdr(g, "m0", "B12:derived:register", g.groupID, body2)
	r2 := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/derived/register", hdr2, body2)
	if r2.code != http.StatusBadRequest {
		t.Fatalf("non-hex childPubkey: want 400, got %d %s", r2.code, r2.text())
	}

	// Wrong-length childPubkeyHex (not 33B).
	body3, _ := json.Marshal(derivedRegisterBody{
		Index: 3, EVMAddress: d12EVM0, TronAddress: d12Tron0,
		ChildPubkeyHex: hex.EncodeToString([]byte{1, 2, 3}),
	})
	hdr3 := h.memberHdr(g, "m0", "B12:derived:register", g.groupID, body3)
	r3 := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/derived/register", hdr3, body3)
	if r3.code != http.StatusBadRequest {
		t.Fatalf("short childPubkey: want 400, got %d %s", r3.code, r3.text())
	}
}
