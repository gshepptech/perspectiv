-- +goose Up
-- A profile lists one author's readable published perspectives, newest
-- first (PerspectiveService/ListByAuthor); this serves its keyset.
CREATE INDEX IF NOT EXISTS perspectives_author_published_idx
    ON perspectives (author_id, published_at DESC, id DESC)
    WHERE status = 'PERSPECTIVE_STATUS_PUBLISHED' AND deleted_at IS NULL AND hidden_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS perspectives_author_published_idx;
