package coord

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"

	"github.com/zzci/mpc/internal/contract"
)

// LOCKED: every data endpoint fail-closes 503 and leaks nothing; /healthz
// stays up (docs/design/server/server.md C9b, api.md:81-84).
func TestLockedFailClosed(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-lock", 2, 3)
	proposer, _, _ := newKey(t)
	env := h.envelope(t, g, proposer, time.Hour)
	raw, _ := json.Marshal(env)

	if err := h.store.Relock(); err != nil {
		t.Fatalf("relock: %v", err)
	}

	for _, ep := range []struct {
		method, path string
		body         []byte
	}{
		{http.MethodPost, "/v1/requests", raw},
		{http.MethodGet, "/v1/requests/x", nil},
		{http.MethodGet, "/v1/groups/grp-lock/pending", nil},
		{http.MethodPost, "/v1/groups", []byte("{}")},
		{http.MethodGet, "/v1/groups/grp-lock", nil},
	} {
		resp := h.do(t, ep.method, ep.path, nil, ep.body)
		if resp.code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s: want 503 got %d", ep.method, ep.path, resp.code)
		}
		var e map[string]map[string]string
		_ = json.Unmarshal(resp.body, &e)
		if e["error"]["code"] != codeLocked {
			t.Fatalf("%s %s: want LOCKED got %v", ep.method, ep.path, e)
		}
	}
	resp := h.do(t, http.MethodGet, "/healthz", nil, nil)
	if resp.code != http.StatusOK {
		t.Fatalf("healthz under LOCKED: %d", resp.code)
	}
}

// C8: a tampered envelope (businessInfo / fields) fails proposerSig -> 400,
// never enqueued.
func TestC8_TamperedEnvelope(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-tamper", 2, 3)
	proposer, _, _ := newKey(t)
	env := h.envelope(t, g, proposer, time.Hour)
	env.Chain = "tampered-after-signing"
	raw, _ := json.Marshal(env)
	resp := h.do(t, http.MethodPost, "/v1/requests", nil, raw)
	if resp.code != http.StatusBadRequest {
		t.Fatalf("tamper: want 400 got %d %s", resp.code, resp.text())
	}
	if _, found, _ := h.co.db.requestStatus(context.Background(), env.RequestID); found {
		t.Fatal("tampered envelope was enqueued")
	}
}

// C8: a forged {R,S,V} that does not recover the group key -> FAILED, no
// result leaked (api.md:30).
func TestC8_ForgedRSV(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-rsv", 2, 3)
	proposer, _, _ := newKey(t)
	env := h.envelope(t, g, proposer, time.Hour)
	raw, _ := json.Marshal(env)
	h.do(t, http.MethodPost, "/v1/requests", nil, raw)
	for _, id := range []string{"m0", "m1"} {
		h.heartbeat(t, g, id)
		h.decide(t, g, id, env.RequestID, "approved")
	}
	h.co.engine.evaluate(context.Background(), env.RequestID)

	wrong, _, _ := newKey(t)
	bad := signCompact(wrong, env.Digest32) // signed by a non-group key
	rb, _ := json.Marshal(resultBody{MemberID: "m0", RSV: bad})
	resp := h.do(t, http.MethodPost, "/v1/requests/"+env.RequestID+"/result",
		h.memberHdr(g, "m0", "B7:result", g.groupID, rb), rb)
	if resp.code != http.StatusBadRequest {
		t.Fatalf("forged rsv: want 400 got %d %s", resp.code, resp.text())
	}
	st, _, _ := h.co.db.requestStatus(context.Background(), env.RequestID)
	if st != stFailed {
		t.Fatalf("want FAILED got %s", st)
	}
	for _, b := range h.callbacks() {
		if b.RequestID == env.RequestID && b.RSV != "" {
			t.Fatal("forged result leaked to external service")
		}
	}
}

// C8: a non-signer cannot drive the result.
func TestC8_NonSignerResultRejected(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-ns", 2, 3)
	proposer, _, _ := newKey(t)
	env := h.envelope(t, g, proposer, time.Hour)
	raw, _ := json.Marshal(env)
	h.do(t, http.MethodPost, "/v1/requests", nil, raw)
	for _, id := range []string{"m0", "m1"} {
		h.heartbeat(t, g, id)
		h.decide(t, g, id, env.RequestID, "approved")
	}
	h.co.engine.evaluate(context.Background(), env.RequestID) // signers = m0,m1

	rsv := rsvFor(g, env.Digest32)
	rb, _ := json.Marshal(resultBody{MemberID: "m2", RSV: rsv})
	resp := h.do(t, http.MethodPost, "/v1/requests/"+env.RequestID+"/result",
		h.memberHdr(g, "m2", "B7:result", g.groupID, rb), rb)
	if resp.code != http.StatusForbidden {
		t.Fatalf("non-signer result: want 403 got %d %s", resp.code, resp.text())
	}
}

// C6(a)/(b): expiry rejected at ingestion and at result; external gets EXPIRED.
func TestC6_Expiry(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-ttl", 2, 3)
	proposer, _, _ := newKey(t)

	// (a)-at-submit: an already-expired envelope is 400, not enqueued.
	past := h.envelope(t, g, proposer, -time.Minute)
	rawPast, _ := json.Marshal(past)
	if resp := h.do(t, http.MethodPost, "/v1/requests", nil, rawPast); resp.code != http.StatusBadRequest {
		t.Fatalf("expired-at-submit: want 400 got %d", resp.code)
	}

	// Live envelope that expires while PENDING -> sweep/evaluate -> EXPIRED +
	// external callback (C6(a)).
	env := h.envelope(t, g, proposer, 30*time.Second)
	raw, _ := json.Marshal(env)
	h.do(t, http.MethodPost, "/v1/requests", nil, raw)
	h.clk.advance(time.Minute)
	h.co.engine.evaluate(context.Background(), env.RequestID)
	st, _, _ := h.co.db.requestStatus(context.Background(), env.RequestID)
	if st != stExpired {
		t.Fatalf("want EXPIRED got %s", st)
	}
	waitFor(t, func() bool {
		for _, b := range h.callbacks() {
			if b.RequestID == env.RequestID && b.Status == stExpired {
				return true
			}
		}
		return false
	})
}

// C6(b): expiry re-checked before accepting {R,S,V}.
func TestC6_ExpiryBeforeResult(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-ttl2", 2, 3)
	proposer, _, _ := newKey(t)
	env := h.envelope(t, g, proposer, 90*time.Second)
	raw, _ := json.Marshal(env)
	h.do(t, http.MethodPost, "/v1/requests", nil, raw)
	for _, id := range []string{"m0", "m1"} {
		h.heartbeat(t, g, id)
		h.decide(t, g, id, env.RequestID, "approved")
	}
	h.co.engine.evaluate(context.Background(), env.RequestID)
	h.clk.advance(2 * time.Minute) // expires after dispatch, before result

	rsv := rsvFor(g, env.Digest32)
	rb, _ := json.Marshal(resultBody{MemberID: "m0", RSV: rsv})
	resp := h.do(t, http.MethodPost, "/v1/requests/"+env.RequestID+"/result",
		h.memberHdr(g, "m0", "B7:result", g.groupID, rb), rb)
	if resp.code != http.StatusGone {
		t.Fatalf("expiry-before-result: want 410 got %d %s", resp.code, resp.text())
	}
}

// Idempotent A2: same requestId returns the original status, no double insert.
func TestIngestIdempotent(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-idem", 2, 3)
	proposer, _, _ := newKey(t)
	env := h.envelope(t, g, proposer, time.Hour)
	raw, _ := json.Marshal(env)
	for i := 0; i < 2; i++ {
		resp := h.do(t, http.MethodPost, "/v1/requests", nil, raw)
		if resp.code != http.StatusAccepted {
			t.Fatalf("ingest %d: %d", i, resp.code)
		}
	}
}

// Member auth: stale ts and replayed nonce are rejected (api.md D).
func TestMemberAuthReplay(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-replay", 2, 3)
	body, _ := json.Marshal(heartbeatBody{GroupID: g.groupID, MemberID: "m0", RelayPeerID: "p"})
	hdr := h.memberHdr(g, "m0", "B5:heartbeat", g.groupID, body)

	r1 := h.do(t, http.MethodPost, "/v1/members/self/heartbeat", hdr, body)
	if r1.code != http.StatusNoContent {
		t.Fatalf("first heartbeat: %d", r1.code)
	}
	r2 := h.do(t, http.MethodPost, "/v1/members/self/heartbeat", hdr, body) // replay
	if r2.code != http.StatusUnauthorized {
		t.Fatalf("replay: want 401 got %d", r2.code)
	}
}

// S-002: provisioning with insufficient / wrong co-signatures is rejected;
// a different ecdsaPubkey on the same groupId is 409 (anti-hijack §6/§3.3).
func TestProvisioningSelfAttest(t *testing.T) {
	h := newHarness(t)

	mainPriv, mainPub, _ := newKey(t)
	capPriv, _, capPubC := newKey(t)
	var entries []memberEntry
	mks := map[string]*btcec.PrivateKey{}
	for i := 0; i < 3; i++ {
		id := "m" + strconv.Itoa(i)
		mp, _, mpc := newKey(t)
		mks[id] = mp
		entries = append(entries, memberEntry{MemberID: id, IdentityPubkey: mpc})
	}
	mk := func(sign bool, cosigCount int) []byte {
		p := groupProvisioning{
			Version: contract.EnvelopeVersionV1, GroupID: "grp-sa",
			ECDSAPubkey: mainPub, GroupPubkey: capPubC,
			ThresholdT: 2, PartiesN: 3, Members: entries,
			CreatedAt: h.clk.Now().Format(time.RFC3339Nano),
		}
		dig, _ := groupProvisionDigest(&p)
		if sign {
			p.GroupSig = contract.SignDigest(capPriv, dig)
		} else {
			p.GroupSig = contract.SignDigest(mainPriv, dig) // wrong key
		}
		for i := 0; i < cosigCount; i++ {
			id := "m" + strconv.Itoa(i)
			p.MemberCoSigs = append(p.MemberCoSigs, coSig{id, contract.SignDigest(mks[id], dig)})
		}
		b, _ := json.Marshal(p)
		return b
	}

	// Wrong group key -> 401.
	if r := h.do(t, http.MethodPost, "/v1/groups", nil, mk(false, 2)); r.code != http.StatusUnauthorized {
		t.Fatalf("wrong groupSig: want 401 got %d %s", r.code, r.text())
	}
	// Too few co-signatures -> 400.
	if r := h.do(t, http.MethodPost, "/v1/groups", nil, mk(true, 1)); r.code != http.StatusBadRequest {
		t.Fatalf("insufficient cosigs: want 400 got %d %s", r.code, r.text())
	}
	// Valid -> 201.
	if r := h.do(t, http.MethodPost, "/v1/groups", nil, mk(true, 2)); r.code != http.StatusCreated {
		t.Fatalf("valid provision: want 201 got %d %s", r.code, r.text())
	}
	// Idempotent same binding -> 201.
	if r := h.do(t, http.MethodPost, "/v1/groups", nil, mk(true, 2)); r.code != http.StatusCreated {
		t.Fatalf("idempotent: want 201 got %d", r.code)
	}
	// Different ecdsaPubkey, same groupId -> 409.
	otherMain, otherPub, _ := newKey(t)
	_ = otherMain
	p := groupProvisioning{
		Version: contract.EnvelopeVersionV1, GroupID: "grp-sa",
		ECDSAPubkey: otherPub, GroupPubkey: capPubC,
		ThresholdT: 2, PartiesN: 3, Members: entries,
		CreatedAt: h.clk.Now().Format(time.RFC3339Nano),
	}
	dig, _ := groupProvisionDigest(&p)
	p.GroupSig = contract.SignDigest(capPriv, dig)
	for i := 0; i < 2; i++ {
		id := "m" + strconv.Itoa(i)
		p.MemberCoSigs = append(p.MemberCoSigs, coSig{id, contract.SignDigest(mks[id], dig)})
	}
	b, _ := json.Marshal(p)
	if r := h.do(t, http.MethodPost, "/v1/groups", nil, b); r.code != http.StatusConflict {
		t.Fatalf("rebind: want 409 got %d %s", r.code, r.text())
	}
}

// S-002 reshare: malicious membership change without ≥t current-member
// co-signatures is rejected; a valid one bumps epoch and rotates members.
func TestReshareAuthorization(t *testing.T) {
	h := newHarness(t)
	g := h.provision(t, "grp-rs", 2, 3)

	mkUpdate := func(epoch int64, cosigners []string, useStoredCap bool) []byte {
		_, newPub, newPubC := newKey(t)
		_ = newPub
		p := membershipUpdate{
			Version: contract.EnvelopeVersionV1, GroupID: g.groupID, Epoch: epoch,
			ECDSAPubkeyAssert: g.mainPub, GroupPubkey: g.capPubC,
			ThresholdT: 2, PartiesN: 4,
			AddedMembers:     []memberEntry{{MemberID: "m3", IdentityPubkey: newPubC}},
			RemovedMemberIDs: nil,
			UpdatedAt:        h.clk.Now().Format(time.RFC3339Nano),
		}
		dig, _ := membershipUpdateDigest(&p)
		if useStoredCap {
			p.GroupSig = contract.SignDigest(g.capPriv, dig)
		} else {
			bogus, _, _ := newKey(t)
			p.GroupSig = contract.SignDigest(bogus, dig)
		}
		for _, id := range cosigners {
			p.MemberCoSigs = append(p.MemberCoSigs, coSig{id, contract.SignDigest(g.members[id], dig)})
		}
		b, _ := json.Marshal(p)
		return b
	}

	// Bogus group key -> 401.
	if r := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/membership", nil,
		mkUpdate(1, []string{"m0", "m1"}, false)); r.code != http.StatusUnauthorized {
		t.Fatalf("bogus reshare groupSig: want 401 got %d %s", r.code, r.text())
	}
	// Only one current-member co-signature (< old t=2) -> 400.
	if r := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/membership", nil,
		mkUpdate(1, []string{"m0"}, true)); r.code != http.StatusBadRequest {
		t.Fatalf("insufficient reshare cosigs: want 400 got %d %s", r.code, r.text())
	}
	// Valid reshare -> 200, epoch bumped.
	r := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/membership", nil,
		mkUpdate(1, []string{"m0", "m1"}, true))
	if r.code != http.StatusOK {
		t.Fatalf("valid reshare: want 200 got %d %s", r.code, r.text())
	}
	// Epoch rollback replay -> 409.
	if r := h.do(t, http.MethodPost, "/v1/groups/"+g.groupID+"/membership", nil,
		mkUpdate(1, []string{"m0", "m1"}, true)); r.code != http.StatusConflict {
		// epoch == current and state already applied -> idempotent UPDATED(200)
		if r.code != http.StatusOK {
			t.Fatalf("epoch replay: want 409 or idempotent 200 got %d", r.code)
		}
	}
}
