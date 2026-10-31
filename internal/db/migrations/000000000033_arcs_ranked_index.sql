-- +goose Up
-- The front page's "Where people are writing" and the everywhere listings
-- read arcs across every place in rank or newest order. Without an index in
-- that order each page sorted every arc: 55 ms warm and 368 ms cold, spilling
-- to disk, at beta scale (90k days); with it, 0.5 ms.
CREATE INDEX IF NOT EXISTS arcs_ranked_idx
    ON arcs ((canon_lore_id IS NOT NULL) DESC, canon_score DESC, member_count DESC, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS arcs_newest_idx ON arcs (created_at DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS arcs_newest_idx;
DROP INDEX IF EXISTS arcs_ranked_idx;
