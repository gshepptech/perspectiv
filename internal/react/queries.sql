-- React domain queries: emoji reacts on perspectives. A user holds at most
-- one of each emoji on a perspective; the primary key enforces it.

-- name: GetTarget :one
SELECT id, author_id, status, deleted_at, hidden_at
FROM perspectives
WHERE id = $1;

-- name: IsBlocked :one
SELECT EXISTS (
    SELECT 1 FROM blocks
    WHERE blocker_id = sqlc.arg(blocker_id)::uuid
      AND blocked_id = sqlc.arg(blocked_id)::uuid
)::boolean AS blocked;

-- name: InsertReact :exec
INSERT INTO reacts (perspective_id, user_id, emoji)
VALUES (sqlc.arg(perspective_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(emoji)::text)
ON CONFLICT (perspective_id, user_id, emoji) DO NOTHING;

-- name: DeleteReact :exec
DELETE FROM reacts
WHERE perspective_id = sqlc.arg(perspective_id)::uuid
  AND user_id = sqlc.arg(user_id)::uuid
  AND emoji = sqlc.arg(emoji)::text;

-- name: CountReacts :many
-- Per-emoji totals on the perspective and whether the user holds each.
SELECT emoji,
       count(*)::int AS total,
       bool_or(user_id = sqlc.arg(user_id)::uuid)::boolean AS mine
FROM reacts
WHERE perspective_id = sqlc.arg(perspective_id)::uuid
GROUP BY emoji;
