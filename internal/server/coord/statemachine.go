package coord

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/royqta/mcp-wallet/internal/server/coorddb"
)

// The C3 state machine (docs/design/server/server.md C3). Every transition is
// guarded (current status must equal `from`) and written together with its
// request_events row in one transaction — D-001's RecordTransition for plain
// transitions, dispatchTx/resultTx here for the two that also set extra
// columns (signers/dispatched_at, result_rsv/fail_reason). The guard is the
// optimistic concurrency control: a lost guard (ErrConflict) means another
// event already advanced the request, which is benign and ignored by the
// engine.
const (
	stPending    = "PENDING"
	stDispatched = "DISPATCHED"
	stSigning    = "SIGNING"
	stSigned     = "SIGNED"
	stReturned   = "RETURNED"
	stExpired    = "EXPIRED"
	stRejected   = "REJECTED"
	stFailed     = "FAILED"
)

// allowed is the C3 transition set. Rollback edges (DISPATCHED/SIGNING ->
// PENDING) implement "signer dropped, still < terminal and not expired ->
// re-schedule".
var allowed = map[string]map[string]bool{
	stPending:    {stDispatched: true, stExpired: true, stRejected: true},
	stDispatched: {stSigning: true, stPending: true, stExpired: true, stFailed: true},
	stSigning:    {stSigned: true, stPending: true, stExpired: true, stFailed: true},
	stSigned:     {stReturned: true},
}

func canTransition(from, to string) bool { return allowed[from][to] }

// isTerminal reports the C3 terminal states (docs/design/server/server.md C3:210).
func isTerminal(s string) bool {
	switch s {
	case stReturned, stExpired, stRejected, stFailed:
		return true
	default:
		return false
	}
}

// transition applies a guarded plain transition via D-001 RecordTransition.
// errConflict is returned (not wrapped) when the guard misses so the engine
// can treat it as "already advanced".
func (c *Coord) transition(ctx context.Context, requestID, from, to, actor, detail string) error {
	if !canTransition(from, to) {
		return fmt.Errorf("coord: illegal transition %s->%s", from, to)
	}
	err := c.store.RecordTransition(ctx, requestID, from, to, actor, detail)
	if errors.Is(err, coorddb.ErrConflict) {
		return errConflict
	}
	return err
}

// errConflict signals a missed status guard (request already advanced).
var errConflict = errors.New("coord: transition guard missed")

// dispatchTx atomically guards PENDING, moves to DISPATCHED, persists the
// chosen signers and dispatched_at, and writes the request_events row — all in
// one BEGIN IMMEDIATE transaction (D-001 RecordTransition cannot set the extra
// columns, and coorddb is unmodifiable).
func (c *Coord) dispatchTx(ctx context.Context, requestID string, signers []string, dispatchedAtMs int64, atISO string) error {
	return c.store.WithTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE signing_requests
			    SET status = 'DISPATCHED', signers = ?, dispatched_at = ?
			  WHERE request_id = ? AND status = 'PENDING'`,
			signersJSON(signers), msText(dispatchedAtMs), requestID)
		if err != nil {
			return fmt.Errorf("coord: dispatch update: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("coord: dispatch rows: %w", err)
		}
		if n == 0 {
			return errConflict
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO request_events (request_id, from_status, to_status, actor, detail, at)
			 VALUES (?, 'PENDING', 'DISPATCHED', 'coord', ?, ?)`,
			requestID, signersJSON(signers), atISO); err != nil {
			return fmt.Errorf("coord: dispatch event: %w", err)
		}
		return nil
	})
}

// resultTx atomically guards `from`, moves to `to`, persists result_rsv and/or
// fail_reason, and writes the request_events row in one transaction.
func (c *Coord) resultTx(ctx context.Context, requestID, from, to string, rsv []byte, failReason, atISO string) error {
	if !canTransition(from, to) {
		return fmt.Errorf("coord: illegal transition %s->%s", from, to)
	}
	return c.store.WithTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE signing_requests
			    SET status = ?, result_rsv = COALESCE(?, result_rsv), fail_reason = ?
			  WHERE request_id = ? AND status = ?`,
			to, nullBytes(rsv), nullStr(failReason), requestID, from)
		if err != nil {
			return fmt.Errorf("coord: result update: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("coord: result rows: %w", err)
		}
		if n == 0 {
			return errConflict
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO request_events (request_id, from_status, to_status, actor, detail, at)
			 VALUES (?, ?, ?, 'coord', ?, ?)`,
			requestID, from, to, nullStr(failReason), atISO); err != nil {
			return fmt.Errorf("coord: result event: %w", err)
		}
		return nil
	})
}

func nullBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
