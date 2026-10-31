-- +goose Up
-- The typeahead ranks every visible place whose name starts with what was
-- typed before it keeps the best 100, and a two-letter start such as "th"
-- matches 21,000 places. Ranked from places_name_prefix_idx, each match
-- was a heap read: 70 ms warm, 375 ms cold, at 640k places. This index
-- carries the name, kind and id the ranking reads, so the same ranking is an
-- index-only scan: 3 to 8 ms warm. It replaces places_name_prefix_idx,
-- whose only reader was the typeahead.
CREATE INDEX IF NOT EXISTS places_name_rank_idx
    ON places (lower(name) text_pattern_ops) INCLUDE (name, kind, id)
    WHERE hidden_at IS NULL;
DROP INDEX IF EXISTS places_name_prefix_idx;

-- +goose Down
CREATE INDEX IF NOT EXISTS places_name_prefix_idx ON places (lower(name) text_pattern_ops);
DROP INDEX IF EXISTS places_name_rank_idx;
