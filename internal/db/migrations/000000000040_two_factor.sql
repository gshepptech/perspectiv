-- +goose Up
-- Two-factor sign-in with an authenticator app (TOTP, RFC 6238).
--
-- totp_secrets holds one row per account that has started or finished
-- setting it up. The shared secret is encrypted through pkg/crypto and stored
-- as (key_id, algorithm, ciphertext); it is never stored in plain text.
-- confirmed_at is NULL while setup waits for the member's first code, and
-- two-factor sign-in is on only once it is set. last_used_step is the newest
-- 30-second step a code was accepted for: a code for that step or an older
-- one is refused, so a code cannot be replayed.
--
-- These rows are credentials, not content a member wrote, so they carry no
-- deleted_at: turning two-factor off or deleting the account deletes them.
CREATE TABLE IF NOT EXISTS totp_secrets (
    user_id        uuid        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    key_id         text        NOT NULL CHECK (key_id <> ''),
    algorithm      text        NOT NULL CHECK (algorithm <> ''),
    ciphertext     bytea       NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    confirmed_at   timestamptz NULL,
    last_used_step bigint      NULL
);

-- Ten single-use recovery codes per account, stored only as SHA-256 hashes
-- of their normalised form. A code carries 80 random bits.
CREATE TABLE IF NOT EXISTS recovery_codes (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash  bytea       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    used_at    timestamptz NULL,
    UNIQUE (user_id, code_hash)
);

-- The short-lived, single-use step between a correct password and a
-- session: Login answers with the token whose hash is stored here, and the
-- second factor is sent with it.
CREATE TABLE IF NOT EXISTS two_factor_challenges (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash bytea       NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz NULL
);

CREATE INDEX IF NOT EXISTS two_factor_challenges_user_idx ON two_factor_challenges (user_id);

-- Every second-factor code checked, kept like login_attempts: the
-- two-factor lockout counts the failures in it.
CREATE TABLE IF NOT EXISTS two_factor_attempts (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    succeeded    boolean     NOT NULL,
    attempted_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS two_factor_attempts_user_attempted_idx
    ON two_factor_attempts (user_id, attempted_at DESC);

-- An operator removing a member's two-factor sign-in from the command line
-- (perspectiv admin reset-two-factor) is an audited admin action.
ALTER TABLE moderation_audit DROP CONSTRAINT IF EXISTS moderation_audit_action_check;
ALTER TABLE moderation_audit ADD CONSTRAINT moderation_audit_action_check CHECK (action IN (
    'AUDIT_ACTION_HIDE_PERSPECTIVE', 'AUDIT_ACTION_HIDE_PLACE', 'AUDIT_ACTION_SUSPEND_USER',
    'AUDIT_ACTION_DISMISS', 'AUDIT_ACTION_HIDE_LORE_POST', 'AUDIT_ACTION_CLEAR_LORE_NAME',
    'AUDIT_ACTION_ADMIN_GRANT', 'AUDIT_ACTION_ACCESS_CODE_CREATE', 'AUDIT_ACTION_ACCESS_CODE_REVOKE',
    'AUDIT_ACTION_WAITLIST_INVITE', 'AUDIT_ACTION_USER_LOOKUP', 'AUDIT_ACTION_LIFT_SUSPENSION', 'AUDIT_ACTION_RESTORE',
    'AUDIT_ACTION_MAKE_VENUE', 'AUDIT_ACTION_MERGE_PLACE', 'AUDIT_ACTION_TWO_FACTOR_RESET'));

-- +goose Down
-- Two-factor sign-in goes with its tables, so every account signs in with
-- its password alone again; the reset audit rows go with the action name.
ALTER TABLE moderation_audit DISABLE TRIGGER moderation_audit_append_only;
DELETE FROM moderation_audit WHERE action = 'AUDIT_ACTION_TWO_FACTOR_RESET';
ALTER TABLE moderation_audit ENABLE TRIGGER moderation_audit_append_only;
ALTER TABLE moderation_audit DROP CONSTRAINT IF EXISTS moderation_audit_action_check;
ALTER TABLE moderation_audit ADD CONSTRAINT moderation_audit_action_check CHECK (action IN (
    'AUDIT_ACTION_HIDE_PERSPECTIVE', 'AUDIT_ACTION_HIDE_PLACE', 'AUDIT_ACTION_SUSPEND_USER',
    'AUDIT_ACTION_DISMISS', 'AUDIT_ACTION_HIDE_LORE_POST', 'AUDIT_ACTION_CLEAR_LORE_NAME',
    'AUDIT_ACTION_ADMIN_GRANT', 'AUDIT_ACTION_ACCESS_CODE_CREATE', 'AUDIT_ACTION_ACCESS_CODE_REVOKE',
    'AUDIT_ACTION_WAITLIST_INVITE', 'AUDIT_ACTION_USER_LOOKUP', 'AUDIT_ACTION_LIFT_SUSPENSION', 'AUDIT_ACTION_RESTORE',
    'AUDIT_ACTION_MAKE_VENUE', 'AUDIT_ACTION_MERGE_PLACE'));
DROP TABLE IF EXISTS two_factor_attempts;
DROP TABLE IF EXISTS two_factor_challenges;
DROP TABLE IF EXISTS recovery_codes;
DROP TABLE IF EXISTS totp_secrets;
