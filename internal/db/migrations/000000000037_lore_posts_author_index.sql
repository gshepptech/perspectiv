-- +goose Up
-- A profile's Thread posts tab lists one writer's posts, newest first
-- (ListUserPosts), and nothing indexed lore_posts by author: every page read
-- the whole table. With 200k posts by 20k writers loaded into the dev
-- database in a rolled-back transaction, a first page took 54 ms on a
-- parallel sequential scan; with this index it reads the writer's ten
-- posts in order and takes 1.1 ms. The data export reads a member's posts
-- through it too.
CREATE INDEX IF NOT EXISTS lore_posts_author_created_idx ON lore_posts (author_id, created_at DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS lore_posts_author_created_idx;
