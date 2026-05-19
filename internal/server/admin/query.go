package admin

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

// Read-only surface (admin.md §1 visible). Every query runs through
// coorddb.Store.WithTx (D-001 public API) against the migration schema, so
// LOCKED is enforced by the store itself (WithTx → coorddb.ErrLocked → 503
// LOCKED upstream, fail-closed) and this package never modifies coorddb.
//
// Hard boundary (admin.md §21/§70): shares and MPC payloads are NEVER
// persisted (database.md §7) so no query can surface them — the no-shares
// guarantee is structural, not a filter. unsigned_tx / business_info ARE
// visible to the operator: that is the documented privacy trade-off
// (admin.md §2 — coord already holds the cleartext envelope; the admin sees no
// trust increment). Semantic decoding into human-readable detail is an
// admin-ui / tx-decode display concern and explicitly non-security
// (admin.md §25); admin-api returns the stored fields verbatim.

const (
	defaultPageLimit = 100
	maxPageLimit     = 500
)

// txFilter is the parsed transaction-search query (admin.md §1: by
// group/time/status/proposer; database.md §6 indexes
// idx_sr_group_created/idx_sr_proposer_created/idx_sr_status_created back it).
type txFilter struct {
	groupID  string
	status   string
	proposer string
	fromMS   string // decimal unix-ms inclusive lower bound, "" = unbounded
	toMS     string // decimal unix-ms inclusive upper bound, "" = unbounded
	beforeMS string // pagination cursor: created_at strictly < beforeMS
	limit    int
}

func parseTxFilter(r *http.Request) (txFilter, error) {
	q := r.URL.Query()
	f := txFilter{
		groupID:  q.Get("group"),
		status:   q.Get("status"),
		proposer: q.Get("proposer"),
		limit:    defaultPageLimit,
	}
	if s := q.Get("status"); s != "" && !validStatus(s) {
		return f, fmt.Errorf("unknown status %q", s)
	}
	for key, dst := range map[string]*string{"from": &f.fromMS, "to": &f.toMS, "before": &f.beforeMS} {
		if v := q.Get(key); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				return f, fmt.Errorf("%s must be a unix-ms integer", key)
			}
			*dst = strconv.FormatInt(n, 10)
		}
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return f, fmt.Errorf("limit must be a positive integer")
		}
		f.limit = min(n, maxPageLimit)
	}
	return f, nil
}

var statusSet = map[string]struct{}{
	"PENDING": {}, "DISPATCHED": {}, "SIGNING": {}, "SIGNED": {},
	"RETURNED": {}, "EXPIRED": {}, "REJECTED": {}, "FAILED": {},
}

func validStatus(s string) bool { _, ok := statusSet[s]; return ok }

// hTransactions lists signing requests newest-first with optional filters and
// a created_at cursor (admin.md §1 transaction-record / historical
// signing-session list view).
func (s *Server) hTransactions(w http.ResponseWriter, r *http.Request) {
	f, err := parseTxFilter(r)
	if err != nil {
		s.writeErr(w, errBadRequest(err.Error()))
		return
	}
	where := "1=1"
	args := []any{}
	add := func(cond string, v any) { where += " AND " + cond; args = append(args, v) }
	if f.groupID != "" {
		add("group_id = ?", f.groupID)
	}
	if f.status != "" {
		add("status = ?", f.status)
	}
	if f.proposer != "" {
		add("proposer = ?", f.proposer)
	}
	if f.fromMS != "" {
		add("created_at >= ?", f.fromMS)
	}
	if f.toMS != "" {
		add("created_at <= ?", f.toMS)
	}
	if f.beforeMS != "" {
		add("created_at < ?", f.beforeMS)
	}
	args = append(args, f.limit)

	items := []map[string]any{}
	qErr := s.store.WithTx(r.Context(), func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(r.Context(),
			`SELECT request_id, group_id, chain, proposer, status,
			        created_at, expiry, dispatched_at, fail_reason
			   FROM signing_requests
			  WHERE `+where+`
			  ORDER BY created_at DESC, request_id DESC
			  LIMIT ?`, args...)
		if err != nil {
			return fmt.Errorf("admin: query transactions: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var reqID, groupID, chain, proposer, status, createdAt, expiry string
			var dispatchedAt, failReason sql.NullString
			if err := rows.Scan(&reqID, &groupID, &chain, &proposer, &status,
				&createdAt, &expiry, &dispatchedAt, &failReason); err != nil {
				return fmt.Errorf("admin: scan transaction: %w", err)
			}
			items = append(items, map[string]any{
				"requestId":    reqID,
				"groupId":      groupID,
				"chain":        chain,
				"proposer":     proposer,
				"status":       status,
				"createdAt":    createdAt,
				"expiry":       expiry,
				"dispatchedAt": dispatchedAt.String,
				"failReason":   failReason.String,
			})
		}
		return rows.Err()
	})
	if qErr != nil {
		s.writeErr(w, asAPIError(qErr))
		return
	}
	out := map[string]any{"items": items, "count": len(items)}
	if n := len(items); n > 0 && n == f.limit {
		out["nextBefore"] = items[n-1]["createdAt"]
	}
	s.writeJSON(w, http.StatusOK, out)
}

// hTransactionDetail returns one request's full decoded detail + complete
// status timeline (request_events) + approvals + result {R,S,V}
// (admin.md §1/§7bis "retrieve any historical request's decode detail
// + full status timeline + approvals + result").
func (s *Server) hTransactionDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("requestId")
	if id == "" {
		s.writeErr(w, errBadRequest("missing requestId"))
		return
	}
	var detail map[string]any
	timeline := []map[string]any{}
	approvals := []map[string]any{}

	qErr := s.store.WithTx(r.Context(), func(tx *sql.Tx) error {
		var groupID, chain, proposer, status, createdAt, expiry string
		var unsignedTx, digest32, metaHash, proposerSig, resultRSV []byte
		var business, dispatchedAt, signers, failReason sql.NullString
		row := tx.QueryRowContext(r.Context(),
			`SELECT group_id, chain, unsigned_tx, digest32, proposer, business_info,
			        meta_hash, proposer_sig, status, created_at, expiry,
			        dispatched_at, signers, result_rsv, fail_reason
			   FROM signing_requests WHERE request_id = ?`, id)
		if err := row.Scan(&groupID, &chain, &unsignedTx, &digest32, &proposer,
			&business, &metaHash, &proposerSig, &status, &createdAt, &expiry,
			&dispatchedAt, &signers, &resultRSV, &failReason); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errNotFound("unknown requestId")
			}
			return fmt.Errorf("admin: read request: %w", err)
		}
		detail = map[string]any{
			"requestId":    id,
			"groupId":      groupID,
			"chain":        chain,
			"proposer":     proposer,
			"status":       status,
			"createdAt":    createdAt,
			"expiry":       expiry,
			"dispatchedAt": dispatchedAt.String,
			"failReason":   failReason.String,
			"unsignedTx":   b64(unsignedTx), // display-only; semantic decode is admin-ui/tx-decode
			"digest32":     b64(digest32),
			"metaHash":     b64(metaHash),
			"proposerSig":  b64(proposerSig),
			"businessInfo": jsonOrNil(business),
			"signers":      jsonOrNil(signers),
		}
		if len(resultRSV) > 0 {
			detail["result"] = map[string]any{"rsv": b64(resultRSV)}
		}

		evRows, err := tx.QueryContext(r.Context(),
			`SELECT from_status, to_status, actor, detail, at
			   FROM request_events WHERE request_id = ? ORDER BY at, id`, id)
		if err != nil {
			return fmt.Errorf("admin: query events: %w", err)
		}
		defer func() { _ = evRows.Close() }()
		for evRows.Next() {
			var from, to, det sql.NullString
			var actor, at string
			if err := evRows.Scan(&from, &to, &actor, &det, &at); err != nil {
				return fmt.Errorf("admin: scan event: %w", err)
			}
			timeline = append(timeline, map[string]any{
				"fromStatus": from.String, "toStatus": to.String,
				"actor": actor, "detail": jsonOrNil(det), "at": at,
			})
		}
		if err := evRows.Err(); err != nil {
			return err
		}

		apRows, err := tx.QueryContext(r.Context(),
			`SELECT member_id, decision, decided_at
			   FROM request_approvals WHERE request_id = ? ORDER BY decided_at`, id)
		if err != nil {
			return fmt.Errorf("admin: query approvals: %w", err)
		}
		defer func() { _ = apRows.Close() }()
		for apRows.Next() {
			var memberID, decision, decidedAt string
			if err := apRows.Scan(&memberID, &decision, &decidedAt); err != nil {
				return fmt.Errorf("admin: scan approval: %w", err)
			}
			approvals = append(approvals, map[string]any{
				"memberId": memberID, "decision": decision, "decidedAt": decidedAt,
			})
		}
		return apRows.Err()
	})
	if qErr != nil {
		s.writeErr(w, asAPIError(qErr))
		return
	}
	detail["timeline"] = timeline
	detail["approvals"] = approvals
	s.writeJSON(w, http.StatusOK, detail)
}

// hAudit lists admin_audit newest-first (admin.md §1 audit view). The trail is
// read-only here and append-only at the store (no update/delete route exists),
// so the administrator cannot tamper with it (admin.md §7bis).
func (s *Server) hAudit(w http.ResponseWriter, r *http.Request) {
	limit := defaultPageLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			s.writeErr(w, errBadRequest("limit must be a positive integer"))
			return
		}
		limit = min(n, maxPageLimit)
	}
	beforeID := int64(-1)
	if v := r.URL.Query().Get("before"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			s.writeErr(w, errBadRequest("before must be a non-negative id"))
			return
		}
		beforeID = n
	}
	items := []map[string]any{}
	qErr := s.store.WithTx(r.Context(), func(tx *sql.Tx) error {
		where := ""
		args := []any{}
		if beforeID >= 0 {
			where = "WHERE id < ?"
			args = append(args, beforeID)
		}
		args = append(args, limit)
		rows, err := tx.QueryContext(r.Context(),
			`SELECT id, admin_id, action, params, src_ip, at
			   FROM admin_audit `+where+`
			  ORDER BY id DESC LIMIT ?`, args...)
		if err != nil {
			return fmt.Errorf("admin: query audit: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var rowID int64
			var adminID, action, at string
			var params, srcIP sql.NullString
			if err := rows.Scan(&rowID, &adminID, &action, &params, &srcIP, &at); err != nil {
				return fmt.Errorf("admin: scan audit: %w", err)
			}
			items = append(items, map[string]any{
				"id": rowID, "adminId": adminID, "action": action,
				"params": jsonOrNil(params), "srcIp": srcIP.String, "at": at,
			})
		}
		return rows.Err()
	})
	if qErr != nil {
		s.writeErr(w, asAPIError(qErr))
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

// hRelayMetrics returns relay connection/reservation/forward counts and reject
// reasons (admin.md §1/§6, server.md R6 — read-only aggregate, no payload).
// The relay role is stateless and coord-independent (server.md R5) so a
// deployment may run it as a separate instance; metrics are read through an
// injected RelayMetrics source. When none is wired the endpoint reports that
// relay metrics are scraped from the relay role's own metrics surface
// (server.md R6) rather than fabricating values.
func (s *Server) hRelayMetrics(w http.ResponseWriter, r *http.Request) {
	if s.relayMetrics == nil {
		s.writeJSON(w, http.StatusOK, map[string]any{
			"available": false,
			"note":      "relay role is decoupled (server.md R5); scrape relay metrics from its own metrics.listen surface (server.md R6)",
		})
		return
	}
	snap, err := s.relayMetrics.Snapshot(r.Context())
	if err != nil {
		s.writeErr(w, asAPIError(err))
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"available": true, "metrics": snap})
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// jsonOrNil returns the raw JSON text (request_events.detail / business_info /
// signers / admin_audit.params are stored as JSON TEXT) wrapped so it is
// emitted as a JSON value, or nil when absent.
func jsonOrNil(ns sql.NullString) any {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	return rawJSON(ns.String)
}
