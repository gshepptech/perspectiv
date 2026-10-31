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
SELECT p.id, p.name, p.timezone,
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
-- The arc with its place chain, read in one walk up the places. The chain
-- is the arc's place and every ancestor, root (STATE) first, as three
-- arrays in the same order. place_hidden is true when the arc's place or
-- any of the seven places above it is hidden; the chain itself is not cut
-- off there.
WITH RECURSIVE up AS (
    SELECT pl.id, pl.parent_id, pl.kind, pl.name, pl.hidden_at, 1 AS depth
    FROM places pl
    JOIN arcs a0 ON a0.place_id = pl.id
    WHERE a0.id = @id::uuid
    UNION ALL
    SELECT parent.id, parent.parent_id, parent.kind, parent.name, parent.hidden_at, up.depth + 1
    FROM places parent
    JOIN up ON parent.id = up.parent_id
)
SELECT a.id, a.place_id, a.local_date, a.hour, a.parent_arc_id, a.title, a.created_by,
       a.canon_lore_id, a.canon_score, a.member_count, a.created_at,
       EXISTS (SELECT 1 FROM up WHERE up.depth <= 8 AND up.hidden_at IS NOT NULL)::bool AS place_hidden,
       u.username AS creator_username,
       (SELECT array_agg(up.id ORDER BY up.depth DESC) FROM up)::uuid[] AS chain_ids,
       (SELECT array_agg(up.kind ORDER BY up.depth DESC) FROM up)::text[] AS chain_kinds,
       (SELECT array_agg(up.name ORDER BY up.depth DESC) FROM up)::text[] AS chain_names
FROM arcs a
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

-- name: ListHourArcs :many
-- A day arc's hour children in hour order, a page at a time after
-- after_hour (NULL on the first page); served by arcs_parent_arc_id_hour_idx.
-- One hour arc exists per hour, so the hour alone is the cursor. Each hour
-- carries its count of published perspectives that are neither deleted nor
-- hidden, so a page of hours needs no count query per hour.
SELECT a.id, a.place_id, a.local_date, a.hour, a.parent_arc_id, a.title, a.created_by,
       a.canon_lore_id, a.canon_score, a.member_count, a.created_at,
       u.username AS creator_username,
       (SELECT count(*) FROM perspectives v
        WHERE v.arc_id = a.id AND v.status = 'PERSPECTIVE_STATUS_PUBLISHED'
          AND v.deleted_at IS NULL AND v.hidden_at IS NULL)::int AS perspective_count
FROM arcs a
JOIN users u ON u.id = a.created_by
WHERE a.parent_arc_id = sqlc.arg(parent_arc_id)::uuid
  AND (sqlc.narg(after_hour)::smallint IS NULL OR a.hour > sqlc.narg(after_hour)::smallint)
ORDER BY a.hour, a.id
LIMIT sqlc.arg(row_limit)::int;

-- name: ListArcTales :many
-- SOLO and REMOVED perspectives in the arc that are in no lore, oldest
-- first, keyset-paginated on (published_at, id); served by
-- perspectives_tales_idx scanned backwards.
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
WHERE pe.arc_id = sqlc.arg(arc_id)::uuid
  AND pe.status = 'PERSPECTIVE_STATUS_PUBLISHED'
  AND pe.lore_status IN ('LORE_STATUS_SOLO', 'LORE_STATUS_REMOVED')
  AND lm.perspective_id IS NULL  -- on the join column, so it plans as an anti-join
  AND (sqlc.narg(after_at)::timestamptz IS NULL
       OR (pe.published_at, pe.id) > (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY pe.published_at, pe.id
LIMIT sqlc.arg(row_limit)::int;

-- name: ListArcUnlinked :many
-- Published perspectives in the arc that are in no lore and are not tales:
-- PENDING, or IN_LORE before membership is recomputed. Oldest first,
-- keyset-paginated on (published_at, id); served by
-- perspectives_arc_unlinked_idx.
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
WHERE pe.arc_id = sqlc.arg(arc_id)::uuid
  AND pe.status = 'PERSPECTIVE_STATUS_PUBLISHED'
  AND pe.lore_status IN ('LORE_STATUS_PENDING', 'LORE_STATUS_IN_LORE')
  AND lm.perspective_id IS NULL  -- on the join column, so it plans as an anti-join
  AND (sqlc.narg(after_at)::timestamptz IS NULL
       OR (pe.published_at, pe.id) > (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY pe.published_at, pe.id
LIMIT sqlc.arg(row_limit)::int;

-- name: ListLorePerspectives :many
-- The published perspectives that are members of the given lores, oldest
-- first. A page of lores is small, so their members come in one query.
SELECT pe.id, pe.author_id, u.username AS author_username, pe.genre, pe.lore_status,
       lm.lore_id AS member_of,
       pe.published_at, pe.deleted_at, pe.hidden_at,
       COALESCE((SELECT left(pp.body, 241) FROM perspective_parts pp
                 WHERE pp.perspective_id = pe.id ORDER BY pp.position LIMIT 1), '')::text AS opening,
       (SELECT count(*) FROM perspective_parts pp WHERE pp.perspective_id = pe.id)::int AS part_count,
       (SELECT count(*) FROM votes v WHERE v.perspective_id = pe.id)::int AS upvotes
FROM lore_members lm
JOIN perspectives pe ON pe.id = lm.perspective_id
JOIN users u ON u.id = pe.author_id
WHERE lm.lore_id = ANY(sqlc.arg(lore_ids)::uuid[])
  AND pe.status = 'PERSPECTIVE_STATUS_PUBLISHED'
ORDER BY pe.published_at, pe.id;

-- name: ListLoresByID :many
-- The given lores, in no order: an arc page's canon, or the canon of every
-- hour on a page of hours in one query.
SELECT l.id, l.status, l.member_count, l.upvotes, l.rank_score, l.computed_at,
       coalesce(l.name, '')::text AS name, l.named_by, coalesce(nb.username, '')::text AS named_by_username
FROM lores l
LEFT JOIN users nb ON nb.id = l.named_by AND nb.deleted_at IS NULL
WHERE l.id = ANY(sqlc.arg(lore_ids)::uuid[]);

-- name: ListArcSublores :many
-- Every lore in the arc but the canon that has a member, highest rank
-- first, keyset-paginated on (rank_score DESC, id); served by
-- lores_arc_rank_idx. A lore whose group dissolved has nothing to read and
-- is not listed.
SELECT l.id, l.status, l.member_count, l.upvotes, l.rank_score, l.computed_at,
       coalesce(l.name, '')::text AS name, l.named_by, coalesce(nb.username, '')::text AS named_by_username
FROM lores l
LEFT JOIN users nb ON nb.id = l.named_by AND nb.deleted_at IS NULL
WHERE l.arc_id = sqlc.arg(arc_id)::uuid
  AND l.id IS DISTINCT FROM sqlc.narg(canon_id)::uuid
  AND EXISTS (SELECT 1 FROM lore_members lm WHERE lm.lore_id = l.id)
  AND (sqlc.narg(after_score)::float8 IS NULL
       OR l.rank_score < sqlc.narg(after_score)::float8
       OR (l.rank_score = sqlc.narg(after_score)::float8 AND l.id > sqlc.narg(after_id)::uuid))
ORDER BY l.rank_score DESC, l.id
LIMIT sqlc.arg(row_limit)::int;

-- name: ListLoreEncounters :many
-- Accepted claims linking two members of the same lore, for the given lores.
SELECT c.id AS claim_id, m1.lore_id, ch.perspective_id, c.claimant_perspective_id
FROM claims c
JOIN characters ch ON ch.id = c.character_id
JOIN lore_members m1 ON m1.perspective_id = ch.perspective_id
JOIN lore_members m2 ON m2.perspective_id = c.claimant_perspective_id AND m2.lore_id = m1.lore_id
WHERE m1.lore_id = ANY(sqlc.arg(lore_ids)::uuid[]) AND c.status = 'CLAIM_STATUS_ACCEPTED'
ORDER BY c.resolved_at, c.id;

-- name: ViewerInArc :one
-- The caller's readable published perspective in the arc and its lore.
SELECT pe.id, lm.lore_id AS member_of
FROM perspectives pe
LEFT JOIN lore_members lm ON lm.perspective_id = pe.id
WHERE pe.arc_id = sqlc.arg(arc_id)::uuid
  AND pe.author_id = sqlc.arg(author_id)::uuid
  AND pe.status = 'PERSPECTIVE_STATUS_PUBLISHED'
  AND pe.deleted_at IS NULL AND pe.hidden_at IS NULL;

-- name: ViewerVotes :many
-- Which of the given perspectives the user has upvoted.
SELECT perspective_id FROM votes
WHERE user_id = sqlc.arg(user_id)::uuid AND perspective_id = ANY(sqlc.arg(ids)::uuid[]);

-- name: ListOpenCharacters :many
-- Characters in the arc's readable stories with no pending or accepted
-- claim. A story voted out of its lore cannot be claimed from, and a deleted
-- author's stories are not shown.
SELECT c.id, c.label, c.perspective_id, p.author_id, u.username
FROM characters c
JOIN perspectives p ON p.id = c.perspective_id
JOIN users u ON u.id = p.author_id
WHERE p.arc_id = sqlc.arg(arc_id)::uuid
  AND p.status = 'PERSPECTIVE_STATUS_PUBLISHED'
  AND p.deleted_at IS NULL AND p.hidden_at IS NULL
  AND p.lore_status <> 'LORE_STATUS_REMOVED'
  AND u.deleted_at IS NULL
  AND NOT EXISTS (
        SELECT 1 FROM claims cl
        WHERE cl.character_id = c.id
          AND cl.status IN ('CLAIM_STATUS_PENDING', 'CLAIM_STATUS_ACCEPTED'))
ORDER BY p.published_at, p.id, c.position
LIMIT 200;

-- name: DayArcStories :many
-- A place's arcs on one day, the day arc first and then its hours in order,
-- each with its count of readable stories and the viewer's own story there. Empty when nobody has written
-- there that day.
SELECT a.id, a.hour, a.title,
       (SELECT count(*) FROM perspectives v
        WHERE v.arc_id = a.id AND v.status = 'PERSPECTIVE_STATUS_PUBLISHED'
          AND v.deleted_at IS NULL AND v.hidden_at IS NULL)::integer AS stories,
       -- The viewer's own story in the arc, draft or published (one each);
       -- the nil id when there is none.
       coalesce((SELECT v.id FROM perspectives v
                 WHERE v.arc_id = a.id AND v.author_id = sqlc.narg(viewer)::uuid AND v.deleted_at IS NULL
                 LIMIT 1), '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS mine
FROM arcs a
WHERE a.place_id = sqlc.arg(place_id)::uuid AND a.local_date = sqlc.arg(local_date)::date
ORDER BY a.hour NULLS FIRST;

-- name: SitemapArcs :many
-- Days and hours with a readable story, not under a hidden place, most
-- recently written first: the days listed in the sitemap.
SELECT a.id, max(v.published_at)::timestamptz AS last_mod
FROM arcs a
JOIN perspectives v ON v.arc_id = a.id
WHERE v.status = 'PERSPECTIVE_STATUS_PUBLISHED' AND v.deleted_at IS NULL AND v.hidden_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM places h WHERE h.id = ANY(a.place_path) AND h.hidden_at IS NOT NULL)
GROUP BY a.id
ORDER BY last_mod DESC, a.id
LIMIT @lim::integer;
