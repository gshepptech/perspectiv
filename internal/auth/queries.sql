-- name: InsertUser :one
INSERT INTO users (email, password_hash, username, age_confirmed_at, joined_with)
VALUES (sqlc.arg(email), sqlc.arg(password_hash), sqlc.arg(username), now(), sqlc.narg(joined_with))
RETURNING id;

-- name: GetUserByEmail :one
SELECT id, email, password_hash, username, role, email_verified_at
FROM users
WHERE lower(email) = lower(sqlc.arg(email)) AND deleted_at IS NULL;

-- name: GetUserByID :one
SELECT id, email, password_hash, username, role, email_verified_at
FROM users
WHERE id = $1 AND deleted_at IS NULL;

-- name: WriteStanding :one
-- The shared write gate's inputs for a live account: whether its email is
-- verified and whether moderation suspended it.
SELECT (email_verified_at IS NOT NULL)::boolean AS verified, (suspended_at IS NOT NULL)::boolean AS suspended
FROM users
WHERE id = $1 AND deleted_at IS NULL;

-- name: LockLoginAttempts :exec
-- Serialises login attempts on one account for the rest of the transaction,
-- so a burst of parallel guesses cannot all read the same failure count.
SELECT pg_advisory_xact_lock(hashtextextended('perspectiv.login:' || sqlc.arg(user_id)::uuid::text, 0));

-- name: CountRecentFailedLogins :one
SELECT count(*)::int
FROM login_attempts
WHERE user_id = sqlc.arg(user_id)
  AND NOT succeeded
  AND attempted_at > now() - make_interval(mins => sqlc.arg(window_minutes)::int);

-- name: InsertLoginAttempt :exec
INSERT INTO login_attempts (user_id, succeeded) VALUES ($1, $2);

-- name: InsertSession :one
INSERT INTO sessions (user_id, expires_at) VALUES ($1, $2)
RETURNING id;

-- name: InsertRefreshToken :exec
INSERT INTO refresh_tokens (session_id, parent_id, token_hash, expires_at)
VALUES (sqlc.arg(session_id), sqlc.narg(parent_id), sqlc.arg(token_hash), sqlc.arg(expires_at));

-- name: GetRefreshTokenForUpdate :one
-- Locks the token and its session, so two refreshes presenting the same
-- token are serialised and the second one sees the first one's rotation.
SELECT rt.id, rt.session_id, rt.expires_at, rt.rotated_at, rt.revoked_at,
       s.user_id, s.expires_at AS session_expires_at, s.revoked_at AS session_revoked_at
FROM refresh_tokens rt
JOIN sessions s ON s.id = rt.session_id
WHERE rt.token_hash = $1
FOR UPDATE OF rt, s;

-- name: MarkRefreshTokenRotated :exec
UPDATE refresh_tokens SET rotated_at = now() WHERE id = $1;

-- name: TouchSession :exec
UPDATE sessions SET last_used_at = now() WHERE id = $1;

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeSessionRefreshTokens :exec
UPDATE refresh_tokens SET revoked_at = now() WHERE session_id = $1 AND revoked_at IS NULL;

-- name: RevokeUserSessions :exec
UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL;

-- name: RevokeUserRefreshTokens :exec
UPDATE refresh_tokens rt
SET revoked_at = now()
FROM sessions s
WHERE rt.session_id = s.id AND s.user_id = $1 AND rt.revoked_at IS NULL;

-- name: GetLiveSessionRole :one
-- The role of a session's user while the session is live: not revoked, not
-- past its absolute expiry, and its account not deleted.
SELECT u.role
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.id = sqlc.arg(session_id)
  AND s.user_id = sqlc.arg(user_id)
  AND s.revoked_at IS NULL
  AND s.expires_at > now()
  AND u.deleted_at IS NULL;

-- name: InsertEmailToken :exec
INSERT INTO email_tokens (user_id, purpose, token_hash, expires_at)
VALUES ($1, $2, $3, $4);

-- name: ConsumeEmailToken :one
-- Marks an unused, unexpired token of the purpose used and returns its user,
-- provided the account still exists.
UPDATE email_tokens et
SET used_at = now()
FROM users u
WHERE et.token_hash = sqlc.arg(token_hash)
  AND et.purpose = sqlc.arg(purpose)
  AND et.used_at IS NULL
  AND et.expires_at > now()
  AND u.id = et.user_id
  AND u.deleted_at IS NULL
RETURNING et.user_id;

-- name: RetireEmailTokens :exec
-- Retires every other outstanding token of the purpose once one is used.
UPDATE email_tokens SET used_at = now()
WHERE user_id = $1 AND purpose = $2 AND used_at IS NULL;

-- name: MarkEmailVerified :exec
UPDATE users SET email_verified_at = now() WHERE id = $1 AND email_verified_at IS NULL;

-- name: UpdatePasswordHash :exec
UPDATE users SET password_hash = $2 WHERE id = $1;

-- name: HitRateLimit :one
-- Counts one hit in the current fixed window of window_seconds, aligned to
-- the Unix epoch, and returns the count including this hit.
INSERT INTO rate_limit_counters AS c (action, subject, window_start, count)
VALUES (
    sqlc.arg(action), sqlc.arg(subject),
    date_bin(make_interval(secs => sqlc.arg(window_seconds)::int), now(), 'epoch'::timestamptz),
    1)
ON CONFLICT (action, subject, window_start) DO UPDATE SET count = c.count + 1
RETURNING c.count, c.window_start;

-- name: DeleteStaleRateWindows :exec
DELETE FROM rate_limit_counters
WHERE action = sqlc.arg(action) AND subject = sqlc.arg(subject) AND window_start < sqlc.arg(current_window);

-- name: UseAccessCode :one
-- Admits one more account on a beta code that is live: not revoked, not
-- expired and not used up. No row when it cannot.
UPDATE access_codes SET uses = uses + 1
WHERE code = sqlc.arg(code)::text
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now())
  AND uses < max_uses
RETURNING code;

-- name: UseMemberInvite :one
-- Admits one more sign-up on a member's invite link while it is live: the
-- inviter is in good standing, the shared story is still readable, and the
-- link has admitted fewer than max_signups. No row when it cannot.
UPDATE invites i SET signups = i.signups + 1
FROM users u, perspectives p
WHERE i.code = sqlc.arg(code)::text
  AND u.id = i.inviter_id AND u.deleted_at IS NULL AND u.suspended_at IS NULL
  AND p.id = i.perspective_id AND p.status = 'PERSPECTIVE_STATUS_PUBLISHED'
  AND p.deleted_at IS NULL AND p.hidden_at IS NULL
  AND i.signups < sqlc.arg(max_signups)::integer
RETURNING i.code;

-- name: ActorStanding :one
-- The stored role and standing of a caller, for admin checks that must not
-- trust an access token minted before a change.
SELECT role, suspended_at, deleted_at FROM users WHERE id = sqlc.arg(id)::uuid;

-- name: InsertAccessCode :one
INSERT INTO access_codes (code, note, max_uses, created_by, expires_at)
VALUES (sqlc.arg(code)::text, sqlc.arg(note)::text, sqlc.arg(max_uses)::integer, sqlc.arg(created_by)::uuid, sqlc.narg(expires_at))
RETURNING code, note, max_uses, uses, created_at, expires_at, revoked_at;

-- name: ListAccessCodes :many
-- Newest first, keyset-paginated on (created_at, code).
SELECT code, note, max_uses, uses, created_at, expires_at, revoked_at
FROM access_codes
WHERE sqlc.narg(after_created)::timestamptz IS NULL
   OR (created_at, code) < (sqlc.narg(after_created)::timestamptz, sqlc.narg(after_code)::text)
ORDER BY created_at DESC, code DESC
LIMIT sqlc.arg(lim)::integer;

-- name: RevokeAccessCode :one
UPDATE access_codes SET revoked_at = coalesce(revoked_at, now())
WHERE code = sqlc.arg(code)::text
RETURNING code, note, max_uses, uses, created_at, expires_at, revoked_at;

-- name: JoinWaitlist :exec
-- A second request from the same address is a no-op.
INSERT INTO waitlist (email, note) VALUES (sqlc.arg(email)::text, sqlc.arg(note)::text)
ON CONFLICT (lower(email)) DO NOTHING;

-- name: ListWaitlist :many
-- Oldest first, keyset-paginated on (created_at, id).
SELECT id, email, note, created_at, invited_at, code
FROM waitlist
WHERE (NOT sqlc.arg(waiting_only)::boolean OR invited_at IS NULL)
  AND (sqlc.narg(after_created)::timestamptz IS NULL
       OR (created_at, id) > (sqlc.narg(after_created)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY created_at, id
LIMIT sqlc.arg(lim)::integer;

-- name: LockWaitlistEntry :one
SELECT id, email, note, created_at, invited_at, code FROM waitlist WHERE id = sqlc.arg(id)::uuid FOR UPDATE;

-- name: MarkWaitlistInvited :one
UPDATE waitlist SET invited_at = now(), code = sqlc.arg(code)::text
WHERE id = sqlc.arg(id)::uuid
RETURNING id, email, note, created_at, invited_at, code;

-- name: RevokeOtherSessions :exec
-- Signs out every other device: all the user's live sessions but keep.
UPDATE sessions SET revoked_at = now()
WHERE user_id = sqlc.arg(user_id)::uuid AND id <> sqlc.arg(keep)::uuid AND revoked_at IS NULL;

-- name: RevokeOtherRefreshTokens :exec
UPDATE refresh_tokens rt
SET revoked_at = now()
FROM sessions s
WHERE rt.session_id = s.id AND s.user_id = sqlc.arg(user_id)::uuid
  AND s.id <> sqlc.arg(keep)::uuid AND rt.revoked_at IS NULL;

-- name: InsertEmailChangeToken :exec
INSERT INTO email_tokens (user_id, purpose, token_hash, expires_at, new_email)
VALUES (sqlc.arg(user_id)::uuid, 'change_email', sqlc.arg(token_hash), sqlc.arg(expires_at), sqlc.arg(new_email)::text);

-- name: ConsumeEmailChangeToken :one
-- Marks an unused, unexpired change_email token used and returns its user
-- and the new address, provided the account still exists.
UPDATE email_tokens et
SET used_at = now()
FROM users u
WHERE et.token_hash = sqlc.arg(token_hash)
  AND et.purpose = 'change_email'
  AND et.used_at IS NULL
  AND et.expires_at > now()
  AND u.id = et.user_id
  AND u.deleted_at IS NULL
RETURNING et.user_id, et.new_email::text AS new_email;

-- name: UpdateEmail :exec
-- The new address was proven by its confirmation link, so it is verified.
UPDATE users SET email = sqlc.arg(email)::text, email_verified_at = now() WHERE id = sqlc.arg(id)::uuid;

-- name: EmailTaken :one
SELECT EXISTS (SELECT 1 FROM users WHERE lower(email) = lower(sqlc.arg(email)::text) AND id <> sqlc.arg(id)::uuid)::boolean AS taken;
