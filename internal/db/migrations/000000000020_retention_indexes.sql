-- +goose Up
-- The daily retention pass deletes by age; these let it find old rows
-- without reading whole tables.
CREATE INDEX IF NOT EXISTS login_attempts_attempted_at_idx ON login_attempts (attempted_at);
CREATE INDEX IF NOT EXISTS email_tokens_expires_at_idx ON email_tokens (expires_at);
CREATE INDEX IF NOT EXISTS sessions_expires_at_idx ON sessions (expires_at);

-- +goose Down
DROP INDEX IF EXISTS sessions_expires_at_idx;
DROP INDEX IF EXISTS email_tokens_expires_at_idx;
DROP INDEX IF EXISTS login_attempts_attempted_at_idx;
