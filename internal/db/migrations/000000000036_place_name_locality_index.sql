-- +goose Up
-- A one- or two-letter typeahead ranked every place whose name starts with
-- it to keep the best 100: "a" read 35,600 index entries and sorted them,
-- 11 to 15 ms warm at 640k places, and "a" under Florida 150 to 190 ms. The
-- ranking puts exact names first, then states, cities and towns before
-- malls and stores, so the search now reads the states and cities, then the
-- towns, from this index, and the malls and stores only when those leave
-- room. Its leading kind lets each step read only its own kinds' names, and
-- the path it carries lets a search under a parent keep to the index
-- instead of intersecting with every place under the parent.
CREATE INDEX IF NOT EXISTS places_name_locality_idx
    ON places (kind, lower(name) text_pattern_ops) INCLUDE (name, id, path)
    WHERE hidden_at IS NULL AND kind IN ('PLACE_KIND_STATE', 'PLACE_KIND_CITY', 'PLACE_KIND_TOWN');

-- +goose Down
DROP INDEX IF EXISTS places_name_locality_idx;
