-- +goose Up
-- path is a place and every place above it, state first, as arcs.place_path
-- is for an arc. A search inside a state asked for everything below it by
-- walking down 509k descendants (1.6 s at beta scale); asking which places
-- carry the state in their path is one index lookup. Triggers keep it
-- right: a place takes its parent's path plus itself, and a place whose
-- parent changes rewrites the paths below it.
ALTER TABLE places ADD COLUMN IF NOT EXISTS path uuid[] NOT NULL DEFAULT '{}';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION places_set_path() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.parent_id IS NULL THEN
        NEW.path := ARRAY[NEW.id];
    ELSE
        NEW.path := (SELECT p.path FROM places p WHERE p.id = NEW.parent_id) || NEW.id;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS places_path ON places;
CREATE TRIGGER places_path BEFORE INSERT OR UPDATE OF parent_id ON places
    FOR EACH ROW EXECUTE FUNCTION places_set_path();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION places_refresh_child_paths() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.parent_id IS DISTINCT FROM OLD.parent_id THEN
        UPDATE places
        SET path = NEW.path || path[array_position(path, NEW.id) + 1:]
        WHERE path @> ARRAY[NEW.id] AND id <> NEW.id;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS places_child_paths ON places;
CREATE TRIGGER places_child_paths AFTER UPDATE OF parent_id ON places
    FOR EACH ROW EXECUTE FUNCTION places_refresh_child_paths();

-- Backfill top-down in one pass rather than walking up from every place.
WITH RECURSIVE tree AS (
    SELECT id, ARRAY[id] AS path FROM places WHERE parent_id IS NULL
    UNION ALL
    SELECT c.id, tree.path || c.id FROM places c JOIN tree ON c.parent_id = tree.id
)
UPDATE places p SET path = tree.path FROM tree WHERE p.id = tree.id AND p.path IS DISTINCT FROM tree.path;

CREATE INDEX IF NOT EXISTS places_path_idx ON places USING gin (path);

-- +goose Down
DROP INDEX IF EXISTS places_path_idx;
DROP TRIGGER IF EXISTS places_child_paths ON places;
DROP FUNCTION IF EXISTS places_refresh_child_paths();
DROP TRIGGER IF EXISTS places_path ON places;
DROP FUNCTION IF EXISTS places_set_path();
ALTER TABLE places DROP COLUMN IF EXISTS path;
