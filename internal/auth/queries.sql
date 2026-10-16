-- name: InsertUser :one
INSERT INTO users (email, password_hash, username, age_confirmed_at)
VALUES (sqlc.arg(email), sqlc.arg(password_hash), sqlc.arg(username), now())
RETURNING id;

-- name: GetUserByEmail :one
SELECT id, email, password_hash, username, role, email_verified_at
FROM users
WHERE lower(email) = lower(sqlc.arg(email)) AND deleted_at IS NULL;

-- name: GetUserByID :one
SELECT id, email, password_hash, username, role, email_verified_at
FROM users
WHERE id = $1 AND deleted_at IS NULL;

-- name: EmailVerified :one
SELECT (email_verified_at IS NOT NULL)::boolean AS verified
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
