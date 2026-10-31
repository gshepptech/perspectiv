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

-- name: LockPair :exec
-- Serialises follows and blocks between two accounts for the rest of the
-- transaction, in either order, so a follow cannot slip in beside a block
-- that is committing at the same moment.
SELECT pg_advisory_xact_lock(hashtextextended(
    least(sqlc.arg(a)::uuid, sqlc.arg(b)::uuid)::text || greatest(sqlc.arg(a)::uuid, sqlc.arg(b)::uuid)::text, 0));

-- name: BlockedEitherWay :one
SELECT EXISTS (
    SELECT 1 FROM blocks
    WHERE (blocker_id = sqlc.arg(a)::uuid AND blocked_id = sqlc.arg(b)::uuid)
       OR (blocker_id = sqlc.arg(b)::uuid AND blocked_id = sqlc.arg(a)::uuid)
)::boolean AS blocked;

-- name: DeleteFollowsBetween :exec
-- Removes a follow between two accounts in both directions.
DELETE FROM user_follows
WHERE (follower_id = sqlc.arg(a)::uuid AND followee_id = sqlc.arg(b)::uuid)
   OR (follower_id = sqlc.arg(b)::uuid AND followee_id = sqlc.arg(a)::uuid);

-- name: DeleteFollowsOf :exec
-- Removes every follow an account made or received.
DELETE FROM user_follows WHERE follower_id = sqlc.arg(user_id)::uuid OR followee_id = sqlc.arg(user_id)::uuid;

-- name: DeleteSavesOf :exec
-- Removes every story an account saved to read later.
DELETE FROM saved_perspectives WHERE user_id = sqlc.arg(user_id)::uuid;

-- name: FollowUser :exec
INSERT INTO user_follows (follower_id, followee_id)
VALUES (sqlc.arg(follower_id)::uuid, sqlc.arg(followee_id)::uuid)
ON CONFLICT (follower_id, followee_id) DO NOTHING;

-- name: UnfollowUser :exec
DELETE FROM user_follows
WHERE follower_id = sqlc.arg(follower_id)::uuid AND followee_id = sqlc.arg(followee_id)::uuid;

-- name: IsFollowingUser :one
SELECT EXISTS (
    SELECT 1 FROM user_follows
    WHERE follower_id = sqlc.arg(follower_id)::uuid AND followee_id = sqlc.arg(followee_id)::uuid
)::boolean AS following;

-- name: CountFollowers :one
-- Accounts that follow the writer and are not deleted.
SELECT count(*)::int
FROM user_follows f
JOIN users u ON u.id = f.follower_id
WHERE f.followee_id = sqlc.arg(followee_id)::uuid AND u.deleted_at IS NULL;

-- name: ListFollowedWriters :many
-- The writers an account follows, most recently followed first, leaving out
-- deleted accounts.
SELECT u.id, u.username, u.avatar_path, f.created_at AS followed_at
FROM user_follows f
JOIN users u ON u.id = f.followee_id
WHERE f.follower_id = sqlc.arg(follower_id)::uuid AND u.deleted_at IS NULL
ORDER BY f.created_at DESC, u.id
LIMIT sqlc.arg(max_rows)::integer;

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

-- name: ExportAccount :one
-- Everything the database holds about one account, as one JSON document,
-- for "Download my data". It carries the account's own email and nobody
-- else's; other people appear only by username, as they do on the site.
-- Soft-deleted and moderated rows are included and marked: they are still
-- held.
SELECT jsonb_build_object(
  'exported_at', now(),
  'account', (
    SELECT jsonb_build_object(
      'id', u.id, 'email', u.email, 'username', u.username, 'bio', u.bio,
      'created_at', u.created_at, 'email_verified_at', u.email_verified_at,
      'age_confirmed_at', u.age_confirmed_at, 'username_changed_at', u.username_changed_at,
      'joined_with', u.joined_with, 'has_avatar', u.avatar_path IS NOT NULL)
    FROM users u WHERE u.id = sqlc.arg(user_id)::uuid),
  'perspectives', coalesce((
    SELECT jsonb_agg(jsonb_build_object(
      'id', p.id, 'arc_id', p.arc_id, 'place_id', p.place_id, 'local_date', p.local_date, 'hour', p.hour,
      'title_if_new_arc', p.title_if_new_arc, 'genre', p.genre, 'status', p.status, 'lore_status', p.lore_status,
      'created_at', p.created_at, 'published_at', p.published_at, 'updated_at', p.updated_at,
      'deleted_at', p.deleted_at, 'hidden_at', p.hidden_at,
      'parts', coalesce((SELECT jsonb_agg(pp.body ORDER BY pp.position) FROM perspective_parts pp WHERE pp.perspective_id = p.id), '[]'::jsonb),
      'characters', coalesce((SELECT jsonb_agg(jsonb_build_object('id', c.id, 'label', c.label) ORDER BY c.position)
                              FROM characters c WHERE c.perspective_id = p.id), '[]'::jsonb))
      ORDER BY p.created_at)
    FROM perspectives p WHERE p.author_id = sqlc.arg(user_id)::uuid), '[]'::jsonb),
  'claims_made', coalesce((
    SELECT jsonb_agg(jsonb_build_object(
      'character', c.label, 'in_perspective_id', c.perspective_id, 'status', cl.status,
      'created_at', cl.created_at, 'resolved_at', cl.resolved_at) ORDER BY cl.created_at)
    FROM claims cl JOIN characters c ON c.id = cl.character_id
    WHERE cl.claimant_user_id = sqlc.arg(user_id)::uuid), '[]'::jsonb),
  'claims_received', coalesce((
    SELECT jsonb_agg(jsonb_build_object(
      'character', c.label, 'by', coalesce(cu.username, '[deleted]'), 'status', cl.status,
      'created_at', cl.created_at, 'resolved_at', cl.resolved_at) ORDER BY cl.created_at)
    FROM claims cl JOIN characters c ON c.id = cl.character_id
    JOIN perspectives p ON p.id = c.perspective_id
    LEFT JOIN users cu ON cu.id = cl.claimant_user_id AND cu.deleted_at IS NULL
    WHERE p.author_id = sqlc.arg(user_id)::uuid), '[]'::jsonb),
  'upvotes', coalesce((
    SELECT jsonb_agg(jsonb_build_object('perspective_id', v.perspective_id, 'created_at', v.created_at) ORDER BY v.created_at)
    FROM votes v WHERE v.user_id = sqlc.arg(user_id)::uuid), '[]'::jsonb),
  'reactions', coalesce((
    SELECT jsonb_agg(jsonb_build_object('perspective_id', r.perspective_id, 'emoji', r.emoji, 'created_at', r.created_at) ORDER BY r.created_at)
    FROM reacts r WHERE r.user_id = sqlc.arg(user_id)::uuid), '[]'::jsonb),
  'remove_votes', coalesce((
    SELECT jsonb_agg(jsonb_build_object('perspective_id', rv.perspective_id, 'created_at', rv.created_at) ORDER BY rv.created_at)
    FROM remove_votes rv WHERE rv.user_id = sqlc.arg(user_id)::uuid), '[]'::jsonb),
  'thread_posts', coalesce((
    SELECT jsonb_agg(jsonb_build_object('id', lp.id, 'lore_id', lp.lore_id, 'reply_to_id', lp.reply_to_id,
                                        'answered_id', lp.answered_id, 'body', lp.body, 'created_at', lp.created_at,
                                        'edited_at', lp.edited_at, 'deleted_at', lp.deleted_at, 'hidden_at', lp.hidden_at) ORDER BY lp.created_at)
    FROM lore_posts lp WHERE lp.author_id = sqlc.arg(user_id)::uuid), '[]'::jsonb),
  'thread_upvotes', coalesce((
    SELECT jsonb_agg(jsonb_build_object('post_id', pv.post_id, 'created_at', pv.created_at) ORDER BY pv.created_at)
    FROM lore_post_votes pv WHERE pv.user_id = sqlc.arg(user_id)::uuid), '[]'::jsonb),
  'lores_named', coalesce((
    SELECT jsonb_agg(jsonb_build_object('lore_id', l.id, 'name', l.name))
    FROM lores l WHERE l.named_by = sqlc.arg(user_id)::uuid), '[]'::jsonb),
  'places_followed', coalesce((
    SELECT jsonb_agg(jsonb_build_object('place_id', f.place_id, 'name', pl.name, 'since', f.created_at) ORDER BY f.created_at)
    FROM place_follows f JOIN places pl ON pl.id = f.place_id WHERE f.user_id = sqlc.arg(user_id)::uuid), '[]'::jsonb),
  'writers_followed', coalesce((
    SELECT jsonb_agg(jsonb_build_object('username', coalesce(fu.username, '[deleted]'), 'since', f.created_at) ORDER BY f.created_at)
    FROM user_follows f LEFT JOIN users fu ON fu.id = f.followee_id AND fu.deleted_at IS NULL
    WHERE f.follower_id = sqlc.arg(user_id)::uuid), '[]'::jsonb),
  'saved_stories', coalesce((
    SELECT jsonb_agg(jsonb_build_object('perspective_id', sp.perspective_id, 'saved_at', sp.created_at) ORDER BY sp.created_at)
    FROM saved_perspectives sp WHERE sp.user_id = sqlc.arg(user_id)::uuid), '[]'::jsonb),
  'blocked', coalesce((
    SELECT jsonb_agg(jsonb_build_object('username', coalesce(bu.username, '[deleted]'), 'since', b.created_at) ORDER BY b.created_at)
    FROM blocks b LEFT JOIN users bu ON bu.id = b.blocked_id AND bu.deleted_at IS NULL
    WHERE b.blocker_id = sqlc.arg(user_id)::uuid), '[]'::jsonb),
  'reports_filed', coalesce((
    SELECT jsonb_agg(jsonb_build_object('target_kind', rp.target_kind, 'target_id', rp.target_id, 'reason', rp.reason,
                                        'details', rp.details, 'status', rp.status, 'created_at', rp.created_at) ORDER BY rp.created_at)
    FROM reports rp WHERE rp.reporter_id = sqlc.arg(user_id)::uuid), '[]'::jsonb),
  'invites_sent', coalesce((
    SELECT jsonb_agg(jsonb_build_object('code', i.code, 'perspective_id', i.perspective_id, 'signups', i.signups, 'created_at', i.created_at) ORDER BY i.created_at)
    FROM invites i WHERE i.inviter_id = sqlc.arg(user_id)::uuid), '[]'::jsonb),
  'notification_settings', coalesce((
    SELECT jsonb_agg(jsonb_build_object('kind', np.kind, 'in_app', np.in_app, 'email', np.email))
    FROM notification_preferences np WHERE np.user_id = sqlc.arg(user_id)::uuid), '[]'::jsonb)
)::text AS export;
