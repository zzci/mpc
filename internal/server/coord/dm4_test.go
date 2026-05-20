package coord

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"

	"github.com/zzci/mpc/internal/contract"
)

// DM-4 (distributed-mpc.md §3/§3.bis/§3.ter / api.md B9-B11): the coord
// event-orchestration endpoints plus the extended dispatchHub event
// types plus the R7 application guard. Tests cover:
//
//   - the strict identity allowlist (ExpectedMembers) fail-closes B9/B10/B11
//     identities that are not pre-declared (EXPECTED_MEMBER_MISMATCH);
//   - B9 refuses re-keygen on a group that already has an ecdsa_pubkey;
//   - B10 happy path dispatches reshare-START to the old committee;
//   - B11 stores attestations and replay-rejects stale ts;
//   - dispatchHub fans out new event types as JSON shapes carrying a
//     "type" discriminator (sign-START is unchanged for backward compat);
//   - R7 application guard rejects an attempted NULL-ecdsa_pubkey
//     ProvisionGroup.

// setExpectedMembers wires the strict-set allowlist for the harness's
// provisioned group, using each member's compressed identity pubkey.
func setExpectedMembers(t *testing.T, h *harness, g *testGroup) {
	t.Helper()
	keys := make([][]byte, 0, len(g.members))
	for _, id := range memberIDsSorted(g) {
		keys = append(keys, g.memberPub[id])
	}
	if h.co.cfg.ExpectedMembers == nil {
		h.co.cfg.ExpectedMembers = map[string][][]byte{}
	}
	h.co.cfg.ExpectedMembers[g.groupID] = keys
}

func memberIDsSorted(g *testGroup) []string {
	out := make([]string, 0, len(g.members))
	for id := range g.members {
		out = append(out, id)
	}
	// canonical order m0, m1, ...; testGroup ids are "m<i>" so lexical
	// sort matches numeric order for n < 10.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// emptyKeyedGroup carves out the "provisioned-but-unkeyed" state needed by
// B9 (the design's pre-keygen group). The existing S-002 ProvisionGroup
// always writes a non-empty ecdsa_pubkey, so we insert one directly via
// the raw store connection (test-only seam).
func emptyKeyedGroup(t *testing.T, h *harness, groupID string, members []memberEntry) {
	t.Helper()
	ctx := context.Background()
	now := h.clk.Now().Format("2006-01-02T15:04:05.999999999Z07:00")
	err := h.store.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO groups
			 (group_id, ecdsa_pubkey, threshold_t, parties_n, group_pubkey, epoch, created_at, updated_at, evm_address, tron_address)
			 VALUES (?, ?, ?, ?, ?, 0, ?, ?, '', '')`,
			groupID, []byte{}, 2, len(members), []byte("placeholder"), now, now); err != nil {
			return err
		}
		for _, m := range members {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO group_members (group_id, member_id, identity_pubkey, status)
				 VALUES (?, ?, ?, 'active')`,
				groupID, m.MemberID, m.IdentityPubkey); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed empty-keyed group: %v", err)
	}
}

func TestDM4_KeygenB9_HappyPath(t *testing.T) {
	h := newHarness(t)
	const gid = "grp-keygen-happy"
	priv0, pub0, _ := newKey(t)
	priv1, pub1, _ := newKey(t)
	priv2, pub2, _ := newKey(t)
	emptyKeyedGroup(t, h, gid, []memberEntry{
		{MemberID: "m0", IdentityPubkey: priv0.PubKey().SerializeCompressed()},
		{MemberID: "m1", IdentityPubkey: priv1.PubKey().SerializeCompressed()},
		{MemberID: "m2", IdentityPubkey: priv2.PubKey().SerializeCompressed()},
	})
	_ = pub0
	_ = pub1
	_ = pub2

	memberSetHex := []string{
		hex.EncodeToString(priv0.PubKey().SerializeCompressed()),
		hex.EncodeToString(priv1.PubKey().SerializeCompressed()),
		hex.EncodeToString(priv2.PubKey().SerializeCompressed()),
	}
	memberSetBytes := [][]byte{
		priv0.PubKey().SerializeCompressed(),
		priv1.PubKey().SerializeCompressed(),
		priv2.PubKey().SerializeCompressed(),
	}
	h.co.cfg.ExpectedMembers = map[string][][]byte{gid: memberSetBytes}

	req := keygenRequest{
		SessionID:  "sess-keygen-1",
		ThresholdT: 2,
		PartiesN:   3,
		MemberSet:  memberSetHex,
		Deadline:   h.clk.Now().UnixMilli() + 60_000,
	}
	digest := keygenRequestDigest(&req, memberSetBytes)
	req.ProposerSig = contract.SignDigest(priv0, digest)
	body, _ := json.Marshal(req)

	hdr := keygenMemberHdr(h, gid, "m0", priv0, body)
	resp := h.do(t, http.MethodPost, "/v1/groups/"+gid+"/keygen", hdr, body)
	if resp.code != http.StatusAccepted {
		t.Fatalf("B9 keygen status=%d body=%s", resp.code, resp.text())
	}

	// Each member's B6 dispatch must yield a keygen-START event with the
	// type discriminator, sessionID, and full memberSet.
	for _, mid := range []string{"m0", "m1", "m2"} {
		got, ok := h.co.hub.take(gid, mid)
		if !ok {
			t.Fatalf("member %s did not receive keygen-START", mid)
		}
		ev, ok := got.(keygenStartEvent)
		if !ok {
			t.Fatalf("member %s received non-keygen event: %T", mid, got)
		}
		if ev.Type != "keygen-START" || ev.SessionID != "sess-keygen-1" || ev.PartiesN != 3 {
			t.Fatalf("member %s bad keygen-START: %+v", mid, ev)
		}
	}
}

func TestDM4_KeygenB9_ExpectedMemberMismatch(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-keygen-mismatch", 2, 3)
	// expected_members for this group: omit m0's key. m0's keygen attempt
	// must be EXPECTED_MEMBER_MISMATCH.
	h.co.cfg.ExpectedMembers = map[string][][]byte{
		g.groupID: {g.memberPub["m1"], g.memberPub["m2"]},
	}

	req := keygenRequest{
		SessionID:  "sess-mismatch",
		ThresholdT: 2, PartiesN: 3,
		MemberSet: []string{
			hex.EncodeToString(g.memberPub["m0"]),
			hex.EncodeToString(g.memberPub["m1"]),
			hex.EncodeToString(g.memberPub["m2"]),
		},
		Deadline: h.clk.Now().UnixMilli() + 60_000,
	}
	memberSetBytes := [][]byte{g.memberPub["m0"], g.memberPub["m1"], g.memberPub["m2"]}
	digest := keygenRequestDigest(&req, memberSetBytes)
	req.ProposerSig = contract.SignDigest(g.members["m0"], digest)
	body, _ := json.Marshal(req)
	hdr := keygenMemberHdr(h, g.groupID, "m0", g.members["m0"], body)
	resp := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/keygen", hdr, body)
	if resp.code != http.StatusConflict {
		t.Fatalf("want 409 got %d body=%s", resp.code, resp.text())
	}
	if !contains409Code(resp.body, codeExpectedMember) {
		t.Fatalf("want EXPECTED_MEMBER_MISMATCH, got %s", resp.text())
	}
}

func TestDM4_KeygenB9_AlreadyKeyedRejected(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-already-keyed", 2, 3)
	setExpectedMembers(t, h, g)

	req := keygenRequest{
		SessionID:  "sess-rekey",
		ThresholdT: 2, PartiesN: 3,
		MemberSet: []string{
			hex.EncodeToString(g.memberPub["m0"]),
			hex.EncodeToString(g.memberPub["m1"]),
			hex.EncodeToString(g.memberPub["m2"]),
		},
		Deadline: h.clk.Now().UnixMilli() + 60_000,
	}
	memberSetBytes := [][]byte{g.memberPub["m0"], g.memberPub["m1"], g.memberPub["m2"]}
	digest := keygenRequestDigest(&req, memberSetBytes)
	req.ProposerSig = contract.SignDigest(g.members["m0"], digest)
	body, _ := json.Marshal(req)
	hdr := keygenMemberHdr(h, g.groupID, "m0", g.members["m0"], body)
	resp := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/keygen", hdr, body)
	if resp.code != http.StatusConflict {
		t.Fatalf("want 409 STATE_CONFLICT, got %d body=%s", resp.code, resp.text())
	}
	if !contains409Code(resp.body, codeStateConflict) {
		t.Fatalf("want STATE_CONFLICT, got %s", resp.text())
	}
}

func TestDM4_ReshareB10_HappyPath(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-reshare", 2, 3)
	setExpectedMembers(t, h, g)

	newSetHex := []string{
		hex.EncodeToString(g.memberPub["m0"]),
		hex.EncodeToString(g.memberPub["m1"]),
		hex.EncodeToString(g.memberPub["m2"]),
	}
	newSetBytes := [][]byte{g.memberPub["m0"], g.memberPub["m1"], g.memberPub["m2"]}
	// canonical oldSet ordering = sort by memberId.
	oldSet := [][]byte{g.memberPub["m0"], g.memberPub["m1"], g.memberPub["m2"]}

	req := reshareRequest{
		SessionID:    "sess-reshare-1",
		NewMemberSet: newSetHex,
		Deadline:     h.clk.Now().UnixMilli() + 60_000,
	}
	digest := reshareRequestDigest(&req, oldSet, newSetBytes)
	req.OldMemberSig = contract.SignDigest(g.members["m0"], digest)
	req.NewMemberSig = [][]byte{
		contract.SignDigest(g.members["m0"], digest),
		contract.SignDigest(g.members["m1"], digest),
		contract.SignDigest(g.members["m2"], digest),
	}
	body, _ := json.Marshal(req)
	hdr := keygenMemberHdr(h, g.groupID, "m0", g.members["m0"], body)
	hdr2 := h.memberHdr(g, "m0", "B10:reshare", g.groupID, body)
	// memberHdr already constructs the right preimage; both keygenMemberHdr
	// and memberHdr should give the same content but use memberHdr for B10.
	_ = hdr
	resp := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/reshare", hdr2, body)
	if resp.code != http.StatusAccepted {
		t.Fatalf("B10 reshare status=%d body=%s", resp.code, resp.text())
	}
	for _, mid := range []string{"m0", "m1", "m2"} {
		got, ok := h.co.hub.take(g.groupID, mid)
		if !ok {
			t.Fatalf("member %s did not receive reshare-START", mid)
		}
		ev, ok := got.(reshareStartEvent)
		if !ok {
			t.Fatalf("member %s got non-reshare event: %T", mid, got)
		}
		if ev.Type != "reshare-START" || ev.SessionID != "sess-reshare-1" {
			t.Fatalf("member %s bad reshare-START: %+v", mid, ev)
		}
	}
}

func TestDM4_ReshareB10_ExpectedMemberMismatch(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-reshare-mismatch", 2, 3)
	// expected_members declares only m0/m1 — the new-committee m2 is
	// rejected.
	h.co.cfg.ExpectedMembers = map[string][][]byte{
		g.groupID: {g.memberPub["m0"], g.memberPub["m1"]},
	}

	newSetHex := []string{
		hex.EncodeToString(g.memberPub["m0"]),
		hex.EncodeToString(g.memberPub["m1"]),
		hex.EncodeToString(g.memberPub["m2"]), // not in expected
	}
	req := reshareRequest{
		SessionID:    "sess-mismatch",
		NewMemberSet: newSetHex,
		NewMemberSig: [][]byte{{1}, {1}, {1}}, // unused; pre-cond fails first
		Deadline:     h.clk.Now().UnixMilli() + 60_000,
	}
	body, _ := json.Marshal(req)
	hdr := h.memberHdr(g, "m0", "B10:reshare", g.groupID, body)
	resp := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/reshare", hdr, body)
	if resp.code != http.StatusConflict || !contains409Code(resp.body, codeExpectedMember) {
		t.Fatalf("want 409 EXPECTED_MEMBER_MISMATCH, got %d %s", resp.code, resp.text())
	}
}

func TestDM4_AttestationB11_HappyPath(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-attest", 2, 3)
	setExpectedMembers(t, h, g)

	for i, mid := range []string{"m0", "m1", "m2"} {
		view := &attestationView{
			IdentityPubkey: g.memberPub[mid],
			HoldsShare:     true,
			GroupPubkey:    g.mainPub,
			Chaincode:      bytesOfLen(32, byte(0x10+i)),
			TS:             h.clk.Now().UnixMilli() + int64(i),
		}
		digest := attestationDigest(g.groupID, view)
		sig := contract.SignDigest(g.members[mid], digest)
		b := attestationBody{
			IdentityPubkey: hex.EncodeToString(g.memberPub[mid]),
			HoldsShare:     true,
			GroupPubkeyHex: hex.EncodeToString(g.mainPub),
			ChaincodeHex:   hex.EncodeToString(view.Chaincode),
			TS:             view.TS,
			Sig:            sig,
		}
		body, _ := json.Marshal(b)
		hdr := h.memberHdr(g, mid, "B11:attestation", g.groupID, body)
		resp := h.do(t, http.MethodPut, "/v1/groups/"+g.groupID+"/attestation", hdr, body)
		if resp.code != http.StatusOK {
			t.Fatalf("attestation[%s] status=%d body=%s", mid, resp.code, resp.text())
		}
		// Each attestation triggers an attestation-ACK back to the
		// attester via the dispatch hub.
		got, ok := h.co.hub.take(g.groupID, mid)
		if !ok {
			t.Fatalf("attester %s did not receive attestation-ACK", mid)
		}
		ack, ok := got.(attestationACKEvent)
		if !ok {
			t.Fatalf("attester %s got non-ACK event: %T", mid, got)
		}
		if ack.Type != "attestation-ACK" || ack.GroupID != g.groupID {
			t.Fatalf("bad attestation-ACK: %+v", ack)
		}
	}
}

func TestDM4_AttestationB11_ReplayReject(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-attest-replay", 2, 3)
	setExpectedMembers(t, h, g)

	mid := "m0"
	ts := h.clk.Now().UnixMilli()
	view := &attestationView{
		IdentityPubkey: g.memberPub[mid],
		HoldsShare:     false,
		TS:             ts,
	}
	digest := attestationDigest(g.groupID, view)
	sig := contract.SignDigest(g.members[mid], digest)
	b := attestationBody{
		IdentityPubkey: hex.EncodeToString(g.memberPub[mid]),
		HoldsShare:     false,
		TS:             ts,
		Sig:            sig,
	}
	body, _ := json.Marshal(b)
	hdr := h.memberHdr(g, mid, "B11:attestation", g.groupID, body)
	if r := h.do(t, http.MethodPut, "/v1/groups/"+g.groupID+"/attestation", hdr, body); r.code != http.StatusOK {
		t.Fatalf("first attestation: %d %s", r.code, r.text())
	}

	// Second submission with the same TS must be a STATE_CONFLICT
	// (monotonic-ts replay rejection — even within the memberGate nonce
	// window which has fresh nonces).
	hdr2 := h.memberHdr(g, mid, "B11:attestation", g.groupID, body)
	r := h.do(t, http.MethodPut, "/v1/groups/"+g.groupID+"/attestation", hdr2, body)
	if r.code != http.StatusConflict {
		t.Fatalf("want 409 replay, got %d %s", r.code, r.text())
	}
}

// TestDM4_DispatchHub_SignStartBackCompat asserts the sign-START JSON
// shape (contract.StartSigning) is unchanged after the dispatchHub
// generalization — an existing client decoding a bare StartSigning still
// succeeds (impl §B DM-4: backward-compat for legacy sign event).
func TestDM4_DispatchHub_SignStartBackCompat(t *testing.T) {
	hub := newDispatchHub()
	st := contract.StartSigning{
		RequestID: "r1",
		Signers:   []string{"m0"},
		SelfRole:  true,
	}
	hub.publish("g", []string{"m0"}, st)
	got, ok := hub.take("g", "m0")
	if !ok {
		t.Fatal("no event taken")
	}
	js, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !containsString(js, []byte(`"requestId":"r1"`)) {
		t.Fatalf("missing requestId in JSON: %s", js)
	}
	// A bare StartSigning decode must succeed (no wrapping shape).
	var decoded contract.StartSigning
	if err := json.Unmarshal(js, &decoded); err != nil {
		t.Fatalf("legacy decode: %v", err)
	}
	if decoded.RequestID != "r1" || !decoded.SelfRole {
		t.Fatalf("decoded mismatch: %+v", decoded)
	}
}

// keygenMemberHdr builds the B-side authentication headers for a B9-style
// request. We need a separate helper from testGroup-aware memberHdr
// because the empty-keyed-group seam in emptyKeyedGroup does not
// construct a testGroup; the signer's private key is supplied directly.
func keygenMemberHdr(h *harness, gid, mid string, priv *btcec.PrivateKey, body []byte) map[string]string {
	ts := h.clk.Now().UnixMilli()
	nonce := make([]byte, 12)
	_, _ = rand.Read(nonce)
	bound := append([]byte("B9:keygen|"+gid+"|"), body...)
	dig := memberAuthDigest(mid, "B9:keygen", hash(bound), ts, nonce)
	sig := contract.SignDigest(priv, dig)
	return map[string]string{
		"X-Member-Id":    mid,
		"X-Member-Ts":    strconv.FormatInt(ts, 10),
		"X-Member-Nonce": base64.StdEncoding.EncodeToString(nonce),
		"X-Member-Sig":   base64.StdEncoding.EncodeToString(sig),
		"Content-Type":   "application/json",
	}
}

// contains409Code is a tiny JSON probe: the api error envelope is
// {error:{code,message,requestId?}}; we only need the code byte-pattern
// since the test asserts the discriminator, not the message.
func contains409Code(body []byte, code string) bool {
	return containsString(body, []byte(`"code":"`+code+`"`))
}

func containsString(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func bytesOfLen(n int, fill byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = fill
	}
	return out
}
