-- Migration v5: group_derived_addresses persists (group_id, child_index) ->
-- (evm_address, tron_address) for HD-derived child addresses that the owning
-- group actually started using (docs/design/mcp/address-derivation.md §7.bis,
-- Q-A/B/C/D user ruling 2026-05-20). Lazy (B-side B12), idempotent on
-- (group_id, child_index), and member-only on the read side; the FK lets a
-- consumer trace a derived address back to its source group, which is the
-- user requirement that motivated the table.
--
-- charter-10: 00001/00003/00004 must not be hand-edited; schema growth lands
-- in a NEW versioned migration. PRAGMA foreign_keys is not enabled in this
-- DB (00001 comment), so the FK is intent + tooling-readable; the
-- "no orphan / no group-delete" invariant is enforced jointly by the
-- application layer (RegisterDerivedAddress checks the parent group exists)
-- and by the design-level pubkey append-only rule (distributed-mpc §R7;
-- groups are never deleted), so a runtime ON DELETE event cannot occur.
-- The CHECK keeps child_index in non-hardened HD range [0, 2^31).
--
-- Down rebuilds the table because go-sqlcipher v4.4.2 embeds a SQLite older
-- than 3.35 (so ALTER TABLE DROP COLUMN is unsafe in general); since this
-- migration only adds a new table, Down can simply DROP it - no copy/rebuild
-- needed for the table itself - but we DROP the index first to mirror the
-- 00001 down style.

-- +goose Up
CREATE TABLE group_derived_addresses (
    group_id     TEXT    NOT NULL,                                  -- FK -> groups.group_id
    child_index  INTEGER NOT NULL CHECK (child_index >= 0 AND child_index < 2147483648),
    evm_address  TEXT    NOT NULL,
    tron_address TEXT    NOT NULL,
    child_pubkey BLOB,                                              -- optional SEC1 compressed (33B); audit aid
    created_at   INTEGER NOT NULL,                                  -- unix seconds
    PRIMARY KEY (group_id, child_index)
);
CREATE INDEX idx_gda_group ON group_derived_addresses(group_id);

-- +goose Down
DROP INDEX idx_gda_group;
DROP TABLE group_derived_addresses;
