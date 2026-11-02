-- name: InsertUser :one
INSERT INTO users (email, password_hash, username, age_confirmed_at, joined_with)
VALUES (sqlc.arg(email), sqlc.arg(password_hash), sqlc.arg(username), now(), sqlc.narg(joined_with))
RETURNING id;

-- name: GetUserByEmail :one
SELECT id, email, password_hash, username, role, email_verified_at,
       EXISTS (SELECT 1 FROM totp_secrets t WHERE t.user_id = users.id AND t.confirmed_at IS NOT NULL)::boolean AS two_factor_enabled
FROM users
WHERE lower(email) = lower(sqlc.arg(email)) AND deleted_at IS NULL;

-- name: GetUserByID :one
SELECT id, email, password_hash, username, role, email_verified_at,
       EXISTS (SELECT 1 FROM totp_secrets t WHERE t.user_id = users.id AND t.confirmed_at IS NOT NULL)::boolean AS two_factor_enabled
FROM users
WHERE id = $1 AND deleted_at IS NULL;

-- name: WriteStanding :one
-- The shared write gate's inputs for a live account: whether its email is
-- verified and whether moderation suspended it.
SELECT (email_verified_at IS NOT NULL)::boolean AS verified, (suspended_at IS NOT NULL)::boolean AS suspended
FROM users
WHERE id = $1 AND deleted_at IS NULL;

-- name: LockUserForLogin :one
-- The account's password hash and session fields, read inside Login's
-- transaction with the row share-locked: a password reset or change that
-- updates the row waits for this sign-in to commit, and one that committed
-- first is the hash this sign-in checks.
SELECT id, password_hash, username, role, email_verified_at
FROM users
WHERE id = sqlc.arg(id)::uuid AND deleted_at IS NULL
FOR SHARE;

-- name: ShareLockUser :one
-- Share-locks a live account's row: step 2 of the lock order in
-- twofactor.go, taken before the two-factor lock.
SELECT id FROM users WHERE id = sqlc.arg(id)::uuid AND deleted_at IS NULL FOR SHARE;

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
INSERT INTO sessions (user_id, expires_at, device) VALUES (sqlc.arg(user_id)::uuid, sqlc.arg(expires_at), sqlc.arg(device)::text)
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

-- name: PurgeEmailTokens :execrows
-- One batch of emailed links spent or expired before the cutoff.
DELETE FROM email_tokens
WHERE id IN (
    SELECT id FROM email_tokens
    WHERE expires_at < sqlc.arg(before)::timestamptz OR used_at < sqlc.arg(before)::timestamptz
    LIMIT sqlc.arg(batch)::integer
);

-- name: PurgeSessions :execrows
-- One batch of sessions that ended (revoked or expired) before the cutoff;
-- their refresh tokens go with them.
DELETE FROM sessions
WHERE id IN (
    SELECT id FROM sessions
    WHERE expires_at < sqlc.arg(before)::timestamptz OR revoked_at < sqlc.arg(before)::timestamptz
    LIMIT sqlc.arg(batch)::integer
);

-- name: PurgeLoginAttempts :execrows
DELETE FROM login_attempts
WHERE id IN (
    SELECT id FROM login_attempts WHERE attempted_at < sqlc.arg(before)::timestamptz
    LIMIT sqlc.arg(batch)::integer
);

-- name: PurgeRateWindows :execrows
-- Counters whose window started before the cutoff; no limit reads them.
DELETE FROM rate_limit_counters
WHERE (action, subject, window_start) IN (
    SELECT action, subject, window_start FROM rate_limit_counters
    WHERE window_start < sqlc.arg(before)::timestamptz
    LIMIT sqlc.arg(batch)::integer
);

-- name: ListLiveSessions :many
-- The account's signed-in sessions, most recently used first.
SELECT id, device, created_at, last_used_at
FROM sessions
WHERE user_id = sqlc.arg(user_id)::uuid AND revoked_at IS NULL AND expires_at > now()
ORDER BY last_used_at DESC, id;

-- name: OwnLiveSession :one
-- Whether the session is the user's and still live.
SELECT EXISTS (
    SELECT 1 FROM sessions
    WHERE id = sqlc.arg(id)::uuid AND user_id = sqlc.arg(user_id)::uuid
      AND revoked_at IS NULL AND expires_at > now()
)::boolean AS live;

-- name: GetTotpForUpdate :one
-- The account's TOTP row, locked so two codes checked at once are
-- serialised and the second sees the first one's last_used_step.
SELECT user_id, key_id, algorithm, ciphertext, created_at, confirmed_at, last_used_step
FROM totp_secrets
WHERE user_id = sqlc.arg(user_id)::uuid
FOR UPDATE;

-- name: GetTwoFactorStatus :one
-- Whether two-factor sign-in is on and how many recovery codes are unused.
SELECT t.confirmed_at,
       (SELECT count(*) FROM recovery_codes r WHERE r.user_id = t.user_id AND r.used_at IS NULL)::int AS codes_left
FROM totp_secrets t
WHERE t.user_id = sqlc.arg(user_id)::uuid AND t.confirmed_at IS NOT NULL;

-- name: UpsertPendingTotp :one
-- Starts (or restarts) setup with a new secret and no wrong codes counted.
-- A confirmed row is left alone and no row is returned, so setup never
-- replaces a secret in use.
INSERT INTO totp_secrets AS t (user_id, key_id, algorithm, ciphertext)
VALUES (sqlc.arg(user_id)::uuid, sqlc.arg(key_id)::text, sqlc.arg(algorithm)::text, sqlc.arg(ciphertext))
ON CONFLICT (user_id) DO UPDATE
SET key_id = excluded.key_id, algorithm = excluded.algorithm, ciphertext = excluded.ciphertext,
    created_at = now(), last_used_step = NULL, setup_failures = 0
WHERE t.confirmed_at IS NULL
RETURNING t.user_id;

-- name: ConfirmTotp :exec
UPDATE totp_secrets SET confirmed_at = now(), last_used_step = sqlc.arg(step)::bigint
WHERE user_id = sqlc.arg(user_id)::uuid AND confirmed_at IS NULL;

-- name: RecordSetupFailure :one
-- Counts one wrong code against a pending setup and returns the count.
UPDATE totp_secrets SET setup_failures = setup_failures + 1
WHERE user_id = sqlc.arg(user_id)::uuid AND confirmed_at IS NULL
RETURNING setup_failures;

-- name: DeletePendingTotp :exec
-- Ends a setup that is still waiting for its first code; a confirmed secret
-- is left alone.
DELETE FROM totp_secrets WHERE user_id = sqlc.arg(user_id)::uuid AND confirmed_at IS NULL;

-- name: SetTotpLastUsedStep :exec
UPDATE totp_secrets SET last_used_step = sqlc.arg(step)::bigint WHERE user_id = sqlc.arg(user_id)::uuid;

-- name: DeleteTotp :exec
DELETE FROM totp_secrets WHERE user_id = sqlc.arg(user_id)::uuid;

-- name: DeleteRecoveryCodes :exec
DELETE FROM recovery_codes WHERE user_id = sqlc.arg(user_id)::uuid;

-- name: InsertRecoveryCode :exec
-- New codes are always hash version 2 (migration 41).
INSERT INTO recovery_codes (user_id, code_hash, hash_version) VALUES (sqlc.arg(user_id)::uuid, sqlc.arg(code_hash), 2);

-- name: UseRecoveryCode :one
-- Spends one unused recovery code, matched by its version 2 hash or, for a
-- code stored before migration 41, its version 1 hash. No row when the code
-- is wrong or spent.
UPDATE recovery_codes SET used_at = now()
WHERE user_id = sqlc.arg(user_id)::uuid AND used_at IS NULL
  AND ((hash_version = 2 AND code_hash = sqlc.arg(code_hash)::bytea)
    OR (hash_version = 1 AND code_hash = sqlc.arg(legacy_code_hash)::bytea))
RETURNING id;

-- name: CountRecoveryCodesLeft :one
SELECT count(*)::int FROM recovery_codes WHERE user_id = sqlc.arg(user_id)::uuid AND used_at IS NULL;

-- name: InsertTwoFactorChallenge :exec
INSERT INTO two_factor_challenges (user_id, token_hash, expires_at)
VALUES (sqlc.arg(user_id)::uuid, sqlc.arg(token_hash), sqlc.arg(expires_at));

-- name: PeekTwoFactorChallenge :one
-- The account of an unused, unexpired challenge, read without a lock so the
-- second step can take the account's locks before the challenge's (the lock
-- order in twofactor.go).
SELECT user_id FROM two_factor_challenges
WHERE token_hash = sqlc.arg(token_hash) AND used_at IS NULL AND expires_at > now();

-- name: GetTwoFactorChallengeForUpdate :one
-- An unused, unexpired challenge and its live account, locked so it is
-- spent at most once.
SELECT c.id, c.user_id, u.username, u.email, u.role, u.email_verified_at
FROM two_factor_challenges c
JOIN users u ON u.id = c.user_id
WHERE c.token_hash = sqlc.arg(token_hash)
  AND c.used_at IS NULL
  AND c.expires_at > now()
  AND u.deleted_at IS NULL
FOR UPDATE OF c;

-- name: UseTwoFactorChallenge :exec
UPDATE two_factor_challenges SET used_at = now() WHERE id = sqlc.arg(id)::uuid;

-- name: DeleteTwoFactorChallenges :exec
-- Ends every challenge the account has open: a password change, a reset or
-- turning two-factor off makes a pending second step meaningless.
DELETE FROM two_factor_challenges WHERE user_id = sqlc.arg(user_id)::uuid;

-- name: LockTwoFactorAttempts :exec
-- Serialises second-factor checks on one account for the rest of the
-- transaction, so parallel guesses cannot all read the same failure count.
SELECT pg_advisory_xact_lock(hashtextextended('perspectiv.two_factor:' || sqlc.arg(user_id)::uuid::text, 0));

-- name: CountRecentFailedTwoFactor :one
SELECT count(*)::int
FROM two_factor_attempts
WHERE user_id = sqlc.arg(user_id)::uuid
  AND NOT succeeded
  AND attempted_at > now() - make_interval(mins => sqlc.arg(window_minutes)::int);

-- name: InsertTwoFactorAttempt :exec
INSERT INTO two_factor_attempts (user_id, succeeded) VALUES (sqlc.arg(user_id)::uuid, sqlc.arg(succeeded)::boolean);

-- name: LockActiveUserByUsername :one
-- The live account with this username, locked for an operator's change.
SELECT id, email, username FROM users
WHERE lower(username) = lower(sqlc.arg(username)::text) AND deleted_at IS NULL
FOR UPDATE;

-- name: PurgeTwoFactorAttempts :execrows
DELETE FROM two_factor_attempts
WHERE id IN (
    SELECT id FROM two_factor_attempts WHERE attempted_at < sqlc.arg(before)::timestamptz
    LIMIT sqlc.arg(batch)::integer
);

-- name: PurgeTwoFactorChallenges :execrows
-- Challenges that expired before the cutoff; a used one has expired too.
DELETE FROM two_factor_challenges
WHERE id IN (
    SELECT id FROM two_factor_challenges WHERE expires_at < sqlc.arg(before)::timestamptz
    LIMIT sqlc.arg(batch)::integer
);
