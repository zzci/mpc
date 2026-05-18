-- coord 持久库初始 schema。由 goose 版本化迁移工具驱动（database.md §1：禁手改
-- schema，schema 变更经新增迁移文件 + goose 版本表 goose_db_version 跟踪）。
-- 类型映射 database.md §98：bytea→BLOB、uuid/text→TEXT、jsonb→TEXT(JSON)、
-- timestamptz→TEXT(RFC3339)、bigserial→INTEGER PK AUTOINCREMENT、text[]→TEXT(JSON)。
-- 约束以应用层 + CHECK 兜底（§98）；FK 列以注释标注，不启用 PRAGMA foreign_keys
-- （request_events 开通审计无对应 signing_requests 行，S-002 §51「request_events 风格」）。

-- +goose Up
CREATE TABLE groups (
    group_id     TEXT    PRIMARY KEY,
    ecdsa_pubkey BLOB    NOT NULL,                 -- 主公钥（公开；回传前验签）
    threshold_t  INTEGER NOT NULL,
    parties_n    INTEGER NOT NULL,
    group_pubkey BLOB    NOT NULL,                 -- 自主式信任锚：能力令牌验签公钥
    epoch        INTEGER NOT NULL DEFAULT 0,       -- S-002 §4.1/§97：reshare 单调计数
    created_at   TEXT    NOT NULL,
    updated_at   TEXT    NOT NULL
);

CREATE TABLE group_members (
    group_id        TEXT NOT NULL,                 -- FK -> groups.group_id
    member_id       TEXT NOT NULL,
    identity_pubkey BLOB NOT NULL,                 -- 成员身份公钥（验心跳/审批/上报）
    status          TEXT NOT NULL DEFAULT 'active'
                       CHECK (status IN ('active', 'removed')),
    PRIMARY KEY (group_id, member_id)
);

CREATE TABLE signing_requests (
    request_id    TEXT PRIMARY KEY,                -- 全局唯一，禁复用
    group_id      TEXT NOT NULL,                   -- FK -> groups.group_id
    chain         TEXT NOT NULL,
    unsigned_tx   BLOB NOT NULL,                   -- 不透明；整库加密承载静态加密
    digest32      BLOB NOT NULL CHECK (length(digest32) = 32),
    proposer      TEXT NOT NULL,
    business_info TEXT,                            -- 带外说明；整库加密承载静态加密
    meta_hash     BLOB NOT NULL,
    proposer_sig  BLOB NOT NULL,
    status        TEXT NOT NULL
                     CHECK (status IN ('PENDING','DISPATCHED','SIGNING','SIGNED',
                                       'RETURNED','EXPIRED','REJECTED','FAILED')),
    created_at    TEXT NOT NULL,
    expiry        TEXT NOT NULL,
    dispatched_at TEXT,
    signers       TEXT,                            -- JSON 数组（text[] 映射）
    result_rsv    BLOB,
    fail_reason   TEXT
);

CREATE TABLE request_approvals (
    request_id TEXT NOT NULL,                      -- FK -> signing_requests.request_id
    member_id  TEXT NOT NULL,
    decision   TEXT NOT NULL CHECK (decision IN ('approved','rejected')),
    sig        BLOB NOT NULL,
    decided_at TEXT NOT NULL,
    PRIMARY KEY (request_id, member_id)
);

CREATE TABLE request_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id  TEXT,                              -- 状态流水关联键；开通审计填 group_id
    from_status TEXT,
    to_status   TEXT,
    actor       TEXT NOT NULL,                     -- external / member:<id> / coord
    detail      TEXT,                              -- JSON；仅元数据，不记 unsigned_tx/分片
    at          TEXT NOT NULL
);

CREATE TABLE push_tokens (
    group_id   TEXT NOT NULL,                      -- FK -> group_members(group_id,member_id)
    member_id  TEXT NOT NULL,
    platform   TEXT NOT NULL CHECK (platform IN ('fcm','apns')),
    token      TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (group_id, member_id, platform)
);

CREATE TABLE admin_audit (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    admin_id TEXT NOT NULL,
    action   TEXT NOT NULL,
    params   TEXT,                                 -- JSON；不含 secret 明文
    src_ip   TEXT,
    at       TEXT NOT NULL
);

-- 索引（database.md §4 + §6 管理面检索）
CREATE INDEX idx_sr_group_status   ON signing_requests (group_id, status);
CREATE INDEX idx_sr_status_expiry  ON signing_requests (status, expiry);
CREATE INDEX idx_sr_expiry_active  ON signing_requests (expiry)
    WHERE status NOT IN ('RETURNED','EXPIRED','REJECTED','FAILED');
CREATE INDEX idx_sr_group_created  ON signing_requests (group_id, created_at);
CREATE INDEX idx_sr_proposer_created ON signing_requests (proposer, created_at);
CREATE INDEX idx_sr_status_created ON signing_requests (status, created_at);
CREATE INDEX idx_ra_request        ON request_approvals (request_id);
CREATE INDEX idx_re_request_at     ON request_events (request_id, at);
CREATE INDEX idx_pt_group_member   ON push_tokens (group_id, member_id);

-- +goose Down
DROP INDEX idx_pt_group_member;
DROP INDEX idx_re_request_at;
DROP INDEX idx_ra_request;
DROP INDEX idx_sr_status_created;
DROP INDEX idx_sr_proposer_created;
DROP INDEX idx_sr_group_created;
DROP INDEX idx_sr_expiry_active;
DROP INDEX idx_sr_status_expiry;
DROP INDEX idx_sr_group_status;
DROP TABLE admin_audit;
DROP TABLE push_tokens;
DROP TABLE request_events;
DROP TABLE request_approvals;
DROP TABLE signing_requests;
DROP TABLE group_members;
DROP TABLE groups;
