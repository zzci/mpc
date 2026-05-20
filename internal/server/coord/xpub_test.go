package coord

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// AD-4 B8 GET /v1/groups/{groupId}/xpub: owning-member-only release of the HD
// extended public key (Q_master, chaincode) per docs/design/mcp/address-
// derivation.md §7. F1 hard constraint: never expose chaincode on the A
// surface; F5: legacy groups (chaincode NULL) surface 409 LEGACY_NO_HD.

// setChaincode injects a 32-byte chaincode into an already-provisioned group's
// row by going through Store.WithTx (the only public DB seam). The post-DKG
// commit-reveal (AD-2) is the production writer; for B8 endpoint tests we only
// need the column populated, so a direct UPDATE keeps the test focused on the
// endpoint surface.
func (h *harness) setChaincode(t *testing.T, groupID string, chaincode []byte) {
	t.Helper()
	if err := h.store.WithTx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(),
			`UPDATE groups SET chaincode = ? WHERE group_id = ?`,
			chaincode, groupID)
		return err
	}); err != nil {
		t.Fatalf("inject chaincode: %v", err)
	}
}

// xpubGet runs an authenticated B8 GET against the harness server as the
// given member, returning the raw response wrapped in hresp.
func (h *harness) xpubGet(t *testing.T, g *testGroup, asMember string) *hresp {
	t.Helper()
	hdr := h.memberHdr(g, asMember, "B8:xpub", g.groupID, []byte(""))
	return h.do(t, http.MethodGet, "/v1/groups/"+g.groupID+"/xpub", hdr, nil)
}

func TestB8Xpub_ReturnsXpubForOwningMember(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-xpub", 2, 3)

	wantCC := bytes.Repeat([]byte{0x5a}, 32)
	h.setChaincode(t, g.groupID, wantCC)

	r := h.xpubGet(t, g, "m0")
	if r.code != http.StatusOK {
		t.Fatalf("xpub: %d %s", r.code, r.text())
	}
	var v struct {
		ECDSAPubkeyHex string `json:"ecdsaPubkeyHex"`
		ChaincodeHex   string `json:"chaincodeHex"`
	}
	if err := json.Unmarshal(r.body, &v); err != nil {
		t.Fatalf("decode xpub: %v", err)
	}
	pub, err := hex.DecodeString(v.ECDSAPubkeyHex)
	if err != nil {
		t.Fatalf("ecdsaPubkeyHex not hex: %v", err)
	}
	if !bytes.Equal(pub, g.mainPub) {
		t.Fatalf("ecdsaPubkey mismatch: got %x want %x", pub, g.mainPub)
	}
	cc, err := hex.DecodeString(v.ChaincodeHex)
	if err != nil {
		t.Fatalf("chaincodeHex not hex: %v", err)
	}
	if !bytes.Equal(cc, wantCC) {
		t.Fatalf("chaincode mismatch: got %x want %x", cc, wantCC)
	}

	// F1 belt-and-braces: the wire body must be exactly 2 keys — no leak of
	// member / groupPubkey / epoch / privileged group state.
	var raw map[string]any
	if err := json.Unmarshal(r.body, &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if len(raw) != 2 {
		t.Fatalf("B8 view must be exactly 2 fields, got %d: %v", len(raw), raw)
	}
	for _, leaked := range []string{
		"members", "memberId", "groupPubkey", "epoch", "activeMembers",
		"degraded", "evmAddress", "tronAddress", "thresholdT", "partiesN",
	} {
		if _, ok := raw[leaked]; ok {
			t.Fatalf("B8 view leaked %q: %v", leaked, raw)
		}
	}
}

func TestB8Xpub_LegacyGroupReturns409(t *testing.T) {
	h := newHarness(t)
	// Provision leaves chaincode NULL (legacy / F5 path); B8 must 409.
	g := h.provision(t, "grp-legacy-xpub", 2, 3)

	r := h.xpubGet(t, g, "m0")
	if r.code != http.StatusConflict {
		t.Fatalf("legacy xpub: want 409 got %d %s", r.code, r.text())
	}
	var e map[string]map[string]string
	_ = json.Unmarshal(r.body, &e)
	if e["error"]["code"] != codeLegacyNoHD {
		t.Fatalf("legacy xpub: want code %q got %v", codeLegacyNoHD, e)
	}
	if !strings.Contains(e["error"]["message"], "predates HD") {
		t.Fatalf("legacy xpub: message does not mention predates HD: %v", e)
	}
}

func TestB8Xpub_UnknownMemberReturns403(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-x-unknown", 2, 3)
	h.setChaincode(t, g.groupID, bytes.Repeat([]byte{0xab}, 32))

	// Sign as "mX" — not in this group; memberGate must fail-close with 403.
	// We still sign with a valid member key (m0) but assert membership against
	// the bogus id, mirroring the active-member lookup branch in memberGate.
	hdr := h.memberHdr(g, "m0", "B8:xpub", g.groupID, []byte(""))
	hdr["X-Member-Id"] = "mX"
	r := h.do(t, http.MethodGet, "/v1/groups/"+g.groupID+"/xpub", hdr, nil)
	if r.code != http.StatusForbidden && r.code != http.StatusUnauthorized {
		t.Fatalf("non-member xpub: want 401/403 got %d %s", r.code, r.text())
	}
}

func TestB8Xpub_NoMemberSigReturns401(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-x-noauth", 2, 3)
	h.setChaincode(t, g.groupID, bytes.Repeat([]byte{0xcd}, 32))

	// Omit X-Member-* headers entirely. memberGate must fail-close before
	// touching the chaincode column.
	r := h.do(t, http.MethodGet, "/v1/groups/"+g.groupID+"/xpub",
		map[string]string{"Content-Type": "application/json"}, nil)
	if r.code != http.StatusUnauthorized {
		t.Fatalf("missing auth xpub: want 401 got %d %s", r.code, r.text())
	}
}

func TestB8Xpub_LockedReturns503(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-x-lock", 2, 3)
	h.setChaincode(t, g.groupID, bytes.Repeat([]byte{0x99}, 32))
	if err := h.store.Relock(); err != nil {
		t.Fatalf("relock: %v", err)
	}
	r := h.xpubGet(t, g, "m0")
	if r.code != http.StatusServiceUnavailable {
		t.Fatalf("locked xpub: want 503 got %d %s", r.code, r.text())
	}
	var e map[string]map[string]string
	_ = json.Unmarshal(r.body, &e)
	if e["error"]["code"] != codeLocked {
		t.Fatalf("locked xpub: want LOCKED got %v", e)
	}
}

func TestB8Xpub_CrossGroupReturns403(t *testing.T) {
	h := newHarness(t)
	gA := h.provision(t, "grp-A-xpub", 2, 3)
	gB := h.provision(t, "grp-B-xpub", 2, 3)
	h.setChaincode(t, gA.groupID, bytes.Repeat([]byte{0xee}, 32))
	h.setChaincode(t, gB.groupID, bytes.Repeat([]byte{0xff}, 32))

	// m0-of-A tries to read B's xpub. memberGate isolates by groupId.
	hdr := h.memberHdr(gA, "m0", "B8:xpub", gB.groupID, []byte(""))
	r := h.do(t, http.MethodGet, "/v1/groups/"+gB.groupID+"/xpub", hdr, nil)
	if r.code != http.StatusForbidden && r.code != http.StatusUnauthorized {
		t.Fatalf("cross-group xpub: want 401/403 got %d %s", r.code, r.text())
	}
}
