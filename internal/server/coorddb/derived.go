package coorddb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// AD-6 group_derived_addresses persistence (docs/design/mcp/address-derivation.md
// §7.bis, Q-A/B/C/D user ruling 2026-05-20). Lazy (B-side B12) registration of
// the HD-derived (group_id, child_index) -> (evm, tron) bindings actually
// started by an owning member, so that a consumer can trace any derived
// address back to its source group (the user's stated motivation).
//
// Concurrency: writes go through Store.WithTx (BEGIN IMMEDIATE on the single
// writer connection, database.md §5), reads are single-shot QueryContext on
// the same connection. LOCKED is enforced by Store: WithTx/conn return
// ErrLocked, which X-001 maps to 503 LOCKED.

// hdChildIndexBound matches the schema CHECK in 00005_group_derived_addresses.sql:
// non-hardened HD range [0, 2^31) (address-derivation.md §2/§F2; uint32 upper
// bound is 2^31 for non-hardened derivation).
const hdChildIndexBound = 1 << 31

// ErrDerivedAddressConflict means a (group_id, child_index) is already
// registered with addresses that do not match the new submission (api.md
// B12: identical → idempotent 200; mismatch → 409 STATE_CONFLICT). The
// repo layer surfaces this as a distinct sentinel so the HTTP handler can
// map it cleanly without parsing strings.
var ErrDerivedAddressConflict = errors.New("coorddb: derived address conflicts with existing (group_id, child_index)")

// ErrDerivedGroupMissing means RegisterDerivedAddress was called for a
// group_id that has no parent row in `groups`. PRAGMA foreign_keys is not
// enabled (00001 comment), so the parent existence check lives in this
// repo path as an application-layer FK enforcement.
var ErrDerivedGroupMissing = errors.New("coorddb: derived address references a non-existent group")

// DerivedAddressRecord is one group_derived_addresses row. ChildPubkey is
// optional (NULL when the caller did not submit one; address-derivation.md
// §7.bis.1: SEC1 compressed 33B, audit aid).
type DerivedAddressRecord struct {
	GroupID     string
	ChildIndex  uint32
	EVMAddress  string
	TronAddress string
	ChildPubkey []byte // optional; nil when unknown
	CreatedAt   int64  // unix seconds
}

// RegisterDerivedAddress inserts a (group_id, child_index) -> (evm, tron)
// binding atomically with a parent-group existence check (application-layer
// FK; PRAGMA foreign_keys is off, see 00001). If a row already exists for
// (group_id, child_index) and the EVM+TRON addresses match, the call is a
// no-op (idempotent 200 per api.md B12); a mismatch returns
// ErrDerivedAddressConflict so the handler can map it to 409 STATE_CONFLICT.
// child_pubkey is treated as an audit aid only: it is stored on first
// register and ignored on idempotent re-registers.
func (s *Store) RegisterDerivedAddress(ctx context.Context, r DerivedAddressRecord) error {
	if r.ChildIndex >= hdChildIndexBound {
		return fmt.Errorf("coorddb: child_index %d out of non-hardened range [0, 2^31)", r.ChildIndex)
	}
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		// Parent FK check (PRAGMA off; application-layer enforcement).
		var exists int
		if err := tx.QueryRowContext(ctx,
			`SELECT 1 FROM groups WHERE group_id = ?`, r.GroupID).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrDerivedGroupMissing
			}
			return fmt.Errorf("coorddb: check parent group: %w", err)
		}

		// Conflict check: an existing row with mismatched addresses is the
		// 409 STATE_CONFLICT path; a perfect match is idempotent.
		var existingEVM, existingTron string
		err := tx.QueryRowContext(ctx,
			`SELECT evm_address, tron_address
			   FROM group_derived_addresses
			  WHERE group_id = ? AND child_index = ?`, r.GroupID, r.ChildIndex).
			Scan(&existingEVM, &existingTron)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// fresh insert below
		case err != nil:
			return fmt.Errorf("coorddb: check existing derived: %w", err)
		default:
			if existingEVM == r.EVMAddress && existingTron == r.TronAddress {
				return nil // idempotent re-register
			}
			return ErrDerivedAddressConflict
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO group_derived_addresses
			 (group_id, child_index, evm_address, tron_address, child_pubkey, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			r.GroupID, r.ChildIndex, r.EVMAddress, r.TronAddress,
			nullableBlob(r.ChildPubkey), r.CreatedAt); err != nil {
			return fmt.Errorf("coorddb: insert derived address: %w", err)
		}
		return nil
	})
}

// ListDerivedAddresses returns the group's registered derived addresses with
// created_at strictly greater than sinceSec, ordered by (created_at,
// child_index) so the `since` cursor pages monotonically (api.md B12 GET
// shape). sinceSec=0 returns the full set. The query is single-shot on the
// store's connection; LOCKED fails closed via Store.conn -> ErrLocked.
func (s *Store) ListDerivedAddresses(ctx context.Context, groupID string, sinceSec int64) ([]DerivedAddressRecord, error) {
	db, err := s.conn()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		`SELECT group_id, child_index, evm_address, tron_address, child_pubkey, created_at
		   FROM group_derived_addresses
		  WHERE group_id = ? AND created_at > ?
		  ORDER BY created_at, child_index`, groupID, sinceSec)
	if err != nil {
		return nil, fmt.Errorf("coorddb: list derived: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []DerivedAddressRecord
	for rows.Next() {
		var rec DerivedAddressRecord
		var childPub []byte
		if err := rows.Scan(&rec.GroupID, &rec.ChildIndex, &rec.EVMAddress,
			&rec.TronAddress, &childPub, &rec.CreatedAt); err != nil {
			return nil, fmt.Errorf("coorddb: scan derived: %w", err)
		}
		rec.ChildPubkey = childPub
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("coorddb: iterate derived: %w", err)
	}
	return out, nil
}
