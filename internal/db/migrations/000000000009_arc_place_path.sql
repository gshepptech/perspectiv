-- +goose Up
-- place_path is an arc's place and every place above it, state first. A
-- place page lists arcs at the place and everything below it; finding them
-- by walking down a state's million descendants took seconds, while asking
-- which arcs carry the place in their path is one index lookup. Triggers
-- keep the path right: on an arc's insert, and on any arc below a place
-- whose parent changes (the place import re-files places).
ALTER TABLE arcs ADD COLUMN IF NOT EXISTS place_path uuid[] NOT NULL DEFAULT '{}';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION place_path_of(p uuid) RETURNS uuid[]
LANGUAGE sql STABLE AS $$
    WITH RECURSIVE up AS (
        SELECT id, parent_id, 0 AS depth FROM places WHERE id = p
        UNION ALL
        SELECT pl.id, pl.parent_id, up.depth + 1
        FROM places pl JOIN up ON pl.id = up.parent_id
        WHERE up.depth < 8
    )
    SELECT coalesce(array_agg(id ORDER BY depth DESC), '{}') FROM up;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION arcs_set_place_path() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.place_path := place_path_of(NEW.place_id);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS arcs_place_path ON arcs;
CREATE TRIGGER arcs_place_path BEFORE INSERT OR UPDATE OF place_id ON arcs
    FOR EACH ROW EXECUTE FUNCTION arcs_set_place_path();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION places_refresh_arc_paths() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.parent_id IS DISTINCT FROM OLD.parent_id THEN
        UPDATE arcs SET place_path = place_path_of(place_id) WHERE place_path @> ARRAY[NEW.id];
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS places_arc_paths ON places;
CREATE TRIGGER places_arc_paths AFTER UPDATE OF parent_id ON places
    FOR EACH ROW EXECUTE FUNCTION places_refresh_arc_paths();

UPDATE arcs SET place_path = place_path_of(place_id);
CREATE INDEX IF NOT EXISTS arcs_place_path_idx ON arcs USING gin (place_path);

-- A place's children list in name order, ignoring leading quotes and
-- symbols ("#1 Garage Door" files under G, not before A).
CREATE INDEX IF NOT EXISTS places_parent_sort_idx
    ON places (parent_id, (lower(regexp_replace(name, '^[^[:alnum:]]+', ''))), id)
    WHERE hidden_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS places_parent_sort_idx;
DROP INDEX IF EXISTS arcs_place_path_idx;
DROP TRIGGER IF EXISTS places_arc_paths ON places;
DROP FUNCTION IF EXISTS places_refresh_arc_paths();
DROP TRIGGER IF EXISTS arcs_place_path ON arcs;
DROP FUNCTION IF EXISTS arcs_set_place_path();
DROP FUNCTION IF EXISTS place_path_of(uuid);
ALTER TABLE arcs DROP COLUMN IF EXISTS place_path;
