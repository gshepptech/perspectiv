-- name: UpsertImported :batchone
-- Writes one imported place. Every imported row is keyed by (source,
-- source_id). Updates the row with this source id, else reuses the sibling with the same
-- normalised name, else inserts. outcome is updated, matched or inserted. A
-- matched row is left untouched: it belongs to its own source or its user.
-- hidden_at and user_added are never written, so a re-import does not unhide
-- a place an admin hid.
WITH by_source AS (
    UPDATE places AS u
    SET kind      = @kind::text,
        name      = @name::text,
        parent_id = sqlc.narg('parent_id')::uuid,
        timezone  = @timezone::text,
        lat       = sqlc.narg('lat')::float8,
        lng       = sqlc.narg('lng')::float8,
        category  = sqlc.narg('category')::text
    WHERE u.source = @source::text AND u.source_id = @source_id::text
    RETURNING u.id
), by_name AS (
    SELECT p.id
    FROM places p
    WHERE NOT EXISTS (SELECT 1 FROM by_source)
      AND p.parent_id IS NOT DISTINCT FROM sqlc.narg('parent_id')::uuid
      AND lower(regexp_replace(btrim(p.name), '\s+', ' ', 'g'))
          = lower(regexp_replace(btrim(@name::text), '\s+', ' ', 'g'))
), inserted AS (
    INSERT INTO places (kind, name, parent_id, timezone, lat, lng, source, source_id, category)
    SELECT @kind::text, @name::text, sqlc.narg('parent_id')::uuid, @timezone::text,
           sqlc.narg('lat')::float8, sqlc.narg('lng')::float8, @source::text, @source_id::text,
           sqlc.narg('category')::text
    WHERE NOT EXISTS (SELECT 1 FROM by_source)
      AND NOT EXISTS (SELECT 1 FROM by_name)
    RETURNING id
)
SELECT id, 'updated'::text AS outcome FROM by_source
UNION ALL
SELECT id, 'matched'::text AS outcome FROM by_name
UNION ALL
SELECT id, 'inserted'::text AS outcome FROM inserted;
