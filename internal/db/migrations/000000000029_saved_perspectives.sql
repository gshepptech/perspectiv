-- +goose Up
-- Stories a member saved to read later. A save outlives its story becoming
-- unreadable (deleted, hidden, or at a hidden place): the Saved list leaves
-- such a story out and shows it again once it is restored.
CREATE TABLE IF NOT EXISTS saved_perspectives (
    user_id        uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    perspective_id uuid        NOT NULL REFERENCES perspectives (id) ON DELETE CASCADE,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, perspective_id)
);
-- The Saved list reads one member's saves, newest first, keyset-paginated on
-- (created_at, perspective_id).
CREATE INDEX IF NOT EXISTS saved_perspectives_user_created_idx
    ON saved_perspectives (user_id, created_at DESC, perspective_id DESC);

-- +goose Down
DROP INDEX IF EXISTS saved_perspectives_user_created_idx;
DROP TABLE IF EXISTS saved_perspectives;
