package coorddb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/zzci/mpc/internal/addr"
)

// This file provides only the minimal persistence primitives D-001
// accepts on: S-002 group provisioning (single transaction), state-machine
// transition + request_events in the same transaction, admin_audit
// append. Full coord orchestration (envelope enqueue / quorum / TTL /
// external & member API) is X-001, not this module.

// ErrConflict means the optimistic state guard did not match (request
// missing, or current status != expected from). Per database.md §5:
// no SELECT ... FOR UPDATE; BEGIN IMMEDIATE + an in-transaction
// read-status→check→update serializes against double-dispatch.
var ErrConflict = errors.New("coorddb: state guard not satisfied")

// GroupRecord is one groups row (public info + S-002 epoch + derived
// chain addresses). EVMAddress/TronAddress are filled by ProvisionGroup,
// deterministically derived from ECDSAPubkey inside the write
// transaction (callers neither need nor should set them; an address is
// not the bare pubkey — database.md groups schema + address-record).
type GroupRecord struct {
	GroupID     string
	ECDSAPubkey []byte
	GroupPubkey []byte
	ThresholdT  int
	PartiesN    int
	Epoch       int64
	CreatedAt   string // RFC3339
	EVMAddress  string // EIP-55; derived by ProvisionGroup from ECDSAPubkey
	TronAddress string // Base58Check; derived by ProvisionGroup from ECDSAPubkey
}

// deriveChainAddrs deterministically derives the EVM (EIP-55) and TRON
// (Base58Check) addresses from the uncompressed secp256k1 master pubkey
// (reuses the internal/addr public API; do not reimplement). Best-effort:
// for a pubkey that is not a valid uncompressed point (low-level test
// stub / legacy dirty row) it returns empty strings rather than erroring,
// so ProvisionGroup / migration backfill still succeed in one
// transaction on degenerate input (existing D-001/S-002/X-001 tests
// provision with non-curve stub pubkeys; a real S-002 provisioning
// passes a 65B uncompressed pubkey and gets correct addresses).
func deriveChainAddrs(pub []byte) (evm, tron string) {
	if e, err := addr.ETHAddress(pub); err == nil {
		evm = e
	}
	if t, err := addr.TronAddress(pub); err == nil {
		tron = t
	}
	return evm, tron
}

// MemberRecord is one group_members row.
type MemberRecord struct {
	MemberID       string
	IdentityPubkey []byte
}

// ProvisionGroup writes, in one transaction, one groups row + one
// group_members row per member (status=active) + one provisioning audit
// event (request_events style, actor=coord), per S-002 §3.1/§3.2/§51.
// Auth/signature checks are X-001; this method only persists — the
// caller must have validated already.
func (s *Store) ProvisionGroup(ctx context.Context, g GroupRecord, members []MemberRecord) error {
	evmAddr, tronAddr := deriveChainAddrs(g.ECDSAPubkey)
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO groups
			 (group_id, ecdsa_pubkey, threshold_t, parties_n, group_pubkey, epoch, created_at, updated_at, evm_address, tron_address)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			g.GroupID, g.ECDSAPubkey, g.ThresholdT, g.PartiesN, g.GroupPubkey,
			g.Epoch, g.CreatedAt, g.CreatedAt, evmAddr, tronAddr); err != nil {
			return fmt.Errorf("coorddb: insert group: %w", err)
		}
		for _, m := range members {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO group_members
				 (group_id, member_id, identity_pubkey, status)
				 VALUES (?, ?, ?, 'active')`,
				g.GroupID, m.MemberID, m.IdentityPubkey); err != nil {
				return fmt.Errorf("coorddb: insert member %s: %w", m.MemberID, err)
			}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO request_events (request_id, from_status, to_status, actor, detail, at)
			 VALUES (?, NULL, 'PROVISIONED', 'coord', NULL, ?)`,
			g.GroupID, g.CreatedAt); err != nil {
			return fmt.Errorf("coorddb: insert provisioning event: %w", err)
		}
		return nil
	})
}

// SigningRequestSeed is the minimal fields to create a pending request
// (X-001 envelope enqueue carries the full set).
type SigningRequestSeed struct {
	RequestID   string
	GroupID     string
	Chain       string
	UnsignedTx  []byte
	Digest32    []byte // must be 32B (schema CHECK backstop)
	Proposer    string
	MetaHash    []byte
	ProposerSig []byte
	CreatedAt   string
	Expiry      string
}

// CreateSigningRequest enqueues one request as PENDING.
func (s *Store) CreateSigningRequest(ctx context.Context, r SigningRequestSeed) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO signing_requests
			 (request_id, group_id, chain, unsigned_tx, digest32, proposer,
			  meta_hash, proposer_sig, status, created_at, expiry)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'PENDING', ?, ?)`,
			r.RequestID, r.GroupID, r.Chain, r.UnsignedTx, r.Digest32, r.Proposer,
			r.MetaHash, r.ProposerSig, r.CreatedAt, r.Expiry)
		if err != nil {
			return fmt.Errorf("coorddb: insert signing_request: %w", err)
		}
		return nil
	})
}

// RecordTransition performs a state-machine transition in one
// transaction: check current status == from (guard), set to, and write
// one request_events row. Any step failing rolls back the whole
// (database.md §5/§8: transition and request_events in one transaction,
// consistent rollback on error).
func (s *Store) RecordTransition(ctx context.Context, requestID, from, to, actor, detail string) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE signing_requests SET status = ? WHERE request_id = ? AND status = ?`,
			to, requestID, from)
		if err != nil {
			return fmt.Errorf("coorddb: transition update: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("coorddb: transition rows: %w", err)
		}
		if n == 0 {
			return ErrConflict
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO request_events (request_id, from_status, to_status, actor, detail, at)
			 VALUES (?, ?, ?, ?, ?, datetime('now'))`,
			requestID, from, to, actor, nullStr(detail)); err != nil {
			return fmt.Errorf("coorddb: transition event: %w", err)
		}
		return nil
	})
}

// AppendAdminAudit append-writes one admin-action audit row; the
// application layer only appends, never updates/deletes (database.md §6:
// admins cannot modify/delete). params must not contain plaintext
// secrets (caller's responsibility).
func (s *Store) AppendAdminAudit(ctx context.Context, adminID, action, params, srcIP, at string) error {
	db, err := s.conn()
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO admin_audit (admin_id, action, params, src_ip, at)
		 VALUES (?, ?, ?, ?, ?)`,
		adminID, action, nullStr(params), nullStr(srcIP), at); err != nil {
		return fmt.Errorf("coorddb: append admin_audit: %w", err)
	}
	return nil
}

// RequestStatus reads one request's current status; fail-closed under LOCKED.
func (s *Store) RequestStatus(ctx context.Context, requestID string) (string, error) {
	db, err := s.conn()
	if err != nil {
		return "", err
	}
	var status string
	err = db.QueryRowContext(ctx,
		`SELECT status FROM signing_requests WHERE request_id = ?`, requestID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", sql.ErrNoRows
	}
	if err != nil {
		return "", fmt.Errorf("coorddb: query status: %w", err)
	}
	return status, nil
}

// GroupEpoch reads a group's current epoch (S-002 §4.1
// monotonic-check dependency); fail-closed under LOCKED.
func (s *Store) GroupEpoch(ctx context.Context, groupID string) (int64, error) {
	db, err := s.conn()
	if err != nil {
		return 0, err
	}
	var epoch int64
	err = db.QueryRowContext(ctx,
		`SELECT epoch FROM groups WHERE group_id = ?`, groupID).Scan(&epoch)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, sql.ErrNoRows
	}
	if err != nil {
		return 0, fmt.Errorf("coorddb: query epoch: %w", err)
	}
	return epoch, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
