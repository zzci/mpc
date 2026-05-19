-- Migration v3: retire push_tokens (single-fixed-webhook ruling
-- 2026-05-19). Notification is one fixed webhook; coord holds no push
-- tokens/credentials and no longer distinguishes fcm/apns, so the B2
-- register-push-token endpoint and its storage are removed. Schema change
-- goes through a NEW versioned migration (database.md §1: no hand-edited
-- schema); 00001_init.sql stays as originally finalized so an already
-- deployed DB (00001 applied) and a fresh DB converge to the same schema
-- after migrating up. Up/down/idempotent are tracked by goose_db_version.

-- +goose Up
DROP INDEX idx_pt_group_member;
DROP TABLE push_tokens;

-- +goose Down
-- Mirror the original 00001_init.sql push_tokens definition exactly so a
-- full rollback restores the pre-v3 schema byte-for-byte.
CREATE TABLE push_tokens (
    group_id   TEXT NOT NULL,                      -- FK -> group_members(group_id,member_id)
    member_id  TEXT NOT NULL,
    platform   TEXT NOT NULL CHECK (platform IN ('fcm','apns')),
    token      TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (group_id, member_id, platform)
);
CREATE INDEX idx_pt_group_member   ON push_tokens (group_id, member_id);
