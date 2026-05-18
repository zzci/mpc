package coorddb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/zzci/mpc/internal/addr"
)

// 本文件仅提供 D-001 验收所需的最小持久化原语：S-002 组开通落库（单事务）、
// 状态机迁移 + request_events 同事务、admin_audit 追加写。完整 coord 编排
// （信封入列/法定人数/TTL/外部&成员 API）属 X-001，不在本模块。

// ErrConflict 表示乐观状态守卫未命中（请求不存在或当前状态 != 期望 from）。
// 对齐 database.md §5：无 SELECT … FOR UPDATE，靠 BEGIN IMMEDIATE + 事务内
// 「读状态→校验→改」串行化防双发。
var ErrConflict = errors.New("coorddb: state guard not satisfied")

// GroupRecord 是 groups 一行（公开信息 + S-002 epoch + 派生链地址）。
// EVMAddress/TronAddress 由 ProvisionGroup 在落库事务内自 ECDSAPubkey 确定性
// 派生填写（调用方无须、也不应自行设置；地址≠单纯公钥，docs/design/server/database.md
// groups schema + 地址记录小节）。
type GroupRecord struct {
	GroupID     string
	ECDSAPubkey []byte
	GroupPubkey []byte
	ThresholdT  int
	PartiesN    int
	Epoch       int64
	CreatedAt   string // RFC3339
	EVMAddress  string // EIP-55；ProvisionGroup 自 ECDSAPubkey 派生
	TronAddress string // Base58Check；ProvisionGroup 自 ECDSAPubkey 派生
}

// deriveChainAddrs 自未压缩 secp256k1 主公钥确定性派生 EVM(EIP-55) 与 TRON
// (Base58Check) 地址（复用 internal/addr 公开 API，勿重实现）。best-effort：
// 公钥非合法未压缩点（如低层测试桩 / 历史脏行）时返回空串而非报错，使
// ProvisionGroup / 迁移 backfill 在退化输入下仍单事务成功（既有 D-001/S-002/
// X-001 测试以非曲线桩公钥开通；真实 S-002 开通传 65B 未压缩公钥得正确地址）。
func deriveChainAddrs(pub []byte) (evm, tron string) {
	if e, err := addr.ETHAddress(pub); err == nil {
		evm = e
	}
	if t, err := addr.TronAddress(pub); err == nil {
		tron = t
	}
	return evm, tron
}

// MemberRecord 是 group_members 一行。
type MemberRecord struct {
	MemberID       string
	IdentityPubkey []byte
}

// ProvisionGroup 单事务写 groups 一行 + 每成员一行 group_members(status=active)
// + 一条开通审计事件（request_events 风格，actor=coord），对齐 S-002 §3.1/§3.2/§51。
// 鉴权/签名校验属 X-001，本方法只做落库；调用方须已校验。
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

// SigningRequestSeed 是创建待签请求所需的最小字段（X-001 信封入列会承载完整字段）。
type SigningRequestSeed struct {
	RequestID   string
	GroupID     string
	Chain       string
	UnsignedTx  []byte
	Digest32    []byte // 必须 32B（schema CHECK 兜底）
	Proposer    string
	MetaHash    []byte
	ProposerSig []byte
	CreatedAt   string
	Expiry      string
}

// CreateSigningRequest 以 PENDING 入列一条待签请求。
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

// RecordTransition 在单事务内执行状态机迁移：校验当前状态 == from（守卫），
// 改为 to，并写一条 request_events。任一步失败整体回滚（database.md §5/§8：
// 状态机迁移与 request_events 同事务，异常回滚一致）。
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

// AppendAdminAudit 追加写一条管理操作审计；应用层仅 append、不提供改/删
// （database.md §6：管理员不可改/删）。params 不得含 secret 明文（调用方保证）。
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

// RequestStatus 读单个请求的当前状态；LOCKED 下 fail-closed。
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

// GroupEpoch 读组的当前 epoch（S-002 §4.1 单调校验依赖）；LOCKED 下 fail-closed。
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
