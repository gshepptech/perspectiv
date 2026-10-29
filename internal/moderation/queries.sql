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
INSERT INTO moderation_audit (actor_id, action, target_kind, target_id, report_id, reason)
VALUES (sqlc.narg(actor_id)::uuid, @action::text, @target_kind::text, @target_id::uuid,
        sqlc.narg(report_id)::uuid, @reason::text);

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
-- status lists every status.
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
