-- name: UpsertImported :batchone
-- Writes one imported place. Every imported row is keyed by (source,
-- source_id). Updates the row with this source id, else reuses the sibling with the same
-- normalised name, else inserts. outcome is updated, matched, inserted or
-- kept. A matched row is left untouched: it belongs to its own source or its
-- user.
-- An update whose name, under its parent, would collide with another row on
-- places_parent_name_key (a cleaned name that now equals a sibling's) keeps
-- the row's own name and parent and reports kept, so one collision never
-- fails the import; the other row is not touched either.
-- An update never turns a MALL back into a STORE: an admin's MakeVenue, or
-- an earlier import, may have given it places inside.
-- hidden_at and user_added are never written, so a re-import does not unhide
-- a place an admin hid.
WITH clash AS (
    SELECT EXISTS (
        SELECT 1
        FROM places o
        WHERE o.parent_id IS NOT DISTINCT FROM sqlc.narg('parent_id')::uuid
          AND lower(regexp_replace(btrim(o.name), '\s+', ' ', 'g'))
              = lower(regexp_replace(btrim(@name::text), '\s+', ' ', 'g'))
          AND (o.source <> @source::text OR o.source_id IS DISTINCT FROM @source_id::text)
    ) AS taken
), by_source AS (
    UPDATE places AS u
    SET kind      = CASE WHEN u.kind = 'PLACE_KIND_MALL' AND @kind::text = 'PLACE_KIND_STORE'
                         THEN u.kind ELSE @kind::text END,
        name      = CASE WHEN c.taken THEN u.name ELSE @name::text END,
        parent_id = CASE WHEN c.taken THEN u.parent_id ELSE sqlc.narg('parent_id')::uuid END,
        timezone  = CASE WHEN c.taken THEN u.timezone ELSE @timezone::text END,
        lat       = sqlc.narg('lat')::float8,
        lng       = sqlc.narg('lng')::float8,
        category  = sqlc.narg('category')::text
    FROM clash c
    WHERE u.source = @source::text AND u.source_id = @source_id::text
    RETURNING u.id, c.taken
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
SELECT id, (CASE WHEN taken THEN 'kept' ELSE 'updated' END)::text AS outcome FROM by_source
UNION ALL
SELECT id, 'matched'::text AS outcome FROM by_name
UNION ALL
SELECT id, 'inserted'::text AS outcome FROM inserted;

-- name: ListDedupeCandidates :many
-- Every visible store and mall with coordinates, for placectl dedupe: with
-- its arcs, the perspectives filed at it (drafts and deleted rows included),
-- the names of the places above it, and when it was added. A place under a
-- hidden ancestor is not visible and is left out.
WITH arc_counts AS (
    SELECT a.place_id, count(*) AS n FROM arcs a GROUP BY a.place_id
), perspective_counts AS (
    SELECT s.place_id, count(DISTINCT s.id) AS n
    FROM (
        SELECT v.id, v.place_id FROM perspectives v WHERE v.place_id IS NOT NULL
        UNION ALL
        SELECT v.id, a.place_id FROM perspectives v JOIN arcs a ON a.id = v.arc_id
    ) s
    GROUP BY s.place_id
)
SELECT p.id,
       p.kind,
       p.name,
       coalesce(p.category, '')::text AS category,
       p.lat::float8 AS lat,
       p.lng::float8 AS lng,
       coalesce(ac.n, 0)::integer AS arcs,
       coalesce(pc.n, 0)::integer AS perspectives,
       coalesce((SELECT array_agg(an.name ORDER BY array_position(p.path, an.id))
                 FROM places an WHERE an.id = ANY(p.path) AND an.id <> p.id), '{}')::text[] AS ancestor_names
FROM places p
LEFT JOIN arc_counts ac ON ac.place_id = p.id
LEFT JOIN perspective_counts pc ON pc.place_id = p.id
WHERE p.kind IN ('PLACE_KIND_STORE', 'PLACE_KIND_MALL')
  AND p.hidden_at IS NULL
  AND p.lat IS NOT NULL AND p.lng IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM places h WHERE h.id = ANY(p.path) AND h.hidden_at IS NOT NULL)
ORDER BY p.id;

-- name: CheckDedupeMove :one
-- Whether the places under loser can move under survivor: the shallowest
-- level among loser's children (0 for none; STATE is 1, STORE 5), the
-- survivor's level, how many children share a name with one of the
-- survivor's, and whether survivor sits inside loser.
SELECT
    coalesce((SELECT min(array_position(ARRAY['PLACE_KIND_STATE', 'PLACE_KIND_CITY', 'PLACE_KIND_TOWN',
                                              'PLACE_KIND_MALL', 'PLACE_KIND_STORE']::text[], c.kind))
              FROM places c WHERE c.parent_id = @loser_id::uuid), 0)::integer AS child_level,
    coalesce((SELECT array_position(ARRAY['PLACE_KIND_STATE', 'PLACE_KIND_CITY', 'PLACE_KIND_TOWN',
                                          'PLACE_KIND_MALL', 'PLACE_KIND_STORE']::text[], s.kind)
              FROM places s WHERE s.id = @survivor_id::uuid), 0)::integer AS survivor_level,
    (SELECT count(*) FROM places c JOIN places t
        ON t.parent_id = @survivor_id::uuid
       AND lower(regexp_replace(btrim(t.name), '\s+', ' ', 'g'))
           = lower(regexp_replace(btrim(c.name), '\s+', ' ', 'g'))
     WHERE c.parent_id = @loser_id::uuid)::integer AS name_clashes,
    EXISTS (SELECT 1 FROM places s WHERE s.id = @survivor_id::uuid AND s.path @> ARRAY[@loser_id::uuid])::boolean
        AS survivor_inside;

-- name: LockDedupePlaces :many
-- Locks the visible places of one merge against a concurrent write. The lock
-- also holds off a new arc or draft at them until the merge commits, since
-- their foreign keys must share-lock the row.
SELECT p.id FROM places p
WHERE p.id = ANY(@ids::uuid[]) AND p.hidden_at IS NULL
ORDER BY p.id
FOR UPDATE;

-- name: HideDedupeLoser :execrows
-- Hides a duplicate, unless it holds an arc or a perspective row of any
-- status: nothing with stories or drafts is ever hidden by the dedupe.
UPDATE places p SET hidden_at = now()
WHERE p.id = @id::uuid AND p.hidden_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM arcs a WHERE a.place_id = p.id)
  AND NOT EXISTS (SELECT 1 FROM perspectives v WHERE v.place_id = p.id);

-- name: MoveDedupeChildren :execrows
UPDATE places SET parent_id = @into_id::uuid WHERE parent_id = @from_id::uuid;

-- name: MoveDedupeFollows :exec
-- Moves a duplicate's followers to the place that stays; a user who already
-- follows it keeps the one follow.
WITH moved AS (
    DELETE FROM place_follows WHERE place_id = @from_id::uuid
    RETURNING user_id, created_at
)
INSERT INTO place_follows (user_id, place_id, created_at)
SELECT m.user_id, @into_id::uuid, m.created_at FROM moved m
ON CONFLICT (user_id, place_id) DO NOTHING;

-- name: InsertDedupeAudit :exec
-- The audit row of one merge. No admin acted, so actor_id is NULL.
INSERT INTO moderation_audit (actor_id, action, target_kind, target_id, reason)
VALUES (NULL, @action::text, @target_kind::text, @target_id::uuid, @reason::text);
