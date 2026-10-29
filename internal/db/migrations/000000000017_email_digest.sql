-- +goose Up
-- A daily email digest instead of one email per event. While email_digest
-- is on, each email-worthy event becomes a digest item; the digest job sends
-- one email of a user's items at most once a day and deletes them.
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_digest boolean NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS digest_sent_at timestamptz NULL;

CREATE TABLE IF NOT EXISTS digest_items (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- The notification email's data: kind, link path, arc title, actor.
    data       jsonb       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS digest_items_user_idx ON digest_items (user_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS digest_items;
ALTER TABLE users DROP COLUMN IF EXISTS digest_sent_at;
ALTER TABLE users DROP COLUMN IF EXISTS email_digest;
