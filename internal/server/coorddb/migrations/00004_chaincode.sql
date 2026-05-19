-- Migration v4: groups gains `chaincode` (BLOB(32) NULL) for HD address
-- derivation (docs/design/mcp/address-derivation.md §4 / charter-10:
-- never hand-edit 00001/00002/00003, schema change goes through a NEW
-- versioned migration). chaincode is the 32-byte commit-reveal output
-- (§3) persisted in the same transaction as ecdsa_pubkey /
-- evm_address / tron_address. NULL is reserved for legacy groups
-- created before this migration (F5: legacy groups stay single-address
-- and non-HD; chaincode cannot be back-injected without reshare,
-- which the design forbids). The CHECK keeps the BLOB(32) intent at
-- the storage layer: either NULL or exactly 32 bytes.
--
-- Down rebuilds the table because go-sqlcipher v4.4.2 embeds a SQLite
-- older than 3.35, so ALTER TABLE DROP COLUMN cannot be relied on;
-- groups has no indexes/triggers and PRAGMA foreign_keys is not
-- enabled (00001_init.sql), so a full-table rebuild is
-- version-independent and safe. The recreated DDL must mirror the
-- post-00003 schema (base columns + evm_address + tron_address) so
-- that goose convergence on a re-up is byte-equivalent to the fresh
-- chain.

-- +goose Up
ALTER TABLE groups ADD COLUMN chaincode BLOB
    CHECK (chaincode IS NULL OR length(chaincode) = 32);

-- +goose Down
ALTER TABLE groups RENAME TO groups_g002_drop;
CREATE TABLE groups (
    group_id     TEXT    PRIMARY KEY,
    ecdsa_pubkey BLOB    NOT NULL,
    threshold_t  INTEGER NOT NULL,
    parties_n    INTEGER NOT NULL,
    group_pubkey BLOB    NOT NULL,
    epoch        INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT    NOT NULL,
    updated_at   TEXT    NOT NULL,
    evm_address  TEXT    NOT NULL DEFAULT '',
    tron_address TEXT    NOT NULL DEFAULT ''
);
INSERT INTO groups
    (group_id, ecdsa_pubkey, threshold_t, parties_n, group_pubkey, epoch,
     created_at, updated_at, evm_address, tron_address)
SELECT
    group_id, ecdsa_pubkey, threshold_t, parties_n, group_pubkey, epoch,
    created_at, updated_at, evm_address, tron_address
FROM groups_g002_drop;
DROP TABLE groups_g002_drop;
