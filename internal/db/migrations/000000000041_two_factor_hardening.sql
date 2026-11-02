-- +goose Up
-- A setup that is waiting for its first code counts its own wrong codes in
-- setup_failures, apart from two_factor_attempts (the sign-in lockout): the
-- fifth wrong code deletes the pending secret and setup starts again behind
-- the password. Starting setup again resets it.
ALTER TABLE totp_secrets ADD COLUMN IF NOT EXISTS setup_failures integer NOT NULL DEFAULT 0;

-- Recovery-code hashes are salted per account. hash_version says how
-- code_hash was made:
--   1  SHA-256 of "perspectiv.recovery:" and the normalised code (the rows
--      written before this migration, unsalted);
--   2  SHA-256 of "perspectiv.recovery.v2", a zero byte, the account's id
--      (16 bytes) and the normalised code.
-- Every new code is written as version 2. A version 1 code still works until
-- it is used or its set is replaced; once no unused version 1 row is left,
-- version 1 can be dropped.
ALTER TABLE recovery_codes ADD COLUMN IF NOT EXISTS hash_version smallint NOT NULL DEFAULT 1;
ALTER TABLE recovery_codes DROP CONSTRAINT IF EXISTS recovery_codes_hash_version_check;
ALTER TABLE recovery_codes ADD CONSTRAINT recovery_codes_hash_version_check CHECK (hash_version IN (1, 2));

-- +goose Down
-- Version 2 hashes cannot be turned back into version 1 without the codes,
-- which are never stored, so going down deletes them: those accounts keep
-- two-factor sign-in and make new recovery codes in Settings.
DELETE FROM recovery_codes WHERE hash_version = 2;
ALTER TABLE recovery_codes DROP CONSTRAINT IF EXISTS recovery_codes_hash_version_check;
ALTER TABLE recovery_codes DROP COLUMN IF EXISTS hash_version;
ALTER TABLE totp_secrets DROP COLUMN IF EXISTS setup_failures;
