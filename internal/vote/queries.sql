-- Vote domain queries: up-votes and remove votes on perspectives. A votes row
-- is an up-vote; there is no downvote, and clearing a vote deletes its row.

-- name: GetTarget :one
-- The perspective a vote targets, with what the vote rules read from it.
SELECT id, arc_id, author_id, status, deleted_at, hidden_at
FROM perspectives
WHERE id = $1;

-- name: IsParticipant :one
-- Whether the user has a published, visible perspective in the arc.
SELECT EXISTS (
    SELECT 1 FROM perspectives
    WHERE arc_id = sqlc.arg(arc_id)::uuid
      AND author_id = sqlc.arg(author_id)::uuid
      AND status = sqlc.arg(published)::text
      AND deleted_at IS NULL
      AND hidden_at IS NULL
)::boolean AS participant;

-- name: IsBlocked :one
SELECT EXISTS (
    SELECT 1 FROM blocks
    WHERE blocker_id = sqlc.arg(blocker_id)::uuid
      AND blocked_id = sqlc.arg(blocked_id)::uuid
)::boolean AS blocked;

-- name: InsertVote :exec
-- The primary key (perspective_id, user_id) keeps one vote per user, so a
-- repeated up-vote is a no-op.
INSERT INTO votes (perspective_id, user_id)
VALUES (sqlc.arg(perspective_id)::uuid, sqlc.arg(user_id)::uuid)
ON CONFLICT (perspective_id, user_id) DO NOTHING;

-- name: DeleteVote :exec
DELETE FROM votes
WHERE perspective_id = sqlc.arg(perspective_id)::uuid
  AND user_id = sqlc.arg(user_id)::uuid;

-- name: CountVotes :one
SELECT count(*)::int AS upvotes FROM votes WHERE perspective_id = $1;

-- name: LockArcLore :exec
-- The lore domain's per-arc advisory lock (lore's LockArcLore query), taken
-- with the same key so a remove vote serialises with every lore writer on
-- the arc and takes its locks in the same order: this lock first, then
-- perspective rows.
SELECT pg_advisory_xact_lock(hashtextextended('lore:' || sqlc.arg(arc_id)::uuid::text, 0));

-- name: GetRemoveVote :one
SELECT lore_id FROM remove_votes
WHERE perspective_id = sqlc.arg(perspective_id)::uuid
  AND user_id = sqlc.arg(user_id)::uuid;

-- name: UpsertRemoveVote :exec
-- One remove vote per user per perspective; casting it again records the
-- lore the voter shares with the target now.
INSERT INTO remove_votes (perspective_id, user_id, lore_id)
VALUES (sqlc.arg(perspective_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(lore_id)::uuid)
ON CONFLICT (perspective_id, user_id) DO UPDATE SET lore_id = EXCLUDED.lore_id;

-- name: CountRemoveVotes :one
-- Remove votes on the perspective cast by the given users.
SELECT count(*)::int AS remove_votes
FROM remove_votes
WHERE perspective_id = sqlc.arg(perspective_id)::uuid
  AND user_id = ANY(sqlc.arg(user_ids)::uuid[]);
