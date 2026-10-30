-- name: GetActor :one
-- The stored role and standing of a caller. Authorization decisions read the
-- row, not the access token, so a revoked role or a suspension takes effect at
-- once.
SELECT role, suspended_at, deleted_at
FROM users
WHERE id = @id::uuid;

-- name: PerspectiveReportable :one
-- A perspective can be reported while a reader can see it: published, not
-- deleted and not already hidden.
SELECT EXISTS (
    SELECT 1 FROM perspectives
    WHERE id = @id::uuid
      AND status = 'PERSPECTIVE_STATUS_PUBLISHED'
      AND deleted_at IS NULL
      AND hidden_at IS NULL
)::boolean AS ok;

-- name: PlaceReportable :one
SELECT EXISTS (
    SELECT 1 FROM places WHERE id = @id::uuid AND hidden_at IS NULL
)::boolean AS ok;

-- name: LorePostReportable :one
-- A thread post can be reported while readers can see it.
SELECT EXISTS (
    SELECT 1 FROM lore_posts WHERE id = @id::uuid AND deleted_at IS NULL AND hidden_at IS NULL
)::boolean AS ok;

-- name: LoreNameReportable :one
-- A lore's name can be reported while it has one.
SELECT EXISTS (
    SELECT 1 FROM lores WHERE id = @id::uuid AND name IS NOT NULL
)::boolean AS ok;

-- name: UserReportable :one
SELECT EXISTS (
    SELECT 1 FROM users WHERE id = @id::uuid AND deleted_at IS NULL
)::boolean AS ok;

-- name: FindOpenReport :one
-- The reporter's own open report on a target, if any.
SELECT id
FROM reports
WHERE reporter_id = @reporter_id::uuid
  AND target_kind = @target_kind::text
  AND target_id = @target_id::uuid
  AND status = 'REPORT_STATUS_OPEN'
ORDER BY created_at, id
LIMIT 1;

-- name: InsertReport :one
INSERT INTO reports (reporter_id, target_kind, target_id, reason, details)
VALUES (@reporter_id::uuid, @target_kind::text, @target_id::uuid, @reason::text, @details::text)
RETURNING id;

-- name: LockReport :one
SELECT id, target_kind, target_id, status
FROM reports
WHERE id = @id::uuid
FOR UPDATE;

-- name: HidePerspective :one
-- Keeps the first hidden_at when the perspective is already hidden.
UPDATE perspectives
SET hidden_at = coalesce(hidden_at, now())
WHERE id = @id::uuid
RETURNING arc_id;

-- name: MarkArcLoreDirty :exec
UPDATE arcs SET lore_dirty_at = now() WHERE id = @id::uuid;

-- name: HidePlace :execrows
UPDATE places SET hidden_at = coalesce(hidden_at, now()) WHERE id = @id::uuid;

-- name: HideLorePost :execrows
UPDATE lore_posts SET hidden_at = coalesce(hidden_at, now()) WHERE id = @id::uuid;

-- name: ClearLoreName :execrows
UPDATE lores SET name = NULL, named_by = NULL, name_cleared_at = now() WHERE id = @id::uuid;

-- name: RestorePerspective :one
-- Unhides a hidden perspective; no row when it was not hidden.
UPDATE perspectives SET hidden_at = NULL WHERE id = @id::uuid AND hidden_at IS NOT NULL
RETURNING arc_id;

-- name: RestorePlace :execrows
UPDATE places SET hidden_at = NULL WHERE id = @id::uuid AND hidden_at IS NOT NULL;

-- name: RestoreLorePost :execrows
UPDATE lore_posts SET hidden_at = NULL WHERE id = @id::uuid AND hidden_at IS NOT NULL;

-- name: MakeVenue :execrows
-- Turns a visible store into a mall-level venue, unless it already sits
-- inside a mall (a venue cannot hold a venue).
UPDATE places p SET kind = 'PLACE_KIND_MALL'
WHERE p.id = @id::uuid AND p.kind = 'PLACE_KIND_STORE' AND p.hidden_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM places parent WHERE parent.id = p.parent_id AND parent.kind = 'PLACE_KIND_MALL');

-- name: LockPlacesForMerge :many
-- Both places of a merge, visible ones only, locked against a concurrent
-- write; with the duplicate's arc count and the deepest level of its
-- children (0 for none), for the merge's checks.
SELECT p.id, p.kind,
       (SELECT count(*) FROM arcs a WHERE a.place_id = p.id)::integer AS arcs,
       coalesce((SELECT max(array_position(ARRAY['PLACE_KIND_STATE', 'PLACE_KIND_CITY', 'PLACE_KIND_TOWN',
                                                 'PLACE_KIND_MALL', 'PLACE_KIND_STORE']::text[], c.kind))
                 FROM places c WHERE c.parent_id = p.id), 0)::integer AS child_level,
       array_position(ARRAY['PLACE_KIND_STATE', 'PLACE_KIND_CITY', 'PLACE_KIND_TOWN',
                            'PLACE_KIND_MALL', 'PLACE_KIND_STORE']::text[], p.kind)::integer AS level
FROM places p
WHERE p.id = ANY(@ids::uuid[]) AND p.hidden_at IS NULL
FOR UPDATE;

-- name: MovePlaceChildren :exec
UPDATE places SET parent_id = @into_id::uuid WHERE parent_id = @from_id::uuid;

-- name: LiftSuspension :execrows
-- Clears a live account's suspension; no row when it was not suspended.
UPDATE users SET suspended_at = NULL WHERE id = @id::uuid AND suspended_at IS NOT NULL AND deleted_at IS NULL;

-- name: SuspendUser :execrows
UPDATE users SET suspended_at = coalesce(suspended_at, now()) WHERE id = @id::uuid;

-- name: ResolveReport :exec
UPDATE reports
SET status = 'REPORT_STATUS_RESOLVED',
    resolved_by = @resolved_by::uuid,
    resolution = @resolution::text,
    resolution_reason = @resolution_reason::text,
    resolved_at = now()
WHERE id = @id::uuid;

-- name: InsertAudit :exec
-- A target is named by target_id, or for an access code by target_ref.
INSERT INTO moderation_audit (actor_id, action, target_kind, target_id, target_ref, report_id, reason)
VALUES (sqlc.narg(actor_id)::uuid, @action::text, @target_kind::text, sqlc.narg(target_id)::uuid,
        @target_ref::text, sqlc.narg(report_id)::uuid, @reason::text);

-- name: GetReport :one
-- One report as the admin queue shows it. A deleted account's username reads
-- as [deleted], as it does in every reading view.
SELECT r.id,
       r.reporter_id,
       (CASE WHEN ru.deleted_at IS NOT NULL THEN '[deleted]' ELSE ru.username END)::text AS reporter_username,
       r.target_kind,
       r.target_id,
       coalesce(CASE r.target_kind
           WHEN 'REPORT_TARGET_KIND_USER' THEN (
               SELECT CASE WHEN tu.deleted_at IS NOT NULL THEN '[deleted]' ELSE tu.username END
               FROM users tu WHERE tu.id = r.target_id)
           WHEN 'REPORT_TARGET_KIND_PLACE' THEN (SELECT tp.name FROM places tp WHERE tp.id = r.target_id)
           WHEN 'REPORT_TARGET_KIND_PERSPECTIVE' THEN (
               SELECT ta.title FROM perspectives tv JOIN arcs ta ON ta.id = tv.arc_id WHERE tv.id = r.target_id)
           WHEN 'REPORT_TARGET_KIND_LORE_POST' THEN (SELECT left(tlp.body, 120) FROM lore_posts tlp WHERE tlp.id = r.target_id)
           WHEN 'REPORT_TARGET_KIND_LORE_NAME' THEN (SELECT tl.name FROM lores tl WHERE tl.id = r.target_id)
       END, '')::text AS target_label,
       r.reason,
       r.details,
       r.status,
       r.resolved_by,
       (CASE WHEN rb.deleted_at IS NOT NULL THEN '[deleted]' ELSE coalesce(rb.username, '') END)::text AS resolver_username,
       r.resolution,
       r.resolution_reason,
       r.created_at,
       r.resolved_at
FROM reports r
JOIN users ru ON ru.id = r.reporter_id
LEFT JOIN users rb ON rb.id = r.resolved_by
WHERE r.id = @id::uuid;

-- name: ListReports :many
-- The admin queue, oldest first, keyset-paginated on (created_at, id). A NULL
-- status lists every status; a reporter narrows to what one user filed, and a
-- subject to reports against one user: the account, or a perspective or
-- thread post they wrote.
SELECT r.id,
       r.reporter_id,
       (CASE WHEN ru.deleted_at IS NOT NULL THEN '[deleted]' ELSE ru.username END)::text AS reporter_username,
       r.target_kind,
       r.target_id,
       coalesce(CASE r.target_kind
           WHEN 'REPORT_TARGET_KIND_USER' THEN (
               SELECT CASE WHEN tu.deleted_at IS NOT NULL THEN '[deleted]' ELSE tu.username END
               FROM users tu WHERE tu.id = r.target_id)
           WHEN 'REPORT_TARGET_KIND_PLACE' THEN (SELECT tp.name FROM places tp WHERE tp.id = r.target_id)
           WHEN 'REPORT_TARGET_KIND_PERSPECTIVE' THEN (
               SELECT ta.title FROM perspectives tv JOIN arcs ta ON ta.id = tv.arc_id WHERE tv.id = r.target_id)
           WHEN 'REPORT_TARGET_KIND_LORE_POST' THEN (SELECT left(tlp.body, 120) FROM lore_posts tlp WHERE tlp.id = r.target_id)
           WHEN 'REPORT_TARGET_KIND_LORE_NAME' THEN (SELECT tl.name FROM lores tl WHERE tl.id = r.target_id)
       END, '')::text AS target_label,
       r.reason,
       r.details,
       r.status,
       r.resolved_by,
       (CASE WHEN rb.deleted_at IS NOT NULL THEN '[deleted]' ELSE coalesce(rb.username, '') END)::text AS resolver_username,
       r.resolution,
       r.resolution_reason,
       r.created_at,
       r.resolved_at
FROM reports r
JOIN users ru ON ru.id = r.reporter_id
LEFT JOIN users rb ON rb.id = r.resolved_by
WHERE (sqlc.narg(status)::text IS NULL OR r.status = sqlc.narg(status)::text)
  AND (sqlc.narg(reporter_id)::uuid IS NULL OR r.reporter_id = sqlc.narg(reporter_id)::uuid)
  AND (sqlc.narg(subject_id)::uuid IS NULL
       OR (r.target_kind = 'REPORT_TARGET_KIND_USER' AND r.target_id = sqlc.narg(subject_id)::uuid)
       OR (r.target_kind = 'REPORT_TARGET_KIND_PERSPECTIVE' AND EXISTS (
               SELECT 1 FROM perspectives sp WHERE sp.id = r.target_id AND sp.author_id = sqlc.narg(subject_id)::uuid))
       OR (r.target_kind = 'REPORT_TARGET_KIND_LORE_POST' AND EXISTS (
               SELECT 1 FROM lore_posts slp WHERE slp.id = r.target_id AND slp.author_id = sqlc.narg(subject_id)::uuid)))
  AND (sqlc.narg(after_created_at)::timestamptz IS NULL
       OR (r.created_at, r.id) > (sqlc.narg(after_created_at)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY r.created_at, r.id
LIMIT @max_rows::integer;

-- name: LockActiveUserByUsername :one
-- The account a username names, compared without regard to case. A deleted
-- account no longer holds its username.
SELECT id, role
FROM users
WHERE lower(username) = lower(@username::text)
  AND deleted_at IS NULL
FOR UPDATE;

-- name: SetRoleAdmin :exec
UPDATE users SET role = 'ROLE_ADMIN' WHERE id = @id::uuid;

-- name: LockReportKey :exec
-- Serializes concurrent reports by one reporter on one target, so a double
-- submit finds the first report instead of opening a second.
SELECT pg_advisory_xact_lock(hashtextextended(@key::text, 0));

-- name: ListAuditLog :many
-- The audit log, newest first, keyset-paginated on id. Each filter is off
-- when NULL; a subject matches actions on the account or on a perspective or
-- thread post it wrote, other than lookups.
SELECT a.id,
       a.actor_id,
       (CASE WHEN au.deleted_at IS NOT NULL THEN '[deleted]' ELSE coalesce(au.username, '') END)::text AS actor_username,
       a.action,
       a.target_kind,
       a.target_id,
       a.target_ref,
       coalesce(CASE a.target_kind
           WHEN 'AUDIT_TARGET_KIND_USER' THEN (
               SELECT CASE WHEN tu.deleted_at IS NOT NULL THEN '[deleted]' ELSE tu.username END
               FROM users tu WHERE tu.id = a.target_id)
           WHEN 'AUDIT_TARGET_KIND_PLACE' THEN (SELECT tp.name FROM places tp WHERE tp.id = a.target_id)
           WHEN 'AUDIT_TARGET_KIND_PERSPECTIVE' THEN (
               SELECT ta.title FROM perspectives tv JOIN arcs ta ON ta.id = tv.arc_id WHERE tv.id = a.target_id)
           WHEN 'AUDIT_TARGET_KIND_LORE_POST' THEN (SELECT left(tlp.body, 120) FROM lore_posts tlp WHERE tlp.id = a.target_id)
           WHEN 'AUDIT_TARGET_KIND_LORE_NAME' THEN (SELECT tl.name FROM lores tl WHERE tl.id = a.target_id)
           WHEN 'AUDIT_TARGET_KIND_ACCESS_CODE' THEN a.target_ref
       END, '')::text AS target_label,
       a.report_id,
       a.reason,
       a.at
FROM moderation_audit a
LEFT JOIN users au ON au.id = a.actor_id
WHERE (sqlc.narg(action)::text IS NULL OR a.action = sqlc.narg(action)::text)
  AND (sqlc.narg(actor_id)::uuid IS NULL OR a.actor_id = sqlc.narg(actor_id)::uuid)
  AND (sqlc.narg(subject_id)::uuid IS NULL
       OR (a.target_kind = 'AUDIT_TARGET_KIND_USER' AND a.target_id = sqlc.narg(subject_id)::uuid)
       OR (a.target_kind = 'AUDIT_TARGET_KIND_PERSPECTIVE' AND EXISTS (
               SELECT 1 FROM perspectives sp WHERE sp.id = a.target_id AND sp.author_id = sqlc.narg(subject_id)::uuid))
       OR (a.target_kind = 'AUDIT_TARGET_KIND_LORE_POST' AND EXISTS (
               SELECT 1 FROM lore_posts slp WHERE slp.id = a.target_id AND slp.author_id = sqlc.narg(subject_id)::uuid)))
  -- About a user means acted on them: lookups only read the account, and a
  -- dossier opened often would bury its actions under them. The whole log
  -- still lists every lookup.
  AND (sqlc.narg(subject_id)::uuid IS NULL OR a.action <> 'AUDIT_ACTION_USER_LOOKUP')
  AND (sqlc.narg(before_id)::bigint IS NULL OR a.id < sqlc.narg(before_id)::bigint)
ORDER BY a.id DESC
LIMIT @max_rows::integer;

-- name: FindUserByID :one
SELECT id FROM users WHERE id = @id::uuid;

-- name: FindUserByUsername :one
-- A deleted account no longer holds its username.
SELECT id FROM users WHERE lower(username) = lower(@username::text) AND deleted_at IS NULL;

-- name: FindUserByEmail :one
SELECT id FROM users WHERE lower(email) = lower(@email::text) AND deleted_at IS NULL;

-- name: AdminUserFacts :one
-- What a moderator sees of one account.
SELECT u.id,
       u.username,
       u.email,
       (u.email_verified_at IS NOT NULL)::boolean AS email_verified,
       u.role,
       u.created_at,
       u.suspended_at,
       (u.deleted_at IS NOT NULL)::boolean AS deleted,
       (SELECT count(*) FROM login_attempts la
        WHERE la.user_id = u.id AND NOT la.succeeded AND la.attempted_at > now() - interval '24 hours')::integer AS failed_sign_ins_24h,
       (SELECT count(*) FROM perspectives p
        WHERE p.author_id = u.id AND p.status = 'PERSPECTIVE_STATUS_PUBLISHED' AND p.deleted_at IS NULL)::integer AS stories,
       (SELECT count(*) FROM perspectives p
        WHERE p.author_id = u.id AND p.hidden_at IS NOT NULL AND p.deleted_at IS NULL)::integer AS hidden_stories,
       (SELECT count(*) FROM reports r WHERE r.reporter_id = u.id)::integer AS reports_filed
FROM users u
WHERE u.id = @id::uuid;

-- name: LastSignIn :one
-- The newest session's start; only a sign-in starts one.
SELECT created_at FROM sessions WHERE user_id = @user_id::uuid ORDER BY created_at DESC LIMIT 1;

-- name: CountReportsAgainst :one
-- Reports against an account: on it, or on a perspective or thread post it
-- wrote; all of them, and those still open.
SELECT count(*)::integer AS total,
       (count(*) FILTER (WHERE r.status = 'REPORT_STATUS_OPEN'))::integer AS open
FROM reports r
WHERE (r.target_kind = 'REPORT_TARGET_KIND_USER' AND r.target_id = @subject_id::uuid)
   OR (r.target_kind = 'REPORT_TARGET_KIND_PERSPECTIVE' AND EXISTS (
           SELECT 1 FROM perspectives sp WHERE sp.id = r.target_id AND sp.author_id = @subject_id::uuid))
   OR (r.target_kind = 'REPORT_TARGET_KIND_LORE_POST' AND EXISTS (
           SELECT 1 FROM lore_posts slp WHERE slp.id = r.target_id AND slp.author_id = @subject_id::uuid));
