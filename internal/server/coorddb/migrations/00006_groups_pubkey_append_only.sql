-- Migration v6: R7 deep-defense triggers — groups.ecdsa_pubkey is
-- append-only (distributed-mpc.md R7, impl §E user ruling 2026-05-19).
--
-- The application-layer guard in coorddb.repo.go (guardR7AppendOnly) is
-- the primary refusal point; these SQLite triggers are the second layer
-- that catches any future writer that bypasses the helper (raw UPDATE /
-- DELETE / mistakenly typed migration code). Together they form the
-- "two layers, both refuse" pattern of §E.
--
-- charter-10: 00001/00003/00004/00005 must not be hand-edited; schema
-- growth lands in a NEW versioned migration. 00005 is occupied by
-- group_derived_addresses (AD-6); this DM-4 migration takes 00006 to
-- preserve numbering continuity.
--
-- goose splits SQL on ';' unless wrapped in StatementBegin/StatementEnd,
-- so the CREATE TRIGGER bodies (which contain embedded ';') are wrapped
-- below; goose then sends each trigger as one statement to the driver.

-- +goose Up
-- +goose StatementBegin
CREATE TRIGGER trg_groups_ecdsa_pubkey_append_only
BEFORE UPDATE OF ecdsa_pubkey ON groups
WHEN OLD.ecdsa_pubkey IS NOT NULL
 AND (NEW.ecdsa_pubkey IS NULL OR NEW.ecdsa_pubkey != OLD.ecdsa_pubkey)
BEGIN
    SELECT RAISE(ABORT, 'R7: groups.ecdsa_pubkey is append-only');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_groups_ecdsa_pubkey_no_delete
BEFORE DELETE ON groups
WHEN OLD.ecdsa_pubkey IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'R7: groups row with ecdsa_pubkey is non-deletable');
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_groups_ecdsa_pubkey_no_delete;
DROP TRIGGER IF EXISTS trg_groups_ecdsa_pubkey_append_only;
