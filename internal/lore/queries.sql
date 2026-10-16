-- Lore domain queries: claims on characters, connected-group membership and
-- the reads behind ListClaims and GetLore. Enumerated columns hold the full
-- proto value name; callers pass them as text parameters.

-- name: LockArcLore :exec
-- Serialises every lore writer on one arc for the rest of the transaction.
-- An advisory lock, not a row lock on arcs, so writers outside the lore
-- domain that lock a perspective and then update the arc row never wait on
-- it.
SELECT pg_advisory_xact_lock(hashtextextended('lore:' || sqlc.arg(arc_id)::uuid::text, 0));

-- name: GetCharacter :one
-- The character and the perspective that declares it.
SELECT ch.id, ch.label, ch.perspective_id, p.author_id, p.arc_id
FROM characters ch
JOIN perspectives p ON p.id = ch.perspective_id
WHERE ch.id = $1;

-- name: FindAuthorPerspectiveInArc :one
-- The author's perspective in the arc, published or not.
SELECT id
FROM perspectives
WHERE arc_id = sqlc.arg(arc_id)::uuid
  AND author_id = sqlc.arg(author_id)::uuid;

-- name: LockPerspectives :many
-- Locks the perspectives in id order, the order every lore writer uses, so
-- two writers locking the same pair cannot deadlock.
SELECT id, arc_id, author_id, status, lore_status, deleted_at, hidden_at
FROM perspectives
WHERE id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY id
FOR UPDATE;

-- name: IsBlocked :one
SELECT EXISTS (
    SELECT 1 FROM blocks
    WHERE blocker_id = sqlc.arg(blocker_id)::uuid
      AND blocked_id = sqlc.arg(blocked_id)::uuid
)::boolean AS blocked;

-- name: GetClaimByClaimant :one
-- The claimant's own claim on the character, if any.
SELECT id, status
FROM claims
WHERE character_id = sqlc.arg(character_id)::uuid
  AND claimant_user_id = sqlc.arg(claimant_user_id)::uuid;

-- name: HasActiveClaim :one
SELECT EXISTS (
    SELECT 1 FROM claims
    WHERE character_id = sqlc.arg(character_id)::uuid
      AND status = ANY(sqlc.arg(active_statuses)::text[])
)::boolean AS active;

-- name: InsertClaim :one
INSERT INTO claims (character_id, claimant_user_id, claimant_perspective_id, status)
VALUES (sqlc.arg(character_id)::uuid, sqlc.arg(claimant_user_id)::uuid,
        sqlc.arg(claimant_perspective_id)::uuid, sqlc.arg(status)::text)
RETURNING id;

-- name: GetClaim :one
-- The claim with the perspective that declares its character.
SELECT c.id, c.status, c.claimant_user_id, c.claimant_perspective_id,
       ch.perspective_id, p.author_id, p.arc_id
FROM claims c
JOIN characters ch ON ch.id = c.character_id
JOIN perspectives p ON p.id = ch.perspective_id
WHERE c.id = $1;

-- name: LockClaimStatus :one
SELECT status FROM claims WHERE id = $1 FOR UPDATE;

-- name: SetClaimStatus :exec
UPDATE claims
SET status = sqlc.arg(status)::text, resolved_at = now()
WHERE id = sqlc.arg(id)::uuid;

-- name: MarkArcLoreDirty :exec
UPDATE arcs SET lore_dirty_at = now() WHERE id = $1;

-- name: RejectClaimsOnWithdrawn :exec
-- Rejects every open or accepted claim in the arc whose declaring or claiming
-- perspective was deleted or hidden.
UPDATE claims c
SET status = sqlc.arg(rejected)::text, resolved_at = now()
FROM characters ch, perspectives p, perspectives cp
WHERE ch.id = c.character_id
  AND p.id = ch.perspective_id
  AND cp.id = c.claimant_perspective_id
  AND p.arc_id = sqlc.arg(arc_id)::uuid
  AND c.status = ANY(sqlc.arg(active_statuses)::text[])
  AND (p.deleted_at IS NOT NULL OR p.hidden_at IS NOT NULL
       OR cp.deleted_at IS NOT NULL OR cp.hidden_at IS NOT NULL);

-- name: ArcGroups :many
-- Connected groups of accepted claims in one arc. Every perspective touched
-- by an accepted claim is labelled with the lowest perspective id it can
-- reach over accepted claims taken in either direction; perspectives sharing
-- a label form one group. UNION drops repeated (root, node) pairs, so the
-- walk ends on cycles.
WITH RECURSIVE edges AS (
    SELECT ch.perspective_id AS a, c.claimant_perspective_id AS b
    FROM claims c
    JOIN characters ch ON ch.id = c.character_id
    JOIN perspectives p ON p.id = ch.perspective_id
    WHERE p.arc_id = sqlc.arg(arc_id)::uuid
      AND c.status = sqlc.arg(accepted)::text
),
links AS (
    SELECT a, b FROM edges
    UNION
    SELECT b, a FROM edges
),
reach (root, node) AS (
    SELECT a, a FROM links
    UNION
    SELECT r.root, l.b FROM reach r JOIN links l ON l.a = r.node
)
SELECT r.node::uuid AS perspective_id,
       min(r.root::text)::uuid AS group_key
FROM reach r
GROUP BY r.node
ORDER BY group_key, perspective_id;

-- name: ArcMemberships :many
-- The stored lore of every member in the arc.
SELECT m.perspective_id, m.lore_id
FROM lore_members m
JOIN lores l ON l.id = m.lore_id
WHERE l.arc_id = $1;

-- name: InsertLore :one
INSERT INTO lores (arc_id) VALUES ($1) RETURNING id;

-- name: DeleteArcMembers :exec
DELETE FROM lore_members m
USING lores l
WHERE l.id = m.lore_id AND l.arc_id = $1;

-- name: InsertMembers :exec
INSERT INTO lore_members (lore_id, perspective_id)
SELECT unnest(sqlc.arg(lore_ids)::uuid[]), unnest(sqlc.arg(perspective_ids)::uuid[]);

-- name: SetArcLoreCounts :exec
-- Member counts for every lore in the arc; a lore whose group vanished
-- keeps its row with 0.
UPDATE lores l
SET member_count = (SELECT count(*) FROM lore_members m WHERE m.lore_id = l.id)
WHERE l.arc_id = $1;

-- name: MarkMembersInLore :exec
UPDATE perspectives p
SET lore_status = sqlc.arg(in_lore)::text, lore_id = m.lore_id
FROM lore_members m, lores l
WHERE m.perspective_id = p.id
  AND l.id = m.lore_id
  AND l.arc_id = sqlc.arg(arc_id)::uuid;

-- name: ReleaseNonMembers :exec
-- A perspective in the arc that is no longer a member loses its lore id, and
-- one that was IN_LORE becomes released_status.
UPDATE perspectives p
SET lore_id = NULL,
    lore_status = CASE WHEN p.lore_status = sqlc.arg(in_lore)::text
                       THEN sqlc.arg(released_status)::text
                       ELSE p.lore_status END
WHERE p.arc_id = sqlc.arg(arc_id)::uuid
  AND (p.lore_id IS NOT NULL OR p.lore_status = sqlc.arg(in_lore)::text)
  AND NOT EXISTS (SELECT 1 FROM lore_members m WHERE m.perspective_id = p.id);

-- name: GetPerspectiveLore :one
SELECT p.lore_status, m.lore_id
FROM perspectives p
LEFT JOIN lore_members m ON m.perspective_id = p.id
WHERE p.id = $1;

-- name: ListLoreMembers :many
SELECT m.perspective_id, p.author_id, u.username
FROM lore_members m
JOIN perspectives p ON p.id = m.perspective_id
JOIN users u ON u.id = p.author_id
WHERE m.lore_id = $1
ORDER BY m.perspective_id;

-- name: RejectClaimsOfPerspective :exec
-- Rejects every open or accepted claim on the perspective's characters, and
-- every one its author made in the same arc.
UPDATE claims c
SET status = sqlc.arg(rejected)::text, resolved_at = now()
FROM characters ch, perspectives p
WHERE ch.id = c.character_id
  AND p.id = ch.perspective_id
  AND c.status = ANY(sqlc.arg(active_statuses)::text[])
  AND (ch.perspective_id = sqlc.arg(perspective_id)::uuid
       OR c.claimant_perspective_id = sqlc.arg(perspective_id)::uuid
       OR (c.claimant_user_id = sqlc.arg(author_id)::uuid AND p.arc_id = sqlc.arg(arc_id)::uuid));

-- name: SetPerspectiveRemoved :exec
UPDATE perspectives
SET lore_status = sqlc.arg(removed)::text, lore_id = NULL
WHERE id = sqlc.arg(id)::uuid;

-- name: DeleteMemberOf :exec
DELETE FROM lore_members WHERE perspective_id = $1;

-- name: ListIncomingClaims :many
-- Claims on characters in the caller's perspectives, newest first, keyset
-- paged on (created_at, id).
SELECT c.id, p.arc_id, c.character_id, ch.label, ch.perspective_id,
       p.author_id, au.username AS author_username,
       c.claimant_user_id, cu.username AS claimant_username,
       c.claimant_perspective_id, c.status, c.created_at, c.resolved_at
FROM claims c
JOIN characters ch ON ch.id = c.character_id
JOIN perspectives p ON p.id = ch.perspective_id
JOIN users au ON au.id = p.author_id
JOIN users cu ON cu.id = c.claimant_user_id
WHERE p.author_id = sqlc.arg(user_id)::uuid
  AND (sqlc.narg(status)::text IS NULL OR c.status = sqlc.narg(status)::text)
  AND (NOT sqlc.arg(has_after)::boolean
       OR (c.created_at, c.id) < (sqlc.arg(after_created_at)::timestamptz, sqlc.arg(after_id)::uuid))
ORDER BY c.created_at DESC, c.id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: ListOutgoingClaims :many
-- Claims the caller made, newest first, keyset paged on (created_at, id).
SELECT c.id, p.arc_id, c.character_id, ch.label, ch.perspective_id,
       p.author_id, au.username AS author_username,
       c.claimant_user_id, cu.username AS claimant_username,
       c.claimant_perspective_id, c.status, c.created_at, c.resolved_at
FROM claims c
JOIN characters ch ON ch.id = c.character_id
JOIN perspectives p ON p.id = ch.perspective_id
JOIN users au ON au.id = p.author_id
JOIN users cu ON cu.id = c.claimant_user_id
WHERE c.claimant_user_id = sqlc.arg(user_id)::uuid
  AND (sqlc.narg(status)::text IS NULL OR c.status = sqlc.narg(status)::text)
  AND (NOT sqlc.arg(has_after)::boolean
       OR (c.created_at, c.id) < (sqlc.arg(after_created_at)::timestamptz, sqlc.arg(after_id)::uuid))
ORDER BY c.created_at DESC, c.id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: GetLore :one
SELECT id, arc_id, status, member_count, upvotes, rank_score, computed_at
FROM lores
WHERE id = $1;

-- name: ListLoreEncounters :many
-- Accepted claims linking two members of the lore.
SELECT c.id AS claim_id, ch.perspective_id, c.claimant_perspective_id
FROM claims c
JOIN characters ch ON ch.id = c.character_id
JOIN lore_members m1 ON m1.perspective_id = ch.perspective_id
JOIN lore_members m2 ON m2.perspective_id = c.claimant_perspective_id AND m2.lore_id = m1.lore_id
WHERE m1.lore_id = sqlc.arg(lore_id)::uuid
  AND c.status = sqlc.arg(accepted)::text
ORDER BY c.id;

-- name: GetPerspectiveArc :one
-- The arc a perspective is published in; NULL for a draft.
SELECT arc_id FROM perspectives WHERE id = $1;
