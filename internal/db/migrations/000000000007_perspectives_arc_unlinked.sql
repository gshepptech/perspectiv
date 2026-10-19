-- +goose Up
-- The arc page's not-yet-linked list (ArcService/ListArcSection), oldest
-- first; this serves its keyset.
CREATE INDEX IF NOT EXISTS perspectives_arc_unlinked_idx
    ON perspectives (arc_id, published_at, id)
    WHERE status = 'PERSPECTIVE_STATUS_PUBLISHED'
      AND lore_status IN ('LORE_STATUS_PENDING', 'LORE_STATUS_IN_LORE');

-- +goose Down
DROP INDEX IF EXISTS perspectives_arc_unlinked_idx;
