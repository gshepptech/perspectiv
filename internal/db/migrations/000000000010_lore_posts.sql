-- +goose Up
-- A lore's thread: plain-text posts by its members and by anyone else with a
-- perspective in the arc. Read by anyone; oldest first.
CREATE TABLE IF NOT EXISTS lore_posts (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    lore_id    uuid        NOT NULL REFERENCES lores (id) ON DELETE CASCADE,
    author_id  uuid        NOT NULL REFERENCES users (id),
    body       text        NOT NULL CONSTRAINT lore_posts_body_check CHECK (char_length(btrim(body)) BETWEEN 1 AND 1000),
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz NULL,
    hidden_at  timestamptz NULL
);
CREATE INDEX IF NOT EXISTS lore_posts_lore_created_idx ON lore_posts (lore_id, created_at, id);

-- +goose Down
DROP TABLE IF EXISTS lore_posts;
