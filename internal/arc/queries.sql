-- name: GetPlaceForArc :one
-- The place and whether it or any ancestor is hidden: hiding a place hides
-- its whole subtree.
WITH RECURSIVE up AS (
    SELECT pl.id, pl.parent_id, pl.hidden_at, 1 AS depth
    FROM places pl
    WHERE pl.id = @id::uuid
    UNION ALL
    SELECT parent.id, parent.parent_id, parent.hidden_at, up.depth + 1
    FROM places parent
    JOIN up ON parent.id = up.parent_id
    WHERE up.depth < 8
)
SELECT p.id, p.name,
       EXISTS (SELECT 1 FROM up WHERE up.hidden_at IS NOT NULL)::bool AS subtree_hidden
FROM places p
WHERE p.id = @id::uuid;

-- name: GetDayArc :one
SELECT id, title
FROM arcs
WHERE place_id = $1 AND local_date = $2 AND hour IS NULL;

-- name: GetHourArc :one
SELECT id, title
FROM arcs
WHERE place_id = $1 AND local_date = $2 AND hour = $3;

-- name: InsertArc :one
-- A concurrent creator that committed first wins; this insert then returns no
-- row and the caller reads the winner's row.
INSERT INTO arcs (place_id, local_date, hour, parent_arc_id, title, created_by)
VALUES ($1, $2, sqlc.narg('hour'), sqlc.narg('parent_arc_id'), $3, $4)
ON CONFLICT (place_id, local_date, hour) DO NOTHING
RETURNING id;

-- name: GetArcRow :one
-- place_hidden is true when the arc's place or any ancestor is hidden.
WITH RECURSIVE up AS (
    SELECT pl.id, pl.parent_id, pl.hidden_at, 1 AS depth
    FROM places pl
    JOIN arcs a0 ON a0.place_id = pl.id
    WHERE a0.id = @id::uuid
    UNION ALL
    SELECT parent.id, parent.parent_id, parent.hidden_at, up.depth + 1
    FROM places parent
    JOIN up ON parent.id = up.parent_id
    WHERE up.depth < 8
)
SELECT a.id, a.place_id, a.local_date, a.hour, a.parent_arc_id, a.title, a.created_by,
       a.canon_lore_id, a.canon_score, a.member_count, a.created_at,
       p.name AS place_name,
       EXISTS (SELECT 1 FROM up WHERE up.hidden_at IS NOT NULL)::bool AS place_hidden,
       u.username AS creator_username
FROM arcs a
JOIN places p ON p.id = a.place_id
JOIN users u ON u.id = a.created_by
WHERE a.id = @id::uuid;

-- name: LockArcForRetitle :one
WITH RECURSIVE up AS (
    SELECT pl.id, pl.parent_id, pl.hidden_at, 1 AS depth
    FROM places pl
    JOIN arcs a0 ON a0.place_id = pl.id
    WHERE a0.id = @id::uuid
    UNION ALL
    SELECT parent.id, parent.parent_id, parent.hidden_at, up.depth + 1
    FROM places parent
    JOIN up ON parent.id = up.parent_id
    WHERE up.depth < 8
)
SELECT a.id, a.hour, a.local_date, a.created_by, p.name AS place_name,
       EXISTS (SELECT 1 FROM up WHERE up.hidden_at IS NOT NULL)::bool AS place_hidden
FROM arcs a
JOIN places p ON p.id = a.place_id
WHERE a.id = @id::uuid
FOR UPDATE OF a;

-- name: UpdateArcTitle :exec
UPDATE arcs SET title = $2 WHERE id = $1;

-- name: PlaceChain :many
-- The arc's place and its ancestors, root (STATE) first.
WITH RECURSIVE chain AS (
    SELECT pl.id, pl.kind, pl.name, pl.parent_id, 0 AS depth
    FROM places pl
    WHERE pl.id = $1
    UNION ALL
    SELECT parent.id, parent.kind, parent.name, parent.parent_id, chain.depth + 1
    FROM places parent
    JOIN chain ON parent.id = chain.parent_id
)
SELECT chain.id, chain.kind, chain.name
FROM chain
ORDER BY chain.depth DESC;

-- name: ListHourArcs :many
SELECT a.id, a.place_id, a.local_date, a.hour, a.parent_arc_id, a.title, a.created_by,
       a.canon_lore_id, a.canon_score, a.member_count, a.created_at,
       u.username AS creator_username
FROM arcs a
JOIN users u ON u.id = a.created_by
WHERE a.parent_arc_id = $1
ORDER BY a.hour, a.id;

-- name: ListArcPerspectives :many
-- Every published perspective in the arc, oldest first, with the lore it is a
-- member of (from lore_members) and the opening of its first part.
SELECT pe.id, pe.author_id, u.username AS author_username, pe.genre, pe.lore_status,
       lm.lore_id AS member_of,
       pe.published_at, pe.deleted_at, pe.hidden_at,
       COALESCE((SELECT left(pp.body, 241) FROM perspective_parts pp
                 WHERE pp.perspective_id = pe.id ORDER BY pp.position LIMIT 1), '')::text AS opening,
       (SELECT count(*) FROM perspective_parts pp WHERE pp.perspective_id = pe.id)::int AS part_count,
       (SELECT count(*) FROM votes v WHERE v.perspective_id = pe.id)::int AS upvotes
FROM perspectives pe
JOIN users u ON u.id = pe.author_id
LEFT JOIN lore_members lm ON lm.perspective_id = pe.id
WHERE pe.arc_id = $1 AND pe.status = 'PERSPECTIVE_STATUS_PUBLISHED'
ORDER BY pe.published_at, pe.id;

-- name: ListArcLores :many
SELECT id, status, member_count, upvotes, rank_score, computed_at
FROM lores
WHERE arc_id = $1
ORDER BY rank_score DESC, id;

-- name: ListArcEncounters :many
-- Accepted claims linking two members of the same lore in the arc.
SELECT c.id AS claim_id, m1.lore_id, ch.perspective_id, c.claimant_perspective_id
FROM claims c
JOIN characters ch ON ch.id = c.character_id
JOIN lore_members m1 ON m1.perspective_id = ch.perspective_id
JOIN lore_members m2 ON m2.perspective_id = c.claimant_perspective_id AND m2.lore_id = m1.lore_id
JOIN lores l ON l.id = m1.lore_id
WHERE l.arc_id = $1 AND c.status = 'CLAIM_STATUS_ACCEPTED'
ORDER BY c.resolved_at, c.id;
