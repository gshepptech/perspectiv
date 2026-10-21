-- name: GetUser :one
-- One account by id, deleted or not; the service decides what a deleted or
-- suspended account shows.
SELECT id, email, email_verified_at, password_hash, username, username_changed_at,
       avatar_path, bio, role, suspended_at, deleted_at, created_at
FROM users
WHERE id = $1;

-- name: LockUser :one
-- Locks the account row for the rest of the transaction and returns what a
-- profile write needs to replace.
SELECT avatar_path, deleted_at
FROM users
WHERE id = $1
FOR UPDATE;

-- name: GetActiveUserIDByUsername :one
SELECT id
FROM users
WHERE lower(username) = lower(sqlc.arg(username)::text) AND deleted_at IS NULL;

-- name: UserIsActive :one
SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND deleted_at IS NULL);

-- name: UpdateProfile :exec
UPDATE users
SET bio = sqlc.arg(bio), avatar_path = sqlc.narg(avatar_path)
WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- name: ChangeUsername :one
-- Succeeds only when the last change is at least window_hours old (or there
-- was none), so two concurrent changes cannot both pass the window check. The
-- window is in hours, not days, so it is the same fixed length whatever the
-- session time zone and its daylight-saving changes.
UPDATE users
SET username = sqlc.arg(username), username_changed_at = now()
WHERE id = sqlc.arg(id)
  AND deleted_at IS NULL
  AND (username_changed_at IS NULL
       OR username_changed_at <= now() - make_interval(hours => sqlc.arg(window_hours)::int))
RETURNING username_changed_at;

-- name: InsertBlock :exec
INSERT INTO blocks (blocker_id, blocked_id)
VALUES (sqlc.arg(blocker_id), sqlc.arg(blocked_id))
ON CONFLICT (blocker_id, blocked_id) DO NOTHING;

-- name: DeleteBlock :exec
DELETE FROM blocks
WHERE blocker_id = sqlc.arg(blocker_id) AND blocked_id = sqlc.arg(blocked_id);

-- name: ListBlocked :many
SELECT u.id, u.username, u.deleted_at
FROM blocks b
JOIN users u ON u.id = b.blocked_id
WHERE b.blocker_id = sqlc.arg(blocker_id)
ORDER BY b.created_at DESC, u.id
LIMIT sqlc.arg(max_rows);

-- name: CountRecentFailedAttempts :one
SELECT count(*)::int
FROM login_attempts
WHERE user_id = sqlc.arg(user_id)
  AND NOT succeeded
  AND attempted_at > now() - make_interval(mins => sqlc.arg(window_minutes)::int);

-- name: InsertFailedAttempt :exec
INSERT INTO login_attempts (user_id, succeeded) VALUES ($1, false);

-- name: SoftDeletePerspectivesByAuthor :many
-- Soft-deletes every live perspective, draft or published, and returns the
-- distinct arcs the published ones sat in.
WITH deleted AS (
    UPDATE perspectives
    SET deleted_at = now(), updated_at = now()
    WHERE author_id = sqlc.arg(author_id) AND deleted_at IS NULL
    RETURNING arc_id
)
SELECT DISTINCT arc_id::uuid AS arc_id
FROM deleted
WHERE arc_id IS NOT NULL;

-- name: MarkArcsLoreDirty :exec
UPDATE arcs SET lore_dirty_at = now() WHERE id = ANY(sqlc.arg(arc_ids)::uuid[]);

-- name: AnonymizeUser :exec
-- Erases the account's personal data and frees its email and username. The
-- replacement username and email are derived from the id, so they are unique,
-- and neither can be chosen at signup: the username fails the username rule
-- and the email domain is reserved (.invalid, RFC 2606).
UPDATE users
SET email = sqlc.arg(email),
    username = sqlc.arg(username),
    password_hash = '!',
    bio = '',
    avatar_path = NULL,
    deleted_at = now()
WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- name: RevokeSessions :exec
UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL;

-- name: RevokeRefreshTokens :exec
UPDATE refresh_tokens rt
SET revoked_at = now()
FROM sessions s
WHERE rt.session_id = s.id AND s.user_id = $1 AND rt.revoked_at IS NULL;

-- name: LoreCounts :one
-- The lores a user's readable perspectives are members of, and how many of
-- them are canon: the profile's badges.
SELECT count(*) FILTER (WHERE l.status = 'LORE_STANDING_CANON')::int AS canon_count,
       count(*)::int AS lore_count
FROM lore_members m
JOIN perspectives p ON p.id = m.perspective_id
JOIN lores l ON l.id = m.lore_id
WHERE p.author_id = $1
  AND p.status = 'PERSPECTIVE_STATUS_PUBLISHED'
  AND p.deleted_at IS NULL AND p.hidden_at IS NULL;
