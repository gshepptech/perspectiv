-- name: ListInbox :many
-- The caller's inbox, newest first, keyset-paginated on (created_at, id) so
-- it is served by notifications_inbox_idx. after_at/after_id are NULL on the
-- first page. The actor's username and the arc's title are joined for display.
-- arc_hidden is true when the arc's place or any ancestor is hidden; the
-- service then drops the arc from the row, so the inbox never names it.
WITH RECURSIVE page AS (
    SELECT n.id, n.kind, n.payload, n.read_at, n.created_at,
           (n.payload ->> 'arc_id')::uuid AS arc_id
    FROM notifications n
    WHERE n.user_id = sqlc.arg(user_id)::uuid
      AND (NOT sqlc.arg(unread_only)::boolean OR n.read_at IS NULL)
      AND (sqlc.narg(after_at)::timestamptz IS NULL
           OR (n.created_at, n.id) < (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid))
    ORDER BY n.created_at DESC, n.id DESC
    LIMIT sqlc.arg(row_limit)::int
), up AS (
    SELECT ar.id AS arc_id, pl.parent_id, pl.hidden_at, 1 AS depth
    FROM arcs ar
    JOIN places pl ON pl.id = ar.place_id
    WHERE ar.id IN (SELECT page.arc_id FROM page)
    UNION ALL
    SELECT up.arc_id, parent.parent_id, parent.hidden_at, up.depth + 1
    FROM places parent
    JOIN up ON parent.id = up.parent_id
    WHERE up.depth < 8
)
SELECT page.id, page.kind, page.payload, page.read_at, page.created_at,
       a.username AS actor_username,
       ar.title   AS arc_title,
       EXISTS (SELECT 1 FROM up WHERE up.arc_id = page.arc_id
               AND up.hidden_at IS NOT NULL)::boolean AS arc_hidden
FROM page
LEFT JOIN users a ON a.id = (page.payload ->> 'actor_id')::uuid
LEFT JOIN arcs ar ON ar.id = page.arc_id
ORDER BY page.created_at DESC, page.id DESC;

-- name: CountUnread :one
SELECT count(*)::int AS unread
FROM notifications
WHERE user_id = $1 AND read_at IS NULL;

-- name: MarkRead :exec
-- Ids that are not the caller's, or already read, are left alone.
UPDATE notifications
SET read_at = now()
WHERE user_id = sqlc.arg(user_id)::uuid
  AND id = ANY(sqlc.arg(ids)::uuid[])
  AND read_at IS NULL;

-- name: MarkAllRead :exec
UPDATE notifications
SET read_at = now()
WHERE user_id = $1 AND read_at IS NULL;

-- name: ListPreferences :many
-- Stored toggles only; a kind with no row has both toggles on.
SELECT kind, in_app, email
FROM notification_preferences
WHERE user_id = $1;

-- name: UpsertPreference :exec
INSERT INTO notification_preferences (user_id, kind, in_app, email)
VALUES (sqlc.arg(user_id)::uuid, sqlc.arg(kind)::text, sqlc.arg(in_app)::boolean, sqlc.arg(email)::boolean)
ON CONFLICT (user_id, kind) DO UPDATE
SET in_app = EXCLUDED.in_app, email = EXCLUDED.email;

-- name: ClaimFacts :one
-- A claim with the perspective holding its character (whose author receives
-- the claim) and the claimant.
SELECT c.claimant_user_id, ch.perspective_id AS character_perspective_id,
       p.author_id AS character_author_id, p.arc_id
FROM claims c
JOIN characters ch ON ch.id = c.character_id
JOIN perspectives p ON p.id = ch.perspective_id
WHERE c.id = $1;

-- name: PerspectiveFacts :one
SELECT author_id, arc_id, lore_id, status, deleted_at, hidden_at
FROM perspectives
WHERE id = $1;

-- name: ArcAuthors :many
-- Authors of the arc's visible published perspectives.
SELECT DISTINCT author_id
FROM perspectives
WHERE arc_id = $1
  AND status = 'PERSPECTIVE_STATUS_PUBLISHED'
  AND deleted_at IS NULL
  AND hidden_at IS NULL;

-- name: LoreAuthors :many
-- Authors of a lore's visible member perspectives.
SELECT DISTINCT p.author_id
FROM lore_members lm
JOIN perspectives p ON p.id = lm.perspective_id
WHERE lm.lore_id = $1
  AND p.deleted_at IS NULL
  AND p.hidden_at IS NULL;

-- name: LoreArc :one
SELECT arc_id FROM lores WHERE id = $1;

-- name: ReportReporter :one
SELECT reporter_id FROM reports WHERE id = $1;

-- name: Recipients :many
-- Candidates who still have an account, are not the actor, and have not
-- blocked the actor, with their toggles for kind (a missing preference row
-- means both on). actor_id is NULL for a system event.
SELECT u.id, u.email,
       COALESCE(np.in_app, true)::boolean AS in_app,
       COALESCE(np.email, true)::boolean  AS email_on
FROM users u
LEFT JOIN notification_preferences np
       ON np.user_id = u.id AND np.kind = sqlc.arg(kind)::text
WHERE u.id = ANY(sqlc.arg(candidates)::uuid[])
  AND u.deleted_at IS NULL
  AND u.id IS DISTINCT FROM sqlc.narg(actor_id)::uuid
  AND NOT EXISTS (
      SELECT 1 FROM blocks b
      WHERE b.blocker_id = u.id AND b.blocked_id = sqlc.narg(actor_id)::uuid)
ORDER BY u.id;

-- name: InsertNotification :exec
INSERT INTO notifications (user_id, kind, payload)
VALUES (sqlc.arg(user_id)::uuid, sqlc.arg(kind)::text, sqlc.arg(payload)::jsonb);

-- name: Username :one
SELECT username FROM users WHERE id = $1;

-- name: ArcTitle :one
SELECT title FROM arcs WHERE id = $1;

-- name: ArcPlaceHidden :one
-- True when the arc's place or any ancestor is hidden: hiding a place hides
-- its whole subtree, the rule the arc domain's GetArcRow applies. An unknown
-- arc is not hidden.
WITH RECURSIVE up AS (
    SELECT pl.parent_id, pl.hidden_at, 1 AS depth
    FROM arcs a
    JOIN places pl ON pl.id = a.place_id
    WHERE a.id = sqlc.arg(arc_id)::uuid
    UNION ALL
    SELECT parent.parent_id, parent.hidden_at, up.depth + 1
    FROM places parent
    JOIN up ON parent.id = up.parent_id
    WHERE up.depth < 8
)
SELECT EXISTS (SELECT 1 FROM up WHERE up.hidden_at IS NOT NULL)::boolean AS hidden;

-- name: InviteAnswer :one
-- For an invite-answered event: who invited the author of this perspective.
SELECT i.inviter_id
FROM perspectives p
JOIN invites i ON i.code = p.invite_code
WHERE p.id = $1;

-- name: FollowersOfArcPlaces :many
-- People who follow the arc's place or any place above it, except those
-- who wrote in the arc: they hear of a new story there already
-- (NEW_PERSPECTIVE_IN_ARC).
SELECT DISTINCT f.user_id
FROM arcs a
JOIN place_follows f ON f.place_id = ANY(a.place_path)
WHERE a.id = $1
  AND f.user_id NOT IN (
        SELECT p.author_id FROM perspectives p
        WHERE p.arc_id = a.id AND p.status = 'PERSPECTIVE_STATUS_PUBLISHED'
          AND p.deleted_at IS NULL AND p.hidden_at IS NULL);
