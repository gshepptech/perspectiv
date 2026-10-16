-- Accounts, sessions, email tokens, login attempts, rate-limit counters and
-- blocks. Enumerated columns are text holding the full proto value name and a
-- CHECK listing exactly the non-zero values of the proto enum. The app is
-- single-tenant: no table carries a tenant key.

-- +goose Up
CREATE TABLE IF NOT EXISTS users (
    id                  uuid        PRIMARY KEY DEFAULT uuidv7(),
    email               text        NOT NULL,
    email_verified_at   timestamptz NULL,
    password_hash       text        NOT NULL,
    username            text        NOT NULL,
    username_changed_at timestamptz NULL,
    avatar_path         text        NULL,
    bio                 text        NOT NULL DEFAULT '',
    role                text        NOT NULL DEFAULT 'ROLE_USER'
                                    CONSTRAINT users_role_check CHECK (role IN ('ROLE_USER', 'ROLE_ADMIN')),
    age_confirmed_at    timestamptz NOT NULL,
    suspended_at        timestamptz NULL,
    deleted_at          timestamptz NULL,
    created_at          timestamptz NOT NULL DEFAULT now()
);

-- Email and username are unique without regard to case.
CREATE UNIQUE INDEX IF NOT EXISTS users_email_lower_key ON users (lower(email));
CREATE UNIQUE INDEX IF NOT EXISTS users_username_lower_key ON users (lower(username));

-- A session is one sign-in; its refresh tokens form a rotation chain.
CREATE TABLE IF NOT EXISTS sessions (
    id           uuid        PRIMARY KEY DEFAULT uuidv7(),
    user_id      uuid        NOT NULL REFERENCES users (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz NULL
);

CREATE INDEX IF NOT EXISTS sessions_user_id_idx ON sessions (user_id);

-- Only a hash of each refresh token is stored. parent_id links a rotated token
-- to the one it replaced; presenting a token whose rotated_at is set is reuse.
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id          uuid        PRIMARY KEY DEFAULT uuidv7(),
    session_id  uuid        NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    parent_id   uuid        NULL REFERENCES refresh_tokens (id) ON DELETE CASCADE,
    token_hash  bytea       NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    rotated_at  timestamptz NULL,
    revoked_at  timestamptz NULL,
    CONSTRAINT refresh_tokens_token_hash_key UNIQUE (token_hash)
);

CREATE INDEX IF NOT EXISTS refresh_tokens_session_id_idx ON refresh_tokens (session_id);
CREATE INDEX IF NOT EXISTS refresh_tokens_parent_id_idx ON refresh_tokens (parent_id);

-- Single-use email verification and password reset tokens, stored hashed.
CREATE TABLE IF NOT EXISTS email_tokens (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    user_id    uuid        NOT NULL REFERENCES users (id),
    purpose    text        NOT NULL
                           CONSTRAINT email_tokens_purpose_check CHECK (purpose IN ('verify_email', 'password_reset')),
    token_hash bytea       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz NULL,
    CONSTRAINT email_tokens_token_hash_key UNIQUE (token_hash)
);

CREATE INDEX IF NOT EXISTS email_tokens_user_id_idx ON email_tokens (user_id, purpose);

-- Every login attempt against an existing account; the lockout counts the
-- failures in a trailing window per account.
CREATE TABLE IF NOT EXISTS login_attempts (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      uuid        NOT NULL REFERENCES users (id),
    succeeded    boolean     NOT NULL,
    attempted_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS login_attempts_user_id_attempted_at_idx
    ON login_attempts (user_id, attempted_at DESC);

-- Fixed-window rate-limit counters keyed by action and subject
-- ('user:<uuid>' or 'ip:<addr>').
CREATE TABLE IF NOT EXISTS rate_limit_counters (
    action       text        NOT NULL,
    subject      text        NOT NULL,
    window_start timestamptz NOT NULL,
    count        integer     NOT NULL DEFAULT 0,
    PRIMARY KEY (action, subject, window_start)
);

CREATE INDEX IF NOT EXISTS rate_limit_counters_window_start_idx ON rate_limit_counters (window_start);

-- blocker_id has blocked blocked_id. Blocking affects interaction only.
CREATE TABLE IF NOT EXISTS blocks (
    blocker_id uuid        NOT NULL REFERENCES users (id),
    blocked_id uuid        NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (blocker_id, blocked_id),
    CONSTRAINT blocks_not_self_check CHECK (blocker_id <> blocked_id)
);

-- +goose Down
DROP TABLE IF EXISTS blocks;
DROP TABLE IF EXISTS rate_limit_counters;
DROP TABLE IF EXISTS login_attempts;
DROP TABLE IF EXISTS email_tokens;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
