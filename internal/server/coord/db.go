package coord

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/zzci/mpc/internal/contract"
	"github.com/zzci/mpc/internal/server/coorddb"
)

// db is coord's access layer over the D-001 store. coorddb (D-001) is owned by
// another task and MUST NOT be modified; it exports Store.WithTx plus a few
// helpers (ProvisionGroup/RecordTransition/RequestStatus/GroupEpoch/presence).
// Everything coord needs beyond those helpers (group/member reads, full
// envelope persistence incl. business_info, pending listing, approvals,
// signer/result columns, reshare) is done here as raw SQL against the
// D-001 migration schema through Store.WithTx, which runs BEGIN IMMEDIATE on
// the single-writer connection (database.md §5) so reads and writes serialize
// correctly. LOCKED is enforced by Store itself: WithTx returns
// coorddb.ErrLocked, mapped to 503 LOCKED upstream (fail-closed).
//
// Time columns for signing_requests are persisted as a decimal unix-ms string
// in the schema's TEXT column. The envelope canonical preimage needs int64
// unix ms (S-001 §2.3/§7); storing the exact integer as text makes the
// persist↔reconstruct round-trip lossless so proposerSig re-verifies bit
// identically after a restart (D-001 used TEXT, not the INTEGER S-001 §7
// recommends; coord adapts without changing D-001 or docs/design/).
type db struct {
	store *coorddb.Store
}

func newDB(s *coorddb.Store) *db { return &db{store: s} }

// groupRow is the public group record (groups table; all public columns).
// EVMAddress/TronAddress are the persisted derived chain addresses (G-001,
// docs/design/server/database.md groups schema); they are public data, never key
// material.
type groupRow struct {
	GroupID     string
	ECDSAPubkey []byte
	GroupPubkey []byte
	ThresholdT  int
	PartiesN    int
	Epoch       int64
	EVMAddress  string
	TronAddress string
}

// memberRow is one group_members row.
type memberRow struct {
	MemberID       string
	IdentityPubkey []byte
	Status         string
}

// storedRequest is a signing_requests row decoded back into typed fields,
// enough to rebuild the exact SigningRequest the proposer signed.
type storedRequest struct {
	RequestID    string
	GroupID      string
	Chain        string
	UnsignedTx   []byte
	Digest32     []byte
	Proposer     string
	BusinessRaw  []byte // JSON object bytes as submitted, or nil when absent
	MetaHash     []byte
	ProposerSig  []byte
	Status       string
	CreatedAtMs  int64
	ExpiryMs     int64
	DispatchedAt int64 // unix ms, 0 when not yet dispatched
	SignersJSON  string
	ResultRSV    []byte
	FailReason   string
}

var errGroupNotFound = errors.New("coord: group not found")

// group reads a group's public record. A missing group is errGroupNotFound so
// callers can map it to the right C-table code per endpoint.
func (d *db) group(ctx context.Context, groupID string) (groupRow, error) {
	var g groupRow
	err := d.store.WithTx(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx,
			`SELECT group_id, ecdsa_pubkey, group_pubkey, threshold_t, parties_n, epoch,
			        evm_address, tron_address
			   FROM groups WHERE group_id = ?`, groupID)
		if err := row.Scan(&g.GroupID, &g.ECDSAPubkey, &g.GroupPubkey,
			&g.ThresholdT, &g.PartiesN, &g.Epoch, &g.EVMAddress, &g.TronAddress); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errGroupNotFound
			}
			return fmt.Errorf("coord: read group: %w", err)
		}
		return nil
	})
	return g, err
}

// members returns every group_members row (active and removed) for the group.
func (d *db) members(ctx context.Context, groupID string) ([]memberRow, error) {
	var out []memberRow
	err := d.store.WithTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT member_id, identity_pubkey, status
			   FROM group_members WHERE group_id = ?`, groupID)
		if err != nil {
			return fmt.Errorf("coord: read members: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var m memberRow
			if err := rows.Scan(&m.MemberID, &m.IdentityPubkey, &m.Status); err != nil {
				return fmt.Errorf("coord: scan member: %w", err)
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

// activeMember returns the identity pubkey of an active member, or false when
// the member is absent or removed (B1 isolation, api.md:36).
func (d *db) activeMember(ctx context.Context, groupID, memberID string) ([]byte, bool, error) {
	ms, err := d.members(ctx, groupID)
	if err != nil {
		return nil, false, err
	}
	for _, m := range ms {
		if m.MemberID == memberID && m.Status == "active" {
			return m.IdentityPubkey, true, nil
		}
	}
	return nil, false, nil
}

// requestStatus returns the current status of a request and whether it exists.
func (d *db) requestStatus(ctx context.Context, requestID string) (string, bool, error) {
	st, err := d.store.RequestStatus(ctx, requestID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return st, true, nil
}

// insertEnvelope enqueues a validated envelope as PENDING in one transaction
// together with its initial request_events row (actor=external). The full
// envelope is persisted — including business_info — so START can carry the
// complete envelope and a post-restart reload re-verifies proposerSig
// identically. D-001's CreateSigningRequest seed omits business_info, so the
// row is written here via WithTx instead (D-001 unmodifiable).
func (d *db) insertEnvelope(ctx context.Context, env *contract.SigningRequest, businessRaw []byte, nowISO string) error {
	return d.store.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO signing_requests
			 (request_id, group_id, chain, unsigned_tx, digest32, proposer,
			  business_info, meta_hash, proposer_sig, status, created_at, expiry)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'PENDING', ?, ?)`,
			env.RequestID, env.GroupID, env.Chain, env.UnsignedTx, env.Digest32, env.Proposer,
			nullBytesAsText(businessRaw), env.MetaHash, env.ProposerSig,
			msText(env.CreatedAt), msText(env.Expiry)); err != nil {
			return fmt.Errorf("coord: insert signing_request: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO request_events (request_id, from_status, to_status, actor, detail, at)
			 VALUES (?, NULL, 'PENDING', 'external', NULL, ?)`,
			env.RequestID, nowISO); err != nil {
			return fmt.Errorf("coord: insert ingest event: %w", err)
		}
		return nil
	})
}

// loadRequest reads a full signing_requests row back into typed fields.
func (d *db) loadRequest(ctx context.Context, requestID string) (*storedRequest, error) {
	var r storedRequest
	err := d.store.WithTx(ctx, func(tx *sql.Tx) error {
		var business sql.NullString
		var createdAt, expiry string
		var dispatchedAt, signers, failReason sql.NullString
		var rsv []byte
		row := tx.QueryRowContext(ctx,
			`SELECT request_id, group_id, chain, unsigned_tx, digest32, proposer,
			        business_info, meta_hash, proposer_sig, status, created_at, expiry,
			        dispatched_at, signers, result_rsv, fail_reason
			   FROM signing_requests WHERE request_id = ?`, requestID)
		if err := row.Scan(&r.RequestID, &r.GroupID, &r.Chain, &r.UnsignedTx, &r.Digest32,
			&r.Proposer, &business, &r.MetaHash, &r.ProposerSig, &r.Status,
			&createdAt, &expiry, &dispatchedAt, &signers, &rsv, &failReason); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return sql.ErrNoRows
			}
			return fmt.Errorf("coord: read request: %w", err)
		}
		if business.Valid {
			r.BusinessRaw = []byte(business.String)
		}
		var perr error
		if r.CreatedAtMs, perr = parseMS(createdAt); perr != nil {
			return perr
		}
		if r.ExpiryMs, perr = parseMS(expiry); perr != nil {
			return perr
		}
		if dispatchedAt.Valid && dispatchedAt.String != "" {
			if r.DispatchedAt, perr = parseMS(dispatchedAt.String); perr != nil {
				return perr
			}
		}
		r.SignersJSON = signers.String
		r.ResultRSV = rsv
		r.FailReason = failReason.String
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// pendingPageSize bounds a B3 response so a large pending backlog cannot
// produce an unbounded body; the `since` createdAt-ms cursor pages the rest
// (api.md:43-46,90). created_at is a 13-digit decimal-ms string for the
// current era, so its lexical order equals numeric order — a sound SQL cursor.
const pendingPageSize = 200

// pending returns up to pendingPageSize of the group's PENDING requests with a
// created-at strictly greater than sinceMs (B3 cursor, api.md:43-46). Expiry
// is filtered by the caller against the coord clock so a not-yet-swept expired
// row never appears.
func (d *db) pending(ctx context.Context, groupID string, sinceMs int64) ([]storedRequest, error) {
	var out []storedRequest
	err := d.store.WithTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT request_id, group_id, chain, unsigned_tx, digest32, proposer,
			        business_info, meta_hash, proposer_sig, status, created_at, expiry
			   FROM signing_requests
			  WHERE group_id = ? AND status = 'PENDING' AND created_at > ?
			  ORDER BY created_at, request_id
			  LIMIT ?`, groupID, msText(sinceMs), pendingPageSize)
		if err != nil {
			return fmt.Errorf("coord: read pending: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var r storedRequest
			var business sql.NullString
			var createdAt, expiry string
			if err := rows.Scan(&r.RequestID, &r.GroupID, &r.Chain, &r.UnsignedTx,
				&r.Digest32, &r.Proposer, &business, &r.MetaHash, &r.ProposerSig,
				&r.Status, &createdAt, &expiry); err != nil {
				return fmt.Errorf("coord: scan pending: %w", err)
			}
			cms, perr := parseMS(createdAt)
			if perr != nil {
				return perr
			}
			ems, perr := parseMS(expiry)
			if perr != nil {
				return perr
			}
			if cms <= sinceMs {
				continue
			}
			if business.Valid {
				r.BusinessRaw = []byte(business.String)
			}
			r.CreatedAtMs, r.ExpiryMs = cms, ems
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// pendingRequestIDs lists every PENDING request id for a group (engine
// re-evaluation fan-out on heartbeat/approval).
func (d *db) pendingRequestIDs(ctx context.Context, groupID string) ([]string, error) {
	var ids []string
	err := d.store.WithTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT request_id FROM signing_requests
			  WHERE group_id = ? AND status = 'PENDING'`, groupID)
		if err != nil {
			return fmt.Errorf("coord: read pending ids: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return fmt.Errorf("coord: scan pending id: %w", err)
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	return ids, err
}

// allPendingRequestIDs lists every PENDING request id across all groups (sweep
// safety net for C6(a) expiry and missed quorum events).
func (d *db) allPendingRequestIDs(ctx context.Context) ([]string, error) {
	var ids []string
	err := d.store.WithTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT request_id FROM signing_requests WHERE status = 'PENDING'`)
		if err != nil {
			return fmt.Errorf("coord: read all pending: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return fmt.Errorf("coord: scan all pending: %w", err)
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	return ids, err
}

// activeRequestIDs lists requests still in flight (DISPATCHED/SIGNING) for the
// signer-offline rollback sweep (docs/design/server/server.md C5).
func (d *db) activeRequestIDs(ctx context.Context) ([]string, error) {
	var ids []string
	err := d.store.WithTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT request_id FROM signing_requests
			  WHERE status IN ('DISPATCHED','SIGNING')`)
		if err != nil {
			return fmt.Errorf("coord: read active ids: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return fmt.Errorf("coord: scan active id: %w", err)
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	return ids, err
}

// saveDecision upserts a member's approve/reject for a request (B4 idempotency
// key (requestId,memberId), api.md:88).
func (d *db) saveDecision(ctx context.Context, requestID, memberID, decision string, sig []byte, atISO string) error {
	return d.store.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO request_approvals (request_id, member_id, decision, sig, decided_at)
			 VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(request_id, member_id)
			 DO UPDATE SET decision = excluded.decision, sig = excluded.sig,
			               decided_at = excluded.decided_at`,
			requestID, memberID, decision, sig, atISO); err != nil {
			return fmt.Errorf("coord: save decision: %w", err)
		}
		return nil
	})
}

// decisions returns memberId->decision for a request.
func (d *db) decisions(ctx context.Context, requestID string) (map[string]string, error) {
	out := map[string]string{}
	err := d.store.WithTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT member_id, decision FROM request_approvals WHERE request_id = ?`, requestID)
		if err != nil {
			return fmt.Errorf("coord: read decisions: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var mid, dec string
			if err := rows.Scan(&mid, &dec); err != nil {
				return fmt.Errorf("coord: scan decision: %w", err)
			}
			out[mid] = dec
		}
		return rows.Err()
	})
	return out, err
}

// nullBytesAsText stores businessInfo JSON bytes in the TEXT column, or NULL
// when absent (api.md businessInfo optional; metaHash already covers absence).
func nullBytesAsText(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

// msText renders an int64 unix-ms timestamp as its exact decimal string for
// the schema's TEXT time columns (lossless round-trip, see db doc).
func msText(ms int64) string { return strconv.FormatInt(ms, 10) }

// parseMS parses a decimal unix-ms string written by msText.
func parseMS(s string) (int64, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("coord: corrupt ms timestamp %q: %w", s, err)
	}
	return v, nil
}

// signersJSON encodes/decodes the signer memberId list stored in the
// signing_requests.signers text[] (JSON) column.
func signersJSON(ids []string) string {
	b, _ := json.Marshal(ids)
	return string(b)
}

func decodeSigners(s string) []string {
	if s == "" {
		return nil
	}
	var ids []string
	_ = json.Unmarshal([]byte(s), &ids)
	return ids
}
