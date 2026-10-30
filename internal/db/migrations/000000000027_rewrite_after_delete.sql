-- +goose Up
-- A member who deletes their story in an arc may write there again: the
-- one-story-per-arc rule counts only stories that are not deleted. A story
-- a moderator hid is not deleted, so it still holds the place.
DROP INDEX IF EXISTS perspectives_arc_author_key;
CREATE UNIQUE INDEX IF NOT EXISTS perspectives_arc_author_key ON perspectives (arc_id, author_id) WHERE deleted_at IS NULL;

-- +goose Down
-- The full index cannot come back while a member has a deleted story and a
-- newer one in the same arc; say so rather than fail on a bare violation.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM perspectives GROUP BY arc_id, author_id HAVING count(*) > 1) THEN
        RAISE EXCEPTION 'cannot restore one row per (arc, author): members rewrote after deleting; remove the deleted rows first';
    END IF;
END $$;
-- +goose StatementEnd
DROP INDEX IF EXISTS perspectives_arc_author_key;
CREATE UNIQUE INDEX IF NOT EXISTS perspectives_arc_author_key ON perspectives (arc_id, author_id);
