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
	"github.com/zzci/mpc/internal/server/coorddb"
)

// DM-6 (distributed-mpc.md §3.7 / impl §B DM-6 / §6.6): closure-gate
// commit-on-attestation-quorum.
//
//   - commitAttestationQuorum is a noop until every expected_members
//     identity has reported holdsShare=true with a consistent
//     (groupPubkey, chaincode);
//   - on quorum, it commits the groups row + group_members rows + one
//     audit event in ONE transaction via
//     coorddb.CommitAttestationQuorum;
//   - a partial / inconsistent set never mutates groups;
//   - an R7 violation (a quorum disagreeing with an already-stored
//     ecdsa_pubkey) surfaces as ErrR7Violation → STATE_CONFLICT;
//   - the wiring from hAttestation propagates an R7 violation as a 409
//     STATE_CONFLICT (asAPIError mapping in errors.go).

// dm6Group is the per-test fixture: members + the pre-keygen seed of
// group_members rows (no groups row) and the strict-set wiring.
type dm6Group struct {
	groupID    string
	members    []*btcec.PrivateKey
	memberPubs [][]byte
	memberIDs  []string
}

// seedDM6Group inserts n group_members rows for groupID directly via
// the encrypted store — simulating the upstream identity-registration
// step. No groups row is created, so the DM-6 commit is exercised on a
// fresh INSERT path. The strict-set Config is wired so hAttestation
// accepts each identity.
func seedDM6Group(t *testing.T, h *harness, groupID string, n int) *dm6Group {
	t.Helper()
	ctx := context.Background()
	g := &dm6Group{groupID: groupID}
	expected := make([][]byte, 0, n)
	for i := 0; i < n; i++ {
		priv, err := btcec.NewPrivateKey()
		if err != nil {
			t.Fatalf("key: %v", err)
		}
		mid := "m" + strconv.Itoa(i)
		pubC := priv.PubKey().SerializeCompressed()
		if err := dm6InsertActiveMember(ctx, h.store, groupID, mid, pubC); err != nil {
			t.Fatalf("seed member %s: %v", mid, err)
		}
		g.members = append(g.members, priv)
		g.memberPubs = append(g.memberPubs, pubC)
		g.memberIDs = append(g.memberIDs, mid)
		expected = append(expected, pubC)
	}
	if h.co.cfg.ExpectedMembers == nil {
		h.co.cfg.ExpectedMembers = map[string][][]byte{}
	}
	h.co.cfg.ExpectedMembers[groupID] = expected
	return g
}

// dm6InsertActiveMember inserts one group_members row via WithTx (the
// only public Store API that exposes the underlying *sql.Tx). The
// schema does not enforce FK on group_id (PRAGMA foreign_keys is off),
// so this seed never requires a groups row.
func dm6InsertActiveMember(ctx context.Context, s *coorddb.Store, gid, mid string, idPub []byte) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO group_members (group_id, member_id, identity_pubkey, status)
			 VALUES (?, ?, ?, 'active')`,
			gid, mid, idPub)
		return err
	})
}

// dm6ReadGroup reads (ecdsa_pubkey, audit-event count, group-row count)
// for an end-of-test assertion suite.
func dm6ReadGroup(ctx context.Context, s *coorddb.Store, gid string) (pub []byte, groupCount, auditCount int, err error) {
	err = s.WithTx(ctx, func(tx *sql.Tx) error {
		var p []byte
		row := tx.QueryRowContext(ctx,
			`SELECT ecdsa_pubkey FROM groups WHERE group_id = ?`, gid)
		switch err := row.Scan(&p); {
		case err == nil:
			pub = p
			groupCount = 1
		case err.Error() == "sql: no rows in result set":
			groupCount = 0
		default:
			return err
		}
		return tx.QueryRowContext(ctx,
			`SELECT count(*) FROM request_events WHERE request_id=? AND to_status='ATTESTATION_QUORUM_COMMITTED'`,
			gid).Scan(&auditCount)
	})
	return pub, groupCount, auditCount, err
}

// dm6PostAttestation drives a B11 attestation for one member of the
// fixture. The supplied (pub, cc) is what the member claims to have
// derived locally.
func dm6PostAttestation(t *testing.T, h *harness, g *dm6Group, idx int, pub, cc []byte, holdsShare bool, ts int64) *hresp {
	t.Helper()
	priv := g.members[idx]
	pubC := g.memberPubs[idx]
	mid := g.memberIDs[idx]
	view := &attestationView{
		IdentityPubkey: pubC,
		HoldsShare:     holdsShare,
		GroupPubkey:    pub,
		Chaincode:      cc,
		TS:             ts,
	}
	digest := attestationDigest(g.groupID, view)
	sig := contract.SignDigest(priv, digest)
	b := attestationBody{
		IdentityPubkey: hex.EncodeToString(pubC),
		HoldsShare:     holdsShare,
		TS:             ts,
		Sig:            sig,
	}
	if holdsShare {
		b.GroupPubkeyHex = hex.EncodeToString(pub)
		b.ChaincodeHex = hex.EncodeToString(cc)
	}
	body, _ := json.Marshal(b)
	hdr := dm6MemberHdr(h, g.groupID, mid, priv, body)
	return h.do(t, http.MethodPut, "/v1/groups/"+g.groupID+"/attestation", hdr, body)
}

// dm6MemberHdr signs the B11 X-Member-* headers for the test fixture
// (the testGroup-aware harness.memberHdr is not usable here — the
// fixture is built around raw keys without a testGroup wrapper).
func dm6MemberHdr(h *harness, gid, mid string, priv *btcec.PrivateKey, body []byte) map[string]string {
	const method = "B11:attestation"
	ts := h.clk.Now().UnixMilli()
	nonce := make([]byte, 12)
	_, _ = rand.Read(nonce)
	bound := append([]byte(method+"|"+gid+"|"), body...)
	dig := memberAuthDigest(mid, method, hash(bound), ts, nonce)
	sig := contract.SignDigest(priv, dig)
	return map[string]string{
		"X-Member-Id":    mid,
		"X-Member-Ts":    strconv.FormatInt(ts, 10),
		"X-Member-Nonce": base64.StdEncoding.EncodeToString(nonce),
		"X-Member-Sig":   base64.StdEncoding.EncodeToString(sig),
		"Content-Type":   "application/json",
	}
}

func TestDM6_CommitOnAttestationQuorum_HappyPath(t *testing.T) {
	h := newHarness(t)
	g := seedDM6Group(t, h, "g-dm6-happy", 3)

	pub := bytesOfLen(65, 0x04) // 65B uncompressed-shaped fixture
	cc := bytesOfLen(32, 0x11)
	for i := 0; i < 3; i++ {
		resp := dm6PostAttestation(t, h, g, i, pub, cc, true, h.clk.Now().UnixMilli()+int64(i))
		if resp.code != http.StatusOK {
			t.Fatalf("attestation[%d]: %d %s", i, resp.code, resp.text())
		}
	}

	gotPub, groupCount, auditCount, err := dm6ReadGroup(context.Background(), h.store, g.groupID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if groupCount != 1 {
		t.Fatalf("want groups row written, got count=%d", groupCount)
	}
	if string(gotPub) != string(pub) {
		t.Fatalf("pubkey mismatch: got %x want %x", gotPub, pub)
	}
	if auditCount != 1 {
		t.Fatalf("want 1 quorum audit event, got %d", auditCount)
	}
}

func TestDM6_CommitOnAttestationQuorum_PartialQuorumNoCommit(t *testing.T) {
	h := newHarness(t)
	g := seedDM6Group(t, h, "g-dm6-partial", 3)
	pub := bytesOfLen(65, 0x04)
	cc := bytesOfLen(32, 0x22)

	for i := 0; i < 2; i++ {
		if r := dm6PostAttestation(t, h, g, i, pub, cc, true, h.clk.Now().UnixMilli()+int64(i)); r.code != http.StatusOK {
			t.Fatalf("attestation[%d]: %d %s", i, r.code, r.text())
		}
	}
	_, groupCount, _, err := dm6ReadGroup(context.Background(), h.store, g.groupID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if groupCount != 0 {
		t.Fatalf("partial quorum must NOT commit; got %d groups rows", groupCount)
	}
}

func TestDM6_CommitOnAttestationQuorum_InconsistentNoCommit(t *testing.T) {
	h := newHarness(t)
	g := seedDM6Group(t, h, "g-dm6-inconsistent", 3)
	pub := bytesOfLen(65, 0x04)
	pubAlt := bytesOfLen(65, 0x05)
	cc := bytesOfLen(32, 0x33)

	for i := 0; i < 2; i++ {
		if r := dm6PostAttestation(t, h, g, i, pub, cc, true, h.clk.Now().UnixMilli()+int64(i)); r.code != http.StatusOK {
			t.Fatalf("attestation[%d]: %d %s", i, r.code, r.text())
		}
	}
	if r := dm6PostAttestation(t, h, g, 2, pubAlt, cc, true, h.clk.Now().UnixMilli()+10); r.code != http.StatusOK {
		t.Fatalf("attestation[2]: %d %s", r.code, r.text())
	}
	_, groupCount, _, err := dm6ReadGroup(context.Background(), h.store, g.groupID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if groupCount != 0 {
		t.Fatalf("inconsistent quorum must NOT commit; got %d groups rows", groupCount)
	}
}

func TestDM6_CommitOnAttestationQuorum_IdempotentReCommit(t *testing.T) {
	h := newHarness(t)
	g := seedDM6Group(t, h, "g-dm6-idem", 3)
	pub := bytesOfLen(65, 0x04)
	cc := bytesOfLen(32, 0x44)

	for i := 0; i < 3; i++ {
		if r := dm6PostAttestation(t, h, g, i, pub, cc, true, h.clk.Now().UnixMilli()+int64(i)); r.code != http.StatusOK {
			t.Fatalf("attestation[%d]: %d %s", i, r.code, r.text())
		}
	}

	// Re-trigger the commit via the orchestrator directly.
	committed, err := h.co.commitAttestationQuorum(context.Background(), g.groupID)
	if err != nil {
		t.Fatalf("re-commit: %v", err)
	}
	if committed {
		t.Fatal("re-commit must report committed=false (idempotent)")
	}
	_, groupCount, auditCount, err := dm6ReadGroup(context.Background(), h.store, g.groupID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if groupCount != 1 || auditCount != 1 {
		t.Fatalf("idempotent re-commit must not duplicate rows; group=%d audit=%d", groupCount, auditCount)
	}
}

func TestDM6_CommitOnAttestationQuorum_R7Violation(t *testing.T) {
	h := newHarness(t)
	g := seedDM6Group(t, h, "g-dm6-r7", 3)

	// Pre-seed the groups row with a DIFFERENT pubkey via the
	// encrypted store, then attempt a quorum commit with a conflicting
	// pubkey. The orchestrator must surface ErrR7Violation through the
	// HTTP edge as a 409 STATE_CONFLICT.
	storedPub := bytesOfLen(65, 0x06)
	if err := h.store.WithTx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(),
			`INSERT INTO groups (group_id, ecdsa_pubkey, threshold_t, parties_n,
			 group_pubkey, epoch, created_at, updated_at, evm_address, tron_address, chaincode)
			 VALUES (?, ?, ?, ?, ?, 0, ?, ?, '', '', NULL)`,
			g.groupID, storedPub, 2, 3, storedPub, "2026-05-20T00:00:00Z", "2026-05-20T00:00:00Z")
		return err
	}); err != nil {
		t.Fatalf("seed groups: %v", err)
	}

	freshPub := bytesOfLen(65, 0x04)
	cc := bytesOfLen(32, 0x55)
	for i := 0; i < 2; i++ {
		if r := dm6PostAttestation(t, h, g, i, freshPub, cc, true, h.clk.Now().UnixMilli()+int64(i)); r.code != http.StatusOK {
			t.Fatalf("attestation[%d]: %d %s", i, r.code, r.text())
		}
	}
	r := dm6PostAttestation(t, h, g, 2, freshPub, cc, true, h.clk.Now().UnixMilli()+10)
	if r.code != http.StatusConflict {
		t.Fatalf("want 409 STATE_CONFLICT on R7, got %d %s", r.code, r.text())
	}
	if !contains409Code(r.body, codeStateConflict) {
		t.Fatalf("want STATE_CONFLICT code, got %s", r.text())
	}

	// Stored pubkey unchanged (R7 trigger + app-layer guard).
	gotPub, _, _, err := dm6ReadGroup(context.Background(), h.store, g.groupID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(gotPub) != string(storedPub) {
		t.Fatalf("R7 violated: pubkey mutated")
	}
}

func TestDM6_CommitAttestationQuorum_NoExpectedMembersNoCommit(t *testing.T) {
	h := newHarness(t)
	committed, err := h.co.commitAttestationQuorum(context.Background(), "g-noexpected")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if committed {
		t.Fatal("no expected_members must NOT commit")
	}
}
