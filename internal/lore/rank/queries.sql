-- Rank job queries. This package is the only writer of lores.status,
-- lores.rank_score, lores.upvotes, arcs.canon_lore_id, arcs.canon_score,
-- arcs.member_count and canon_changes. Enumerated columns hold the full proto
-- value name; callers pass them as text parameters.

-- name: DirtyArcs :many
-- Arcs marked for the rank job, keyset paged by id so one pass ends even
-- while writers keep marking arcs.
SELECT id
FROM arcs
WHERE lore_dirty_at IS NOT NULL
  AND id > sqlc.arg(after)::uuid
ORDER BY id
LIMIT sqlc.arg(row_limit)::int;

-- name: ArcDirtyMark :one
-- The arc's current mark; the pass clears exactly this value and no later one.
SELECT lore_dirty_at FROM arcs WHERE id = $1;

-- name: ArcLoreTallies :many
-- Every lore in the arc with its confirmed member count and the up-votes on
-- its members' perspectives. A votes row is an up-vote; there is no downvote.
SELECT l.id, l.status, l.member_count,
       (SELECT count(*)
        FROM lore_members m
        JOIN votes v ON v.perspective_id = m.perspective_id
        WHERE m.lore_id = l.id)::int AS upvotes
FROM lores l
WHERE l.arc_id = $1
ORDER BY l.id;

-- name: StoreLoreScores :exec
UPDATE lores l
SET upvotes = s.upvotes, rank_score = s.rank_score, computed_at = now()
FROM (SELECT unnest(sqlc.arg(ids)::uuid[])        AS id,
             unnest(sqlc.arg(upvotes)::int[])     AS upvotes,
             unnest(sqlc.arg(scores)::float8[])   AS rank_score) s
WHERE l.id = s.id
  AND l.arc_id = sqlc.arg(arc_id)::uuid;

-- name: SetArcStanding :exec
-- Moves every lore of the arc currently at from_status to to_status, except
-- keep_id. Demotion runs before promotion so the one-canon index holds.
UPDATE lores
SET status = sqlc.arg(to_status)::text
WHERE arc_id = sqlc.arg(arc_id)::uuid
  AND status = sqlc.arg(from_status)::text
  AND id <> sqlc.arg(keep_id)::uuid;

-- name: SetLoreStanding :exec
UPDATE lores
SET status = sqlc.arg(status)::text
WHERE id = sqlc.arg(id)::uuid
  AND arc_id = sqlc.arg(arc_id)::uuid;

-- name: InsertCanonChange :exec
INSERT INTO canon_changes (arc_id, from_lore_id, to_lore_id, from_score, to_score)
VALUES (sqlc.arg(arc_id)::uuid, sqlc.narg(from_lore_id)::uuid, sqlc.narg(to_lore_id)::uuid,
        sqlc.narg(from_score)::float8, sqlc.narg(to_score)::float8);

-- name: StoreArcCanon :exec
-- Stores the arc's canon, its score and member count, and clears the dirty
-- mark only when it still holds the value read before the pass. A write that
-- marked the arc after that read leaves a different value, so the arc stays
-- marked for the next pass.
UPDATE arcs
SET canon_lore_id = sqlc.narg(canon_lore_id)::uuid,
    canon_score   = sqlc.arg(canon_score)::float8,
    member_count  = sqlc.arg(member_count)::int,
    lore_dirty_at = CASE WHEN lore_dirty_at IS NOT DISTINCT FROM sqlc.narg(seen)::timestamptz
                         THEN NULL ELSE lore_dirty_at END
WHERE id = sqlc.arg(id)::uuid;
