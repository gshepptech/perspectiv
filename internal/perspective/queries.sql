-- name: GetAuthor :one
-- The writer's account state for the publish guard.
SELECT email_verified_at, suspended_at, deleted_at
FROM users
WHERE id = $1;

-- name: GetPerspectiveRef :one
-- An unlocked read of where a perspective sits, taken before the arc lock.
SELECT id, author_id, arc_id, status, deleted_at, hidden_at
FROM perspectives
WHERE id = $1;

-- name: LockPerspective :one
SELECT id, author_id, arc_id, status, deleted_at, hidden_at
FROM perspectives
WHERE id = $1
FOR UPDATE;

-- name: LockArcLore :exec
-- The lore domain's per-arc advisory lock. Every writer that changes claims
-- or characters in an arc takes it before any perspective row lock, the
-- lore domain's lock order, so a character edit and a claim resolution on
-- the same arc serialise instead of interleaving. The key expression must
-- stay identical to the lore domain's LockArcLore.
SELECT pg_advisory_xact_lock(hashtextextended('lore:' || sqlc.arg(arc_id)::uuid::text, 0));

-- name: InsertDraft :one
INSERT INTO perspectives (author_id, place_id, local_date, hour, title_if_new_arc, genre)
VALUES (sqlc.arg(author_id)::uuid, sqlc.narg(place_id)::uuid, sqlc.narg(local_date)::date,
        sqlc.narg(hour)::smallint, sqlc.narg(title_if_new_arc)::text, sqlc.narg(genre)::text)
RETURNING id;

-- name: UpdateDraft :exec
UPDATE perspectives
SET place_id = sqlc.narg(place_id)::uuid,
    local_date = sqlc.narg(local_date)::date,
    hour = sqlc.narg(hour)::smallint,
    title_if_new_arc = sqlc.narg(title_if_new_arc)::text,
    genre = sqlc.narg(genre)::text,
    updated_at = now()
WHERE id = sqlc.arg(id)::uuid;

-- name: FindInArc :one
-- The author's perspective in an arc that is not deleted, draft, published
-- or hidden; the partial unique index allows at most one. A deleted one
-- frees the place, so its author may write there again.
SELECT id, deleted_at, hidden_at
FROM perspectives
WHERE arc_id = sqlc.arg(arc_id)::uuid AND author_id = sqlc.arg(author_id)::uuid AND deleted_at IS NULL;

-- name: InsertPublished :one
INSERT INTO perspectives (arc_id, author_id, place_id, local_date, hour, genre,
                          status, lore_status, published_at)
VALUES (sqlc.arg(arc_id)::uuid, sqlc.arg(author_id)::uuid, sqlc.arg(place_id)::uuid,
        sqlc.arg(local_date)::date, sqlc.narg(hour)::smallint, sqlc.arg(genre)::text,
        sqlc.arg(status)::text, sqlc.arg(lore_status)::text, now())
RETURNING id, published_at;

-- name: PublishDraft :one
-- Turns a draft into a published perspective in arc_id. The draft's title is
-- cleared: the arc holds the title from here on.
UPDATE perspectives
SET arc_id = sqlc.arg(arc_id)::uuid,
    place_id = sqlc.arg(place_id)::uuid,
    local_date = sqlc.arg(local_date)::date,
    hour = sqlc.narg(hour)::smallint,
    title_if_new_arc = NULL,
    genre = sqlc.arg(genre)::text,
    status = sqlc.arg(status)::text,
    lore_status = sqlc.arg(lore_status)::text,
    published_at = now(),
    updated_at = now()
WHERE id = sqlc.arg(id)::uuid
RETURNING published_at;

-- name: UpdatePublished :exec
UPDATE perspectives
SET genre = sqlc.arg(genre)::text, updated_at = now()
WHERE id = sqlc.arg(id)::uuid;

-- name: SoftDelete :exec
UPDATE perspectives
SET deleted_at = now(), updated_at = now()
WHERE id = $1;

-- name: MarkArcLoreDirty :exec
UPDATE arcs SET lore_dirty_at = now() WHERE id = $1;

-- name: DeleteParts :exec
DELETE FROM perspective_parts WHERE perspective_id = $1;

-- name: InsertParts :exec
-- Inserts the parts in order; position is the 1-based array index.
INSERT INTO perspective_parts (perspective_id, position, body)
SELECT sqlc.arg(perspective_id)::uuid, b.ord::smallint, b.body
FROM unnest(sqlc.arg(bodies)::text[]) WITH ORDINALITY AS b(body, ord);

-- name: ListCharacterClaims :many
-- A perspective's characters with whether each holds an ACCEPTED or a
-- PENDING claim.
SELECT ch.id,
       EXISTS (SELECT 1 FROM claims c WHERE c.character_id = ch.id
               AND c.status = 'CLAIM_STATUS_ACCEPTED')::boolean AS has_accepted
FROM characters ch
WHERE ch.perspective_id = $1
FOR UPDATE OF ch;

-- name: DeleteCharacters :exec
DELETE FROM characters WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- name: UpdateCharacter :exec
UPDATE characters
SET label = sqlc.arg(label)::text, position = sqlc.arg(position)::smallint
WHERE id = sqlc.arg(id)::uuid;

-- name: InsertCharacter :exec
INSERT INTO characters (perspective_id, label, position)
VALUES (sqlc.arg(perspective_id)::uuid, sqlc.arg(label)::text, sqlc.arg(position)::smallint);

-- name: GetPerspectives :many
-- Full rows for a set of perspectives with their arc, place and author. The
-- place joined is the arc's for a published perspective and the draft's own
-- otherwise; see placeholder in read.go. place_hidden is true when that
-- place or any ancestor is hidden: hiding a place hides its whole subtree,
-- the rule the arc domain's GetArcRow applies.
--
-- A draft has no arc yet, so draft_arc_title is the title of the arc that
-- exists for its place, day and hour, the one a publish would join, and
-- superseded_by_id is the author's own live published story there, which
-- makes the draft impossible to publish. arcs_place_date_hour_key and
-- perspectives_arc_author_key serve both joins.
WITH RECURSIVE up AS (
    SELECT p0.id AS perspective_id, pl0.parent_id, pl0.hidden_at, 1 AS depth
    FROM perspectives p0
    LEFT JOIN arcs a0 ON a0.id = p0.arc_id
    JOIN places pl0 ON pl0.id = COALESCE(a0.place_id, p0.place_id)
    WHERE p0.id = ANY(sqlc.arg(ids)::uuid[])
    UNION ALL
    SELECT up.perspective_id, parent.parent_id, parent.hidden_at, up.depth + 1
    FROM places parent
    JOIN up ON parent.id = up.parent_id
    WHERE up.depth < 8
)
SELECT p.id, p.arc_id, p.author_id, p.genre, p.status, p.lore_status, p.lore_id,
       p.title_if_new_arc, p.created_at, p.published_at, p.updated_at, p.deleted_at, p.hidden_at,
       p.place_id   AS draft_place_id,
       p.local_date AS draft_local_date,
       p.hour       AS draft_hour,
       a.place_id   AS arc_place_id,
       a.local_date AS arc_local_date,
       a.hour       AS arc_hour,
       a.title      AS arc_title,
       pl.kind  AS place_kind,
       pl.name  AS place_name,
       u.username AS author_username,
       da.title AS draft_arc_title,
       sup.id   AS superseded_by_id,
       EXISTS (SELECT 1 FROM up WHERE up.perspective_id = p.id
               AND up.hidden_at IS NOT NULL)::boolean AS place_hidden,
       (SELECT count(*) FROM votes v WHERE v.perspective_id = p.id)::int AS upvotes,
       EXISTS (SELECT 1 FROM votes v WHERE v.perspective_id = p.id
               AND v.user_id = sqlc.narg(viewer)::uuid)::boolean AS voted,
       EXISTS (SELECT 1 FROM saved_perspectives s WHERE s.perspective_id = p.id
               AND s.user_id = sqlc.narg(viewer)::uuid)::boolean AS saved
FROM perspectives p
JOIN users u ON u.id = p.author_id
LEFT JOIN arcs a ON a.id = p.arc_id
LEFT JOIN places pl ON pl.id = COALESCE(a.place_id, p.place_id)
LEFT JOIN arcs da ON p.arc_id IS NULL AND da.place_id = p.place_id
    AND da.local_date = p.local_date AND da.hour IS NOT DISTINCT FROM p.hour
LEFT JOIN perspectives sup ON sup.arc_id = da.id AND sup.author_id = p.author_id
    AND sup.status = 'PERSPECTIVE_STATUS_PUBLISHED' AND sup.deleted_at IS NULL
WHERE p.id = ANY(sqlc.arg(ids)::uuid[]);

-- name: ListPartsOf :many
SELECT perspective_id, position, body
FROM perspective_parts
WHERE perspective_id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY perspective_id, position;

-- name: ListCharactersOf :many
-- Each character with its claim state: who it turned out to be (the accepted
-- claimant, unless that account is deleted) and the viewer's own claim.
SELECT ch.id, ch.perspective_id, ch.position, ch.label,
       EXISTS (SELECT 1 FROM claims c WHERE c.character_id = ch.id
               AND c.status = 'CLAIM_STATUS_ACCEPTED')::boolean AS has_accepted,
       EXISTS (SELECT 1 FROM claims c WHERE c.character_id = ch.id
               AND c.status = 'CLAIM_STATUS_PENDING')::boolean AS has_pending,
       acc_u.id AS claimed_by_id,
       acc_u.username AS claimed_by_username,
       -- The claimant's own story of the day, when it is readable. A claim
       -- is made from the same arc, so its place is visible whenever the
       -- character's story is.
       cp.id AS claimed_by_story_id,
       COALESCE((SELECT c.status FROM claims c
                 WHERE c.character_id = ch.id AND c.claimant_user_id = sqlc.narg(viewer)::uuid
                 ORDER BY c.created_at DESC LIMIT 1), '')::text AS my_claim
FROM characters ch
LEFT JOIN claims acc ON acc.character_id = ch.id AND acc.status = 'CLAIM_STATUS_ACCEPTED'
LEFT JOIN users acc_u ON acc_u.id = acc.claimant_user_id AND acc_u.deleted_at IS NULL
LEFT JOIN perspectives cp ON cp.id = acc.claimant_perspective_id AND acc_u.id IS NOT NULL
    AND cp.status = 'PERSPECTIVE_STATUS_PUBLISHED' AND cp.deleted_at IS NULL AND cp.hidden_at IS NULL
WHERE ch.perspective_id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY ch.perspective_id, ch.position;

-- name: ReactCountsOf :many
SELECT r.perspective_id, r.emoji, count(*)::int AS total,
       COALESCE(bool_or(r.user_id = sqlc.narg(viewer)::uuid), false)::boolean AS mine
FROM reacts r
WHERE r.perspective_id = ANY(sqlc.arg(ids)::uuid[])
GROUP BY r.perspective_id, r.emoji
ORDER BY r.perspective_id, r.emoji;

-- name: ListMine :many
-- The author's live perspectives, newest edit first, keyset-paginated on
-- (updated_at, id) so it is served by perspectives_author_updated_idx.
-- Filters are NULL when unused; see Service.ListMine for the hour rule.
SELECT p.id, p.updated_at
FROM perspectives p
LEFT JOIN arcs a ON a.id = p.arc_id
WHERE p.author_id = sqlc.arg(author_id)::uuid
  AND p.deleted_at IS NULL
  AND (sqlc.narg(status)::text IS NULL OR p.status = sqlc.narg(status)::text)
  AND (sqlc.narg(place_id)::uuid IS NULL OR COALESCE(a.place_id, p.place_id) = sqlc.narg(place_id)::uuid)
  AND (sqlc.narg(local_date)::date IS NULL OR COALESCE(a.local_date, p.local_date) = sqlc.narg(local_date)::date)
  AND (NOT sqlc.arg(match_hour)::boolean
       OR (CASE WHEN a.id IS NULL THEN p.hour ELSE a.hour END) IS NOT DISTINCT FROM sqlc.narg(hour)::smallint)
  AND (sqlc.narg(after_at)::timestamptz IS NULL
       OR (p.updated_at, p.id) < (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY p.updated_at DESC, p.id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: ListByAuthor :many
-- One author's readable published perspectives, newest first, keyset-
-- paginated on (published_at, id); mine is served by
-- perspectives_author_published_idx. A perspective whose arc's place or any
-- ancestor is hidden is left out: hiding a place hides its whole subtree.
WITH RECURSIVE mine AS (
    SELECT p.id, p.published_at, a.place_id
    FROM perspectives p
    JOIN arcs a ON a.id = p.arc_id
    WHERE p.author_id = sqlc.arg(author_id)::uuid
      AND p.status = 'PERSPECTIVE_STATUS_PUBLISHED'
      AND p.deleted_at IS NULL
      AND p.hidden_at IS NULL
      AND p.published_at IS NOT NULL
      AND (sqlc.narg(after_at)::timestamptz IS NULL
           OR (p.published_at, p.id) < (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid))
), up AS (
    SELECT m.id AS perspective_id, pl.parent_id, pl.hidden_at, 1 AS depth
    FROM mine m
    JOIN places pl ON pl.id = m.place_id
    UNION ALL
    SELECT up.perspective_id, parent.parent_id, parent.hidden_at, up.depth + 1
    FROM places parent
    JOIN up ON parent.id = up.parent_id
    WHERE up.depth < 8
)
SELECT m.id, m.published_at::timestamptz AS published_at
FROM mine m
WHERE NOT EXISTS (SELECT 1 FROM up WHERE up.perspective_id = m.id AND up.hidden_at IS NOT NULL)
ORDER BY m.published_at DESC, m.id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: InviteTarget :one
-- The perspective an author wants to share, with the checks CreateInvite
-- makes: its author, whether it is readable, and whether the named
-- character (NULL for none) belongs to it.
SELECT p.author_id,
       (p.status = 'PERSPECTIVE_STATUS_PUBLISHED' AND p.deleted_at IS NULL AND p.hidden_at IS NULL)::boolean AS readable,
       (sqlc.narg(character_id)::uuid IS NULL OR EXISTS (
           SELECT 1 FROM characters c
           WHERE c.id = sqlc.narg(character_id)::uuid AND c.perspective_id = p.id))::boolean AS character_ok
FROM perspectives p
WHERE p.id = sqlc.arg(perspective_id)::uuid;

-- name: UpsertInvite :one
-- One link per perspective and character: a second request returns the
-- first link's code.
INSERT INTO invites (code, perspective_id, inviter_id, character_id)
VALUES (sqlc.arg(code)::text, sqlc.arg(perspective_id)::uuid, sqlc.arg(inviter_id)::uuid, sqlc.narg(character_id)::uuid)
ON CONFLICT (perspective_id, character_id) DO UPDATE SET code = invites.code
RETURNING code;

-- name: GetInviteTeaser :one
-- What an invite link shows. readable is false once the perspective is
-- deleted, hidden or no longer published, or its arc's place or any
-- ancestor is hidden: hiding a place hides its whole subtree.
WITH RECURSIVE target AS (
    SELECT i.code, i.inviter_id, i.character_id, p.id AS perspective_id, p.status, p.deleted_at, p.hidden_at,
           a.id AS arc_id, a.title AS arc_title, a.place_id, a.local_date, a.hour
    FROM invites i
    JOIN perspectives p ON p.id = i.perspective_id
    JOIN arcs a ON a.id = p.arc_id
    WHERE i.code = sqlc.arg(code)::text
), up AS (
    SELECT pl.parent_id, pl.hidden_at, 1 AS depth
    FROM target t
    JOIN places pl ON pl.id = t.place_id
    UNION ALL
    SELECT parent.parent_id, parent.hidden_at, up.depth + 1
    FROM places parent
    JOIN up ON parent.id = up.parent_id
    WHERE up.depth < 8
)
SELECT t.code, t.inviter_id, u.username AS inviter_username, t.arc_id, t.arc_title, t.local_date, t.hour,
       pl.id AS place_id, pl.kind AS place_kind, pl.name AS place_name,
       COALESCE((SELECT left(pp.body, 241) FROM perspective_parts pp
                 WHERE pp.perspective_id = t.perspective_id ORDER BY pp.position LIMIT 1), '')::text AS opening,
       COALESCE((SELECT c.label FROM characters c WHERE c.id = t.character_id), '')::text AS character_label,
       (t.status = 'PERSPECTIVE_STATUS_PUBLISHED' AND t.deleted_at IS NULL AND t.hidden_at IS NULL
        AND u.deleted_at IS NULL
        AND NOT EXISTS (SELECT 1 FROM up WHERE up.hidden_at IS NOT NULL))::boolean AS readable
FROM target t
JOIN users u ON u.id = t.inviter_id
JOIN places pl ON pl.id = t.place_id;

-- name: InviteArc :one
-- The arc and inviter of an invite, for recording it on a publish.
SELECT p.arc_id, i.inviter_id
FROM invites i
JOIN perspectives p ON p.id = i.perspective_id
WHERE i.code = $1;

-- name: SetInviteCode :exec
UPDATE perspectives SET invite_code = sqlc.arg(code)::text WHERE id = sqlc.arg(id)::uuid;

-- name: SavePerspective :exec
-- Saving twice keeps the first save and its time.
INSERT INTO saved_perspectives (user_id, perspective_id)
VALUES (sqlc.arg(user_id)::uuid, sqlc.arg(perspective_id)::uuid)
ON CONFLICT (user_id, perspective_id) DO NOTHING;

-- name: UnsavePerspective :exec
DELETE FROM saved_perspectives
WHERE user_id = sqlc.arg(user_id)::uuid AND perspective_id = sqlc.arg(perspective_id)::uuid;

-- name: ListSaved :many
-- One member's saved stories that are readable now, most recently saved
-- first, keyset-paginated on (saved_at, id); mine is served by
-- saved_perspectives_user_created_idx. A save of a story that is a draft,
-- deleted, hidden, or whose arc's place or any ancestor is hidden is left
-- out but kept, so it returns when the story is restored: the rule
-- ListByAuthor applies.
WITH RECURSIVE mine AS (
    SELECT s.perspective_id AS id, s.created_at AS saved_at, a.place_id
    FROM saved_perspectives s
    JOIN perspectives p ON p.id = s.perspective_id
    JOIN arcs a ON a.id = p.arc_id
    WHERE s.user_id = sqlc.arg(user_id)::uuid
      AND p.status = 'PERSPECTIVE_STATUS_PUBLISHED'
      AND p.deleted_at IS NULL
      AND p.hidden_at IS NULL
      AND (sqlc.narg(after_at)::timestamptz IS NULL
           OR (s.created_at, s.perspective_id) < (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid))
), up AS (
    SELECT m.id AS perspective_id, pl.parent_id, pl.hidden_at, 1 AS depth
    FROM mine m
    JOIN places pl ON pl.id = m.place_id
    UNION ALL
    SELECT up.perspective_id, parent.parent_id, parent.hidden_at, up.depth + 1
    FROM places parent
    JOIN up ON parent.id = up.parent_id
    WHERE up.depth < 8
)
SELECT m.id, m.saved_at
FROM mine m
WHERE NOT EXISTS (SELECT 1 FROM up WHERE up.perspective_id = m.id AND up.hidden_at IS NOT NULL)
ORDER BY m.saved_at DESC, m.id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: SitemapStories :many
-- Readable stories, most recently changed first: the stories listed in the
-- sitemap.
SELECT v.id, v.updated_at AS last_mod
FROM perspectives v
JOIN arcs a ON a.id = v.arc_id
WHERE v.status = 'PERSPECTIVE_STATUS_PUBLISHED' AND v.deleted_at IS NULL AND v.hidden_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM places h WHERE h.id = ANY(a.place_path) AND h.hidden_at IS NOT NULL)
ORDER BY v.updated_at DESC, v.id
LIMIT @lim::integer;
