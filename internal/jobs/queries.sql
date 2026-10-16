-- name: LockPerspectiveForExpiry :one
-- Locks the perspective row so a concurrent lore writer that also locks it
-- (claim resolve, delete, hide) is ordered before or after the expiry.
SELECT arc_id, lore_status, deleted_at, hidden_at
FROM perspectives
WHERE id = $1
FOR UPDATE;

-- name: HasAcceptedClaim :one
-- True when the perspective has an accepted claim in either direction: on one
-- of its characters, or made by it about another perspective's character.
SELECT EXISTS (
    SELECT 1
    FROM claims c
    JOIN characters ch ON ch.id = c.character_id
    WHERE c.status = sqlc.arg(accepted_status)::text
      AND (ch.perspective_id = sqlc.arg(perspective_id)::uuid
           OR c.claimant_perspective_id = sqlc.arg(perspective_id)::uuid)
)::boolean AS accepted;

-- name: TransitionLoreStatus :exec
-- Moves the perspective from one lore status to another; a row no longer in
-- the from status is left alone.
UPDATE perspectives
SET lore_status = sqlc.arg(to_status)::text
WHERE id = sqlc.arg(id)::uuid
  AND lore_status = sqlc.arg(from_status)::text;

-- name: MarkArcLoreDirty :exec
UPDATE arcs
SET lore_dirty_at = now()
WHERE id = $1;
