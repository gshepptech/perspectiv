-- +goose Up
-- place_category is the category of an arc's own place, copied onto the arc
-- so the "best days at sports and recreation places" listing walks an index
-- in rank order instead of sorting every arc: at beta scale (90k arcs, 225k
-- stories) sorting took 90 ms a page. It also answers "which categories have
-- a story?" with one index probe per category. NULL when the place has no
-- category. Triggers keep the copy right: on an arc's insert or move, and on
-- every arc at a place whose category changes (the place import re-files
-- categories).
ALTER TABLE arcs ADD COLUMN IF NOT EXISTS place_category text;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION arcs_set_place_category() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.place_category := (SELECT p.category FROM places p WHERE p.id = NEW.place_id);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS arcs_place_category ON arcs;
CREATE TRIGGER arcs_place_category BEFORE INSERT OR UPDATE OF place_id ON arcs
    FOR EACH ROW EXECUTE FUNCTION arcs_set_place_category();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION places_refresh_arc_category() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.category IS DISTINCT FROM OLD.category THEN
        UPDATE arcs SET place_category = NEW.category WHERE place_id = NEW.id;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS places_arc_category ON places;
CREATE TRIGGER places_arc_category AFTER UPDATE OF category ON places
    FOR EACH ROW EXECUTE FUNCTION places_refresh_arc_category();

UPDATE arcs a SET place_category = p.category
FROM places p
WHERE p.id = a.place_id AND a.place_category IS DISTINCT FROM p.category;

-- A category's days in rank order and newest first (EverywhereArcsRanked,
-- EverywhereArcsNewest and the place listings with a category).
CREATE INDEX IF NOT EXISTS arcs_category_ranked_idx
    ON arcs (place_category, (canon_lore_id IS NOT NULL) DESC, canon_score DESC, member_count DESC, created_at DESC, id DESC)
    WHERE place_category IS NOT NULL;
CREATE INDEX IF NOT EXISTS arcs_category_newest_idx
    ON arcs (place_category, created_at DESC, id DESC)
    WHERE place_category IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS arcs_category_newest_idx;
DROP INDEX IF EXISTS arcs_category_ranked_idx;
DROP TRIGGER IF EXISTS places_arc_category ON places;
DROP FUNCTION IF EXISTS places_refresh_arc_category();
DROP TRIGGER IF EXISTS arcs_place_category ON arcs;
DROP FUNCTION IF EXISTS arcs_set_place_category();
ALTER TABLE arcs DROP COLUMN IF EXISTS place_category;
