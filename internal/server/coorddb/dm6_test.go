package coorddb

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// DM-6 (distributed-mpc.md §3.7 / impl §B DM-6 / §6.6): same-transaction
// n-party attestation commit. The primitive must:
//
//   - INSERT groups + group_members + audit event atomically on first
//     commit (groups row absent → new row + member rows + one event);
//   - be idempotent on a re-call with the SAME ecdsa_pubkey (no
//     duplicate audit, no row touch);
//   - refuse a re-call with a DIFFERENT ecdsa_pubkey as ErrR7Violation;
//   - refuse an empty/NULL ecdsa_pubkey outright (the same R7 guard
//     ProvisionGroup uses).
//
// These tests run against a fresh encrypted store; the 00006 trigger is
// in place but never fires here because every successful path is an
// INSERT or a no-op read.

func TestDM6_CommitAttestationQuorum_FirstTime(t *testing.T) {
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())

	pub := []byte("ecdsa-pubkey-dm6-first")
	cc := bytesOfLenDM6(32, 0xCC)
	rec := GroupRecord{
		GroupID: "g-dm6-first", ECDSAPubkey: pub, GroupPubkey: pub,
		ThresholdT: 2, PartiesN: 3, Epoch: 0,
		CreatedAt: "2026-05-20T00:00:00Z",
		Chaincode: cc,
	}
	members := []MemberRecord{
		{MemberID: "m0", IdentityPubkey: []byte{0x01}},
		{MemberID: "m1", IdentityPubkey: []byte{0x02}},
		{MemberID: "m2", IdentityPubkey: []byte{0x03}},
	}
	already, err := s.CommitAttestationQuorum(ctx, rec, members)
	if err != nil {
		t.Fatalf("first commit: %v", err)
	}
	if already {
		t.Fatal("first commit must NOT report alreadyCommitted=true")
	}

	db := mustDB(t, s)
	var (
		gotPub  []byte
		gotCC   []byte
		gotT    int
		gotN    int
		gotEpoc int64
	)
	if err := db.QueryRowContext(ctx,
		`SELECT ecdsa_pubkey, chaincode, threshold_t, parties_n, epoch FROM groups WHERE group_id=?`,
		"g-dm6-first").Scan(&gotPub, &gotCC, &gotT, &gotN, &gotEpoc); err != nil {
		t.Fatalf("read groups: %v", err)
	}
	if string(gotPub) != string(pub) {
		t.Fatalf("pubkey mismatch: %q vs %q", gotPub, pub)
	}
	if string(gotCC) != string(cc) {
		t.Fatalf("chaincode mismatch")
	}
	if gotT != 2 || gotN != 3 || gotEpoc != 0 {
		t.Fatalf("scalar mismatch: t=%d n=%d epoch=%d", gotT, gotN, gotEpoc)
	}

	// All three group_members upserted active.
	var memberCount int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM group_members WHERE group_id=? AND status='active'`,
		"g-dm6-first").Scan(&memberCount); err != nil {
		t.Fatalf("read members: %v", err)
	}
	if memberCount != 3 {
		t.Fatalf("want 3 active members, got %d", memberCount)
	}

	// One ATTESTATION_QUORUM_COMMITTED audit event written.
	var eventCount int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM request_events WHERE request_id=? AND to_status='ATTESTATION_QUORUM_COMMITTED'`,
		"g-dm6-first").Scan(&eventCount); err != nil {
		t.Fatalf("read events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("want 1 quorum audit event, got %d", eventCount)
	}
}

func TestDM6_CommitAttestationQuorum_IdempotentSamePubkey(t *testing.T) {
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())
	pub := []byte("ecdsa-pubkey-dm6-idem")
	rec := GroupRecord{
		GroupID: "g-dm6-idem", ECDSAPubkey: pub, GroupPubkey: pub,
		ThresholdT: 2, PartiesN: 3, CreatedAt: "2026-05-20T00:00:00Z",
		Chaincode: bytesOfLenDM6(32, 0xAA),
	}
	members := []MemberRecord{
		{MemberID: "m0", IdentityPubkey: []byte{0x01}},
		{MemberID: "m1", IdentityPubkey: []byte{0x02}},
		{MemberID: "m2", IdentityPubkey: []byte{0x03}},
	}
	if _, err := s.CommitAttestationQuorum(ctx, rec, members); err != nil {
		t.Fatalf("seed commit: %v", err)
	}

	already, err := s.CommitAttestationQuorum(ctx, rec, members)
	if err != nil {
		t.Fatalf("re-commit: %v", err)
	}
	if !already {
		t.Fatal("re-commit with same pubkey must report alreadyCommitted=true")
	}

	// Idempotent: no duplicate audit row, no duplicate group row.
	db := mustDB(t, s)
	var (
		groupCount int
		eventCount int
	)
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM groups WHERE group_id=?`, "g-dm6-idem").Scan(&groupCount); err != nil {
		t.Fatalf("count groups: %v", err)
	}
	if groupCount != 1 {
		t.Fatalf("want 1 group row, got %d", groupCount)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM request_events WHERE request_id=? AND to_status='ATTESTATION_QUORUM_COMMITTED'`,
		"g-dm6-idem").Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("want 1 audit event after idempotent re-commit, got %d", eventCount)
	}
}

func TestDM6_CommitAttestationQuorum_DifferentPubkeyR7(t *testing.T) {
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())

	pubA := []byte("ecdsa-A")
	if _, err := s.CommitAttestationQuorum(ctx,
		GroupRecord{
			GroupID: "g-dm6-r7", ECDSAPubkey: pubA, GroupPubkey: pubA,
			ThresholdT: 2, PartiesN: 3, CreatedAt: "2026-05-20T00:00:00Z",
			Chaincode: bytesOfLenDM6(32, 0xBB),
		},
		[]MemberRecord{{MemberID: "m0", IdentityPubkey: []byte{0x01}}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// A second commit with a different ecdsa_pubkey must be R7.
	pubB := []byte("ecdsa-B-totally-different")
	_, err := s.CommitAttestationQuorum(ctx,
		GroupRecord{
			GroupID: "g-dm6-r7", ECDSAPubkey: pubB, GroupPubkey: pubB,
			ThresholdT: 2, PartiesN: 3, CreatedAt: "2026-05-20T00:00:00Z",
			Chaincode: bytesOfLenDM6(32, 0xBB),
		},
		[]MemberRecord{{MemberID: "m0", IdentityPubkey: []byte{0x01}}})
	if !errors.Is(err, ErrR7Violation) {
		t.Fatalf("want ErrR7Violation, got %v", err)
	}

	// The original pubkey is unchanged (R7 enforced both at the
	// application read and the SQLite trigger; see r7_test.go).
	db := mustDB(t, s)
	var got []byte
	if err := db.QueryRowContext(ctx,
		`SELECT ecdsa_pubkey FROM groups WHERE group_id=?`, "g-dm6-r7").Scan(&got); err != nil {
		t.Fatalf("post-read: %v", err)
	}
	if string(got) != string(pubA) {
		t.Fatalf("pubkey mutated: %q vs %q", got, pubA)
	}
}

func TestDM6_CommitAttestationQuorum_EmptyPubkeyRejected(t *testing.T) {
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())
	_, err := s.CommitAttestationQuorum(ctx,
		GroupRecord{
			GroupID: "g-dm6-empty", ECDSAPubkey: nil, GroupPubkey: []byte{0x01},
			ThresholdT: 2, PartiesN: 3, CreatedAt: "2026-05-20T00:00:00Z",
		},
		[]MemberRecord{{MemberID: "m0", IdentityPubkey: []byte{0x01}}})
	if !errors.Is(err, ErrR7Violation) {
		t.Fatalf("want ErrR7Violation on empty pubkey, got %v", err)
	}
}

func TestDM6_CommitAttestationQuorum_PreSeededMembersUpserted(t *testing.T) {
	// Future identity-registration path: group_members rows already
	// exist for the group (e.g. seeded by a hypothetical
	// PUT /v1/members/self/register flow), but no groups row exists
	// yet. The DM-6 commit must INSERT the groups row + UPSERT every
	// existing member (identity_pubkey refreshed, status flipped back
	// to active if needed) all in ONE transaction.
	ctx := context.Background()
	s := mustUnlocked(t, t.TempDir())
	db := mustDB(t, s)

	// Pre-seed two members; the third does not exist yet.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO group_members (group_id, member_id, identity_pubkey, status)
		 VALUES (?, ?, ?, 'removed')`,
		"g-dm6-pre", "m0", []byte{0xAA}); err != nil {
		t.Fatalf("seed m0: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO group_members (group_id, member_id, identity_pubkey, status)
		 VALUES (?, ?, ?, 'active')`,
		"g-dm6-pre", "m1", []byte{0xBB}); err != nil {
		t.Fatalf("seed m1: %v", err)
	}

	pub := []byte("ecdsa-pubkey-dm6-pre")
	rec := GroupRecord{
		GroupID: "g-dm6-pre", ECDSAPubkey: pub, GroupPubkey: pub,
		ThresholdT: 2, PartiesN: 3, CreatedAt: "2026-05-20T00:00:00Z",
		Chaincode: bytesOfLenDM6(32, 0xDD),
	}
	// New identity_pubkey for m0; m1 unchanged; m2 brand new.
	members := []MemberRecord{
		{MemberID: "m0", IdentityPubkey: []byte{0x11}},
		{MemberID: "m1", IdentityPubkey: []byte{0xBB}},
		{MemberID: "m2", IdentityPubkey: []byte{0x33}},
	}
	if _, err := s.CommitAttestationQuorum(ctx, rec, members); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Read back every member: m0 must be reactivated with new key,
	// m1 unchanged active, m2 inserted active.
	rows, err := db.QueryContext(ctx,
		`SELECT member_id, identity_pubkey, status FROM group_members WHERE group_id=? ORDER BY member_id`,
		"g-dm6-pre")
	if err != nil {
		t.Fatalf("query members: %v", err)
	}
	defer func() { _ = rows.Close() }()
	type row struct {
		id   string
		key  []byte
		stat string
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.key, &r.stat); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 members, got %d (%+v)", len(got), got)
	}
	if got[0].id != "m0" || string(got[0].key) != string([]byte{0x11}) || got[0].stat != "active" {
		t.Fatalf("m0 not reactivated with new key: %+v", got[0])
	}
	if got[1].id != "m1" || string(got[1].key) != string([]byte{0xBB}) || got[1].stat != "active" {
		t.Fatalf("m1 mismatch: %+v", got[1])
	}
	if got[2].id != "m2" || string(got[2].key) != string([]byte{0x33}) || got[2].stat != "active" {
		t.Fatalf("m2 not inserted: %+v", got[2])
	}
}

// bytesOfLenDM6 builds a fixed-length filler byte slice — local mirror of
// the test helper used by chaincode_test/dm4_test so this file compiles
// standalone without coupling to other test files.
func bytesOfLenDM6(n int, fill byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = fill
	}
	return out
}

// silence unused import if needed
var _ sql.NullByte
