-- +goose Up
-- A profile's Thread posts tab reads only posts a reader can see. Indexing
-- every post let an author make each public page of their own tab walk
-- all their deleted posts first (a cost they control, bounded only by the
-- posting limit), so the index now holds live posts only. The data export,
-- which reads deleted posts too, is rate-limited and rare.
DROP INDEX IF EXISTS lore_posts_author_created_idx;
CREATE INDEX IF NOT EXISTS lore_posts_author_live_idx ON lore_posts (author_id, created_at DESC, id DESC)
    WHERE deleted_at IS NULL AND hidden_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS lore_posts_author_live_idx;
CREATE INDEX IF NOT EXISTS lore_posts_author_created_idx ON lore_posts (author_id, created_at DESC, id DESC);
