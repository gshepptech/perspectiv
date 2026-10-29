-- +goose Up
-- Invite-only beta. While SIGNUP_MODE is invite, signing up needs an access
-- code: an admin-issued beta code below, or a member's invite link code
-- (invites.code). A code can admit up to max_uses accounts until it expires
-- or is revoked.
CREATE TABLE IF NOT EXISTS access_codes (
    code       text        PRIMARY KEY CONSTRAINT access_codes_code_check CHECK (code ~ '^[a-z0-9-]{4,32}$'),
    note       text        NOT NULL DEFAULT '' CONSTRAINT access_codes_note_check CHECK (char_length(note) <= 200),
    max_uses   integer     NOT NULL CONSTRAINT access_codes_max_uses_check CHECK (max_uses BETWEEN 1 AND 10000),
    uses       integer     NOT NULL DEFAULT 0 CONSTRAINT access_codes_uses_check CHECK (uses BETWEEN 0 AND max_uses),
    created_by uuid        NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NULL,
    revoked_at timestamptz NULL
);

-- People asking to join while the beta is invite-only. An admin's invite
-- issues them a single-use code and emails it.
CREATE TABLE IF NOT EXISTS waitlist (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    email      text        NOT NULL CONSTRAINT waitlist_email_check CHECK (char_length(email) BETWEEN 3 AND 254),
    note       text        NOT NULL DEFAULT '' CONSTRAINT waitlist_note_check CHECK (char_length(note) <= 300),
    created_at timestamptz NOT NULL DEFAULT now(),
    invited_at timestamptz NULL,
    code       text        NULL REFERENCES access_codes (code)
);
CREATE UNIQUE INDEX IF NOT EXISTS waitlist_email_lower_key ON waitlist (lower(email));
CREATE INDEX IF NOT EXISTS waitlist_created_idx ON waitlist (created_at, id);

-- How an account got in: the access code or invite code it signed up with;
-- NULL when sign-up was open.
ALTER TABLE users ADD COLUMN IF NOT EXISTS joined_with text NULL;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS joined_with;
DROP TABLE IF EXISTS waitlist;
DROP TABLE IF EXISTS access_codes;
