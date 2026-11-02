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
-- The author's perspective in the arc that is not deleted, published or not.
SELECT id
FROM perspectives
WHERE arc_id = sqlc.arg(arc_id)::uuid
  AND author_id = sqlc.arg(author_id)::uuid
  AND deleted_at IS NULL;

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

-- name: ArcClaimEdges :many
-- The accepted claims in one arc as perspective pairs: the declaring
-- perspective and the claimant's. Groups are found from these in Go; a
-- recursive walk here grew with the square of a group's size.
SELECT ch.perspective_id AS declarer, c.claimant_perspective_id AS claimant
FROM perspectives p
JOIN characters ch ON ch.perspective_id = p.id
JOIN claims c ON c.character_id = ch.id
WHERE p.arc_id = sqlc.arg(arc_id)::uuid
  AND c.status = sqlc.arg(accepted)::text;

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
ORDER BY p.published_at, m.perspective_id;

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
-- paged on (created_at, id). claimant_opening is the first 241 characters of
-- the claimant's story, whitespace folded, while anyone may read it (published, neither deleted
-- nor hidden, not filed at or below a hidden place), else ''; claimExcerpt
-- cuts it at a word. place_name, local_date and hour say where and when the
-- story declaring the character is set. ListOutgoingClaims returns the same
-- columns.
SELECT c.id, p.arc_id, c.character_id, ch.label, ch.perspective_id,
       p.author_id, au.username AS author_username,
       c.claimant_user_id, cu.username AS claimant_username,
       c.claimant_perspective_id, c.status, c.created_at, c.resolved_at,
       coalesce(pl.name, '')::text AS place_name, ar.local_date, ar.hour,
       COALESCE((SELECT left(regexp_replace(btrim(pp.body), '\s+', ' ', 'g'), 241) FROM perspective_parts pp
                 JOIN perspectives cp ON cp.id = pp.perspective_id
                 JOIN arcs ca ON ca.id = cp.arc_id
                 WHERE pp.perspective_id = c.claimant_perspective_id
                   AND cp.status = 'PERSPECTIVE_STATUS_PUBLISHED'
                   AND cp.deleted_at IS NULL AND cp.hidden_at IS NULL
                   AND NOT EXISTS (SELECT 1 FROM places h
                                   WHERE h.id = ANY(ca.place_path) AND h.hidden_at IS NOT NULL)
                 ORDER BY pp.position LIMIT 1), '')::text AS claimant_opening
FROM claims c
JOIN characters ch ON ch.id = c.character_id
JOIN perspectives p ON p.id = ch.perspective_id
JOIN users au ON au.id = p.author_id
JOIN users cu ON cu.id = c.claimant_user_id
LEFT JOIN arcs ar ON ar.id = p.arc_id
LEFT JOIN places pl ON pl.id = ar.place_id
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
       c.claimant_perspective_id, c.status, c.created_at, c.resolved_at,
       coalesce(pl.name, '')::text AS place_name, ar.local_date, ar.hour,
       COALESCE((SELECT left(regexp_replace(btrim(pp.body), '\s+', ' ', 'g'), 241) FROM perspective_parts pp
                 JOIN perspectives cp ON cp.id = pp.perspective_id
                 JOIN arcs ca ON ca.id = cp.arc_id
                 WHERE pp.perspective_id = c.claimant_perspective_id
                   AND cp.status = 'PERSPECTIVE_STATUS_PUBLISHED'
                   AND cp.deleted_at IS NULL AND cp.hidden_at IS NULL
                   AND NOT EXISTS (SELECT 1 FROM places h
                                   WHERE h.id = ANY(ca.place_path) AND h.hidden_at IS NOT NULL)
                 ORDER BY pp.position LIMIT 1), '')::text AS claimant_opening
FROM claims c
JOIN characters ch ON ch.id = c.character_id
JOIN perspectives p ON p.id = ch.perspective_id
JOIN users au ON au.id = p.author_id
JOIN users cu ON cu.id = c.claimant_user_id
LEFT JOIN arcs ar ON ar.id = p.arc_id
LEFT JOIN places pl ON pl.id = ar.place_id
WHERE c.claimant_user_id = sqlc.arg(user_id)::uuid
  AND (sqlc.narg(status)::text IS NULL OR c.status = sqlc.narg(status)::text)
  AND (NOT sqlc.arg(has_after)::boolean
       OR (c.created_at, c.id) < (sqlc.arg(after_created_at)::timestamptz, sqlc.arg(after_id)::uuid))
ORDER BY c.created_at DESC, c.id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: GetLore :one
-- The lore, and whether its arc's place or any of the seven places above it
-- is hidden (ArcPlaceHidden's rule), read in the same statement.
WITH RECURSIVE up AS (
    SELECT pl.parent_id, pl.hidden_at, 1 AS depth
    FROM lores l0
    JOIN arcs a ON a.id = l0.arc_id
    JOIN places pl ON pl.id = a.place_id
    WHERE l0.id = @id::uuid
    UNION ALL
    SELECT parent.parent_id, parent.hidden_at, up.depth + 1
    FROM places parent
    JOIN up ON parent.id = up.parent_id
    WHERE up.depth < 8
)
SELECT l.id, l.arc_id, l.status, l.member_count, l.upvotes, l.rank_score, l.computed_at,
       coalesce(l.name, '')::text AS name, l.named_by, coalesce(nb.username, '')::text AS named_by_username,
       EXISTS (SELECT 1 FROM up WHERE up.hidden_at IS NOT NULL)::boolean AS place_hidden
FROM lores l
LEFT JOIN users nb ON nb.id = l.named_by AND nb.deleted_at IS NULL
WHERE l.id = @id::uuid;

-- name: IsLoreMember :one
-- Whether the user wrote a readable perspective that is a member of the lore.
SELECT EXISTS (
    SELECT 1 FROM lore_members lm
    JOIN perspectives p ON p.id = lm.perspective_id
    WHERE lm.lore_id = sqlc.arg(lore_id)::uuid
      AND p.author_id = sqlc.arg(user_id)::uuid
      AND p.status = 'PERSPECTIVE_STATUS_PUBLISHED'
      AND p.deleted_at IS NULL AND p.hidden_at IS NULL
)::boolean AS is_member;

-- name: LoreNameClearedAt :one
-- When a moderator last cleared the lore's name; NULL when never.
SELECT name_cleared_at FROM lores WHERE id = sqlc.arg(id)::uuid;

-- name: SetLoreName :exec
UPDATE lores SET name = sqlc.arg(name)::text, named_by = sqlc.arg(named_by)::uuid
WHERE id = sqlc.arg(id)::uuid;

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

-- name: HasActiveClaimInPerspective :one
-- Whether the claimant already has a PENDING or ACCEPTED claim on any
-- character in the perspective: a person is one character per story.
SELECT EXISTS (
    SELECT 1 FROM claims c
    JOIN characters ch ON ch.id = c.character_id
    WHERE ch.perspective_id = sqlc.arg(perspective_id)::uuid
      AND c.claimant_user_id = sqlc.arg(claimant_user_id)::uuid
      AND c.status IN ('CLAIM_STATUS_PENDING', 'CLAIM_STATUS_ACCEPTED')
)::boolean AS active;

-- name: CanPostToLore :one
-- Whether the user may post to the lore's thread: a readable published
-- perspective in the lore's arc (members have one by definition).
SELECT EXISTS (
    SELECT 1 FROM lores l
    JOIN perspectives p ON p.arc_id = l.arc_id
    WHERE l.id = sqlc.arg(lore_id)::uuid
      AND p.author_id = sqlc.arg(user_id)::uuid
      AND p.status = 'PERSPECTIVE_STATUS_PUBLISHED'
      AND p.deleted_at IS NULL AND p.hidden_at IS NULL
)::boolean AS can_post;

-- name: ListThreadTop :many
-- A lore's top-level posts, most upvoted first and ties oldest first,
-- keyset-paged on (upvotes DESC, created_at, id) and served by
-- lore_posts_top_idx. A hidden post is left out, and so is a removed one
-- unless it still has readable replies, which it then stands above as
-- "[deleted]".
SELECT v.*
FROM lore_thread_posts v
WHERE v.lore_id = sqlc.arg(lore_id)::uuid
  AND v.reply_to_id IS NULL AND v.hidden_at IS NULL
  AND (NOT v.removed OR v.reply_count > 0)
  AND (NOT sqlc.arg(has_after)::boolean
       OR v.upvotes < sqlc.arg(after_upvotes)::int
       OR (v.upvotes = sqlc.arg(after_upvotes)::int
           AND (v.created_at, v.id) > (sqlc.arg(after_at)::timestamptz, sqlc.arg(after_id)::uuid)))
ORDER BY v.upvotes DESC, v.created_at, v.id
LIMIT sqlc.arg(row_limit)::int;

-- name: ListThreadNew :many
-- The same posts as ListThreadTop, newest first, keyset-paged on
-- (created_at, id) descending and served by lore_posts_new_idx.
SELECT v.*
FROM lore_thread_posts v
WHERE v.lore_id = sqlc.arg(lore_id)::uuid
  AND v.reply_to_id IS NULL AND v.hidden_at IS NULL
  AND (NOT v.removed OR v.reply_count > 0)
  AND (NOT sqlc.arg(has_after)::boolean
       OR (v.created_at, v.id) < (sqlc.arg(after_at)::timestamptz, sqlc.arg(after_id)::uuid))
ORDER BY v.created_at DESC, v.id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: ListFirstReplies :many
-- The first per_post readable replies under each of the posts, oldest
-- first, one index probe per post on lore_posts_replies_idx.
SELECT r.*
FROM unnest(sqlc.arg(post_ids)::uuid[]) AS t(id)
CROSS JOIN LATERAL (
    SELECT v.*
    FROM lore_thread_posts v
    WHERE v.reply_to_id = t.id AND v.hidden_at IS NULL AND NOT v.removed
    ORDER BY v.created_at, v.id
    LIMIT sqlc.arg(per_post)::int
) r
ORDER BY r.reply_to_id, r.created_at, r.id;

-- name: ListReplies :many
-- Readable replies under one post, oldest first, keyset-paged on
-- (created_at, id).
SELECT v.*
FROM lore_thread_posts v
WHERE v.reply_to_id = sqlc.arg(post_id)::uuid AND v.hidden_at IS NULL AND NOT v.removed
  AND (NOT sqlc.arg(has_after)::boolean
       OR (v.created_at, v.id) > (sqlc.arg(after_at)::timestamptz, sqlc.arg(after_id)::uuid))
ORDER BY v.created_at, v.id
LIMIT sqlc.arg(row_limit)::int;

-- name: CountThreadPosts :one
-- The posts a reader of the lore's thread sees, replies included: neither
-- deleted nor hidden, by a live account, and a reply only while its
-- top-level post is not hidden. Top-level posts come through
-- lore_posts_top_idx and replies through lore_posts_replies_idx.
SELECT (
    (SELECT count(*) FROM lore_posts t JOIN users tu ON tu.id = t.author_id
     WHERE t.lore_id = @lore_id::uuid AND t.reply_to_id IS NULL
       AND t.deleted_at IS NULL AND t.hidden_at IS NULL AND tu.deleted_at IS NULL)
  + (SELECT count(*) FROM lore_posts t
     JOIN lore_posts r ON r.reply_to_id = t.id
     JOIN users ru ON ru.id = r.author_id
     WHERE t.lore_id = @lore_id::uuid AND t.reply_to_id IS NULL AND t.hidden_at IS NULL
       AND r.deleted_at IS NULL AND r.hidden_at IS NULL AND ru.deleted_at IS NULL)
)::integer AS posts;

-- name: GetThreadPost :one
SELECT v.* FROM lore_thread_posts v WHERE v.id = $1;

-- name: ViewerUpvoted :many
-- Which of the posts the user holds an upvote on.
SELECT post_id FROM lore_post_votes
WHERE user_id = sqlc.arg(user_id)::uuid AND post_id = ANY(sqlc.arg(post_ids)::uuid[]);

-- name: LockLorePost :one
-- One post with what the thread's write rules read, its row locked. A
-- reply's top-level post decides whether a moderator hid the whole branch.
SELECT lp.id, lp.lore_id, lp.author_id, lp.reply_to_id, lp.deleted_at, lp.hidden_at,
       u.deleted_at AS author_deleted_at,
       (top.hidden_at IS NOT NULL)::boolean AS branch_hidden
FROM lore_posts lp
JOIN users u ON u.id = lp.author_id
LEFT JOIN lore_posts top ON top.id = lp.reply_to_id
WHERE lp.id = $1
FOR UPDATE OF lp;

-- name: InsertLorePost :one
INSERT INTO lore_posts (lore_id, author_id, body, reply_to_id, answered_id)
VALUES (sqlc.arg(lore_id)::uuid, sqlc.arg(author_id)::uuid, sqlc.arg(body)::text,
        sqlc.narg(reply_to_id)::uuid, sqlc.narg(answered_id)::uuid)
RETURNING id;

-- name: EditLorePost :exec
-- Stamps edited_at only when the text changes.
UPDATE lore_posts
SET body = sqlc.arg(body)::text, edited_at = now()
WHERE id = sqlc.arg(id)::uuid AND body <> sqlc.arg(body)::text;

-- name: DeleteLorePost :exec
UPDATE lore_posts SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL;

-- name: InsertPostVote :exec
-- The primary key keeps one upvote per person, so a repeat is a no-op.
INSERT INTO lore_post_votes (post_id, user_id)
VALUES (sqlc.arg(post_id)::uuid, sqlc.arg(user_id)::uuid)
ON CONFLICT (post_id, user_id) DO NOTHING;

-- name: DeletePostVote :exec
DELETE FROM lore_post_votes
WHERE post_id = sqlc.arg(post_id)::uuid AND user_id = sqlc.arg(user_id)::uuid;

-- name: RecountPostVotes :one
-- Sets the post's upvotes from its vote rows; the caller holds its row lock.
UPDATE lore_posts
SET upvotes = (SELECT count(*) FROM lore_post_votes WHERE post_id = sqlc.arg(id)::uuid)
WHERE id = sqlc.arg(id)::uuid
RETURNING upvotes;
