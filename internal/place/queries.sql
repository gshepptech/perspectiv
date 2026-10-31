-- Place hierarchy reads, user-created places and the epic listings.
--
-- A place is visible when neither it nor any ancestor has hidden_at set:
-- hiding a place by moderation hides its whole subtree from every read here.
-- Recursive walks stop at depth 8; the hierarchy has five levels, so the cap
-- only matters for a corrupt cycle, which it turns into a short result rather
-- than an endless query.

-- name: GetVisiblePlace :one
WITH RECURSIVE chain AS (
    SELECT p.id, p.parent_id, p.hidden_at, 1 AS depth
    FROM places p
    WHERE p.id = @id::uuid
    UNION ALL
    SELECT a.id, a.parent_id, a.hidden_at, c.depth + 1
    FROM places a
    JOIN chain c ON a.id = c.parent_id
    WHERE c.depth < 8
)
SELECT p.id, p.kind, p.name, p.parent_id, p.timezone, p.source, p.user_added,
       coalesce(p.category, '')::text AS category
FROM places p
WHERE p.id = @id::uuid
  AND NOT EXISTS (SELECT 1 FROM chain WHERE chain.hidden_at IS NOT NULL);

-- name: ListAncestors :many
-- Every ancestor of each given place, root first within a place.
WITH RECURSIVE chain AS (
    SELECT p.id AS place_id, p.parent_id AS ancestor_id, 1 AS depth
    FROM places p
    WHERE p.id = ANY(@ids::uuid[]) AND p.parent_id IS NOT NULL
    UNION ALL
    SELECT c.place_id, a.parent_id, c.depth + 1
    FROM chain c
    JOIN places a ON a.id = c.ancestor_id
    WHERE a.parent_id IS NOT NULL AND c.depth < 8
)
SELECT c.place_id, a.id AS ancestor_id, a.kind, a.name, c.depth::integer AS depth
FROM chain c
JOIN places a ON a.id = c.ancestor_id
ORDER BY c.place_id, c.depth DESC;

-- name: SearchPlaces :many
-- Typeahead. Candidates are visible-by-row places whose lowercased name
-- contains the pattern, restricted to the descendants of parent_id when it
-- is set. Ranking: exact name, then name prefix, then substring; within
-- each, broader kinds first (STATE before STORE), then shorter names, then
-- name and id. The top candidate_limit candidates are then checked for a
-- hidden ancestor, and the first result_limit survivors returned.
--
-- Every prefix match outranks every substring match, so the prefix matches
-- (places_name_prefix_idx) are read first and the substring scan (the
-- trigram index) runs only when they leave room: a common word such as
-- "park" used to rank 15,000 substring matches to keep 100.
WITH RECURSIVE prefixed AS (
    SELECT p.id, p.kind, p.name, p.parent_id, p.timezone, p.source, p.user_added,
           coalesce(p.category, '') AS category,
           (lower(p.name) = @exact::text) AS is_exact,
           true AS is_prefix,
           array_position(ARRAY['PLACE_KIND_STATE', 'PLACE_KIND_CITY', 'PLACE_KIND_TOWN',
                                'PLACE_KIND_MALL', 'PLACE_KIND_STORE']::text[], p.kind) AS level
    FROM places p
    WHERE p.hidden_at IS NULL
      AND lower(p.name) LIKE @prefix::text
      AND (sqlc.narg(kind)::text IS NULL OR p.kind = sqlc.narg(kind)::text)
      -- Below parent_id: its id is on the place's path (places_path_idx).
      AND (sqlc.narg(parent_id)::uuid IS NULL
           OR (p.path @> ARRAY[sqlc.narg(parent_id)::uuid] AND p.id <> sqlc.narg(parent_id)::uuid))
    ORDER BY is_exact DESC, level, char_length(p.name), lower(p.name), p.id
    LIMIT @candidate_limit::integer
),
contained AS (
    SELECT p.id, p.kind, p.name, p.parent_id, p.timezone, p.source, p.user_added,
           coalesce(p.category, '') AS category,
           false AS is_exact,
           false AS is_prefix,
           array_position(ARRAY['PLACE_KIND_STATE', 'PLACE_KIND_CITY', 'PLACE_KIND_TOWN',
                                'PLACE_KIND_MALL', 'PLACE_KIND_STORE']::text[], p.kind) AS level
    FROM places p
    -- A one-time filter: with the candidates already full, no row is read.
    WHERE (SELECT count(*) FROM prefixed) < @candidate_limit::integer
      AND p.hidden_at IS NULL
      AND lower(p.name) LIKE @contains::text
      AND lower(p.name) NOT LIKE @prefix::text
      AND (sqlc.narg(kind)::text IS NULL OR p.kind = sqlc.narg(kind)::text)
      AND (sqlc.narg(parent_id)::uuid IS NULL
           OR (p.path @> ARRAY[sqlc.narg(parent_id)::uuid] AND p.id <> sqlc.narg(parent_id)::uuid))
    ORDER BY level, char_length(p.name), lower(p.name), p.id
    LIMIT @candidate_limit::integer
),
candidates AS (
    SELECT u.* FROM (SELECT * FROM prefixed UNION ALL SELECT * FROM contained) u
    ORDER BY u.is_exact DESC, u.is_prefix DESC, u.level, char_length(u.name), lower(u.name), u.id
    LIMIT @candidate_limit::integer
),
up AS (
    SELECT m.id AS place_id, m.parent_id AS ancestor_id, 1 AS depth
    FROM candidates m
    WHERE m.parent_id IS NOT NULL
    UNION ALL
    SELECT u.place_id, a.parent_id, u.depth + 1
    FROM up u
    JOIN places a ON a.id = u.ancestor_id
    WHERE a.parent_id IS NOT NULL AND u.depth < 8
)
SELECT m.id, m.kind, m.name, m.parent_id, m.timezone, m.source, m.user_added, m.category::text AS category,
       st.stories
FROM candidates m
-- Readable stories at the place and below, counted up to 100: enough to
-- tell friends which of several same-named entries everyone writes in.
CROSS JOIN LATERAL (
    SELECT count(*)::integer AS stories FROM (
        SELECT 1 FROM arcs a JOIN perspectives v ON v.arc_id = a.id
        WHERE a.place_path @> ARRAY[m.id]
          -- Stories under a hidden place are not readable, so not counted.
          AND NOT EXISTS (SELECT 1 FROM places h WHERE h.id = ANY(a.place_path) AND h.hidden_at IS NOT NULL)
          AND v.status = 'PERSPECTIVE_STATUS_PUBLISHED' AND v.deleted_at IS NULL AND v.hidden_at IS NULL
        LIMIT 100) x
) st
WHERE NOT EXISTS (
    SELECT 1 FROM up u JOIN places a ON a.id = u.ancestor_id
    WHERE u.place_id = m.id AND a.hidden_at IS NOT NULL)
-- Within each match tier, places with stories first.
ORDER BY m.is_exact DESC, m.is_prefix DESC, (st.stories > 0) DESC, m.level, char_length(m.name), lower(m.name), m.id
LIMIT @result_limit::integer;

-- name: ListStates :many
SELECT p.id, p.kind, p.name, p.parent_id, p.timezone, p.source, p.user_added
FROM places p
WHERE p.parent_id IS NULL AND p.hidden_at IS NULL
  AND (sqlc.narg(after_id)::uuid IS NULL
       OR (lower(p.name), p.id) > (sqlc.narg(after_name)::text, sqlc.narg(after_id)::uuid))
ORDER BY lower(p.name), p.id
LIMIT @lim::integer;

-- name: ListChildren :many
-- A parent's visible children in name order, ignoring leading quotes and
-- symbols; served by places_parent_sort_idx. sort_key is the cursor.
SELECT p.id, p.kind, p.name, p.parent_id, p.timezone, p.source, p.user_added,
       lower(regexp_replace(p.name, '^[^[:alnum:]]+', ''))::text AS sort_key,
       coalesce(p.category, '')::text AS category
FROM places p
WHERE p.parent_id = @parent_id::uuid AND p.hidden_at IS NULL
  AND (sqlc.narg(category)::text IS NULL OR p.category = sqlc.narg(category)::text)
  AND (sqlc.narg(after_id)::uuid IS NULL
       OR (lower(regexp_replace(p.name, '^[^[:alnum:]]+', '')), p.id) > (sqlc.narg(after_name)::text, sqlc.narg(after_id)::uuid))
ORDER BY lower(regexp_replace(p.name, '^[^[:alnum:]]+', '')), p.id
LIMIT @lim::integer;

-- name: ListStoriedChildren :many
-- category is '' for a place with none.
-- The children of a parent (the states when parent_id is NULL) with at least
-- one readable story at or below them, most recently started arc first.
-- Found from the arcs' place paths, so a state with a million places costs
-- what its arcs cost.
SELECT p.id, p.kind, p.name, p.parent_id, p.timezone, p.source, p.user_added,
       coalesce(p.category, '')::text AS category
FROM (
    SELECT CASE WHEN sqlc.narg(parent_id)::uuid IS NULL THEN a.place_path[1]
                ELSE a.place_path[array_position(a.place_path, sqlc.narg(parent_id)::uuid) + 1] END AS child_id,
           max(a.created_at) AS latest
    FROM arcs a
    WHERE (sqlc.narg(parent_id)::uuid IS NULL OR a.place_path @> ARRAY[sqlc.narg(parent_id)::uuid])
      AND NOT EXISTS (SELECT 1 FROM places h WHERE h.id = ANY(a.place_path) AND h.hidden_at IS NOT NULL)
      AND EXISTS (SELECT 1 FROM perspectives v
                  WHERE v.arc_id = a.id AND v.status = 'PERSPECTIVE_STATUS_PUBLISHED'
                    AND v.deleted_at IS NULL AND v.hidden_at IS NULL)
    GROUP BY 1
) t
JOIN places p ON p.id = t.child_id
ORDER BY t.latest DESC, p.id
LIMIT @lim::integer;

-- name: LockVisibleParent :one
-- Holds the parent row against a concurrent hide until the insert commits.
SELECT p.kind, p.timezone
FROM places p
WHERE p.id = @id::uuid AND p.hidden_at IS NULL
FOR SHARE;

-- name: InsertUserPlace :one
INSERT INTO places (kind, name, parent_id, timezone, source, user_added, created_by)
VALUES (@kind::text, @name::text, @parent_id::uuid, @timezone::text, 'PLACE_SOURCE_USER', true, @created_by::uuid)
RETURNING id, kind, name, parent_id, timezone, source, user_added;

-- name: EpicArcsRanked :many
-- Arcs at the place and every visible descendant, found by arcs.place_path
-- (arcs_place_path_idx) (a listing only: each
-- arc keeps its own stored canon). An arc is listed when it has at least one
-- readable perspective, of the genre when one is given. Order: arcs with a
-- canon first, then the stored canon score, member count, newest, id.
SELECT a.id, a.title, a.place_id, pl.kind AS place_kind, pl.name AS place_name,
       a.local_date, a.hour, a.canon_lore_id, a.canon_score, a.member_count, a.created_at,
       (SELECT count(*) FROM perspectives v
        WHERE v.arc_id = a.id AND v.status = 'PERSPECTIVE_STATUS_PUBLISHED'
          AND v.deleted_at IS NULL AND v.hidden_at IS NULL)::integer AS perspective_count,
       -- The city the arc is in, for a listing that spans cities.
       coalesce((SELECT c.name FROM places c
                 WHERE c.id = ANY(a.place_path) AND c.kind = 'PLACE_KIND_CITY' LIMIT 1), '')::text AS city_name,
       -- The opening of the canon's first story, or of the first story when
       -- there is no canon: the line a listing quotes.
       coalesce((SELECT left(pp.body, 240) FROM perspectives v
                 JOIN perspective_parts pp ON pp.perspective_id = v.id
                 WHERE v.arc_id = a.id AND v.status = 'PERSPECTIVE_STATUS_PUBLISHED'
                   AND v.deleted_at IS NULL AND v.hidden_at IS NULL
                 ORDER BY (v.lore_id IS NOT DISTINCT FROM a.canon_lore_id) DESC, v.published_at, pp.position
                 LIMIT 1), '')::text AS excerpt,
       -- The canon lore's name, so a listing can say which lore is canon.
       coalesce((SELECT l.name FROM lores l WHERE l.id = a.canon_lore_id), '')::text AS canon_lore_name
FROM arcs a
JOIN places pl ON pl.id = a.place_id
WHERE a.place_path @> ARRAY[@place_id::uuid]
  -- Hiding a place hides every arc below it.
  AND NOT EXISTS (SELECT 1 FROM places h WHERE h.id = ANY(a.place_path) AND h.hidden_at IS NOT NULL)
  AND EXISTS (
        SELECT 1 FROM perspectives v
        WHERE v.arc_id = a.id AND v.status = 'PERSPECTIVE_STATUS_PUBLISHED'
          AND v.deleted_at IS NULL AND v.hidden_at IS NULL
          AND (sqlc.narg(genre)::text IS NULL OR v.genre = sqlc.narg(genre)::text))
  AND (sqlc.narg(after_id)::uuid IS NULL
       OR (a.canon_lore_id IS NOT NULL, a.canon_score, a.member_count, a.created_at, a.id)
          < (sqlc.narg(after_has_canon)::boolean, sqlc.narg(after_score)::double precision,
             sqlc.narg(after_members)::integer, sqlc.narg(after_created)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY (a.canon_lore_id IS NOT NULL) DESC, a.canon_score DESC, a.member_count DESC, a.created_at DESC, a.id DESC
LIMIT @lim::integer;

-- name: EpicArcsNewest :many
-- The same arcs as EpicArcsRanked, newest first.
SELECT a.id, a.title, a.place_id, pl.kind AS place_kind, pl.name AS place_name,
       a.local_date, a.hour, a.canon_lore_id, a.canon_score, a.member_count, a.created_at,
       (SELECT count(*) FROM perspectives v
        WHERE v.arc_id = a.id AND v.status = 'PERSPECTIVE_STATUS_PUBLISHED'
          AND v.deleted_at IS NULL AND v.hidden_at IS NULL)::integer AS perspective_count,
       -- The city the arc is in, for a listing that spans cities.
       coalesce((SELECT c.name FROM places c
                 WHERE c.id = ANY(a.place_path) AND c.kind = 'PLACE_KIND_CITY' LIMIT 1), '')::text AS city_name,
       -- The opening of the canon's first story, or of the first story when
       -- there is no canon: the line a listing quotes.
       coalesce((SELECT left(pp.body, 240) FROM perspectives v
                 JOIN perspective_parts pp ON pp.perspective_id = v.id
                 WHERE v.arc_id = a.id AND v.status = 'PERSPECTIVE_STATUS_PUBLISHED'
                   AND v.deleted_at IS NULL AND v.hidden_at IS NULL
                 ORDER BY (v.lore_id IS NOT DISTINCT FROM a.canon_lore_id) DESC, v.published_at, pp.position
                 LIMIT 1), '')::text AS excerpt,
       -- The canon lore's name, so a listing can say which lore is canon.
       coalesce((SELECT l.name FROM lores l WHERE l.id = a.canon_lore_id), '')::text AS canon_lore_name
FROM arcs a
JOIN places pl ON pl.id = a.place_id
WHERE a.place_path @> ARRAY[@place_id::uuid]
  -- Hiding a place hides every arc below it.
  AND NOT EXISTS (SELECT 1 FROM places h WHERE h.id = ANY(a.place_path) AND h.hidden_at IS NOT NULL)
  AND EXISTS (
        SELECT 1 FROM perspectives v
        WHERE v.arc_id = a.id AND v.status = 'PERSPECTIVE_STATUS_PUBLISHED'
          AND v.deleted_at IS NULL AND v.hidden_at IS NULL
          AND (sqlc.narg(genre)::text IS NULL OR v.genre = sqlc.narg(genre)::text))
  AND (sqlc.narg(after_id)::uuid IS NULL
       OR (a.created_at, a.id) < (sqlc.narg(after_created)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY a.created_at DESC, a.id DESC
LIMIT @lim::integer;

-- name: EverywhereArcsRanked :many
-- EpicArcsRanked across every visible place: the front page. A separate
-- query rather than an optional place filter, so the place listings keep
-- their plan on arcs_place_path_idx.
SELECT a.id, a.title, a.place_id, pl.kind AS place_kind, pl.name AS place_name,
       a.local_date, a.hour, a.canon_lore_id, a.canon_score, a.member_count, a.created_at,
       (SELECT count(*) FROM perspectives v
        WHERE v.arc_id = a.id AND v.status = 'PERSPECTIVE_STATUS_PUBLISHED'
          AND v.deleted_at IS NULL AND v.hidden_at IS NULL)::integer AS perspective_count,
       -- The city the arc is in, for a listing that spans cities.
       coalesce((SELECT c.name FROM places c
                 WHERE c.id = ANY(a.place_path) AND c.kind = 'PLACE_KIND_CITY' LIMIT 1), '')::text AS city_name,
       -- The opening of the canon's first story, or of the first story when
       -- there is no canon: the line a listing quotes.
       coalesce((SELECT left(pp.body, 240) FROM perspectives v
                 JOIN perspective_parts pp ON pp.perspective_id = v.id
                 WHERE v.arc_id = a.id AND v.status = 'PERSPECTIVE_STATUS_PUBLISHED'
                   AND v.deleted_at IS NULL AND v.hidden_at IS NULL
                 ORDER BY (v.lore_id IS NOT DISTINCT FROM a.canon_lore_id) DESC, v.published_at, pp.position
                 LIMIT 1), '')::text AS excerpt,
       -- The canon lore's name, so a listing can say which lore is canon.
       coalesce((SELECT l.name FROM lores l WHERE l.id = a.canon_lore_id), '')::text AS canon_lore_name
FROM arcs a
JOIN places pl ON pl.id = a.place_id
WHERE NOT EXISTS (SELECT 1 FROM places h WHERE h.id = ANY(a.place_path) AND h.hidden_at IS NOT NULL)
  AND EXISTS (
        SELECT 1 FROM perspectives v
        WHERE v.arc_id = a.id AND v.status = 'PERSPECTIVE_STATUS_PUBLISHED'
          AND v.deleted_at IS NULL AND v.hidden_at IS NULL
          AND (sqlc.narg(genre)::text IS NULL OR v.genre = sqlc.narg(genre)::text))
  AND (sqlc.narg(after_id)::uuid IS NULL
       OR (a.canon_lore_id IS NOT NULL, a.canon_score, a.member_count, a.created_at, a.id)
          < (sqlc.narg(after_has_canon)::boolean, sqlc.narg(after_score)::double precision,
             sqlc.narg(after_members)::integer, sqlc.narg(after_created)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY (a.canon_lore_id IS NOT NULL) DESC, a.canon_score DESC, a.member_count DESC, a.created_at DESC, a.id DESC
LIMIT @lim::integer;

-- name: EverywhereArcsNewest :many
-- EverywhereArcsRanked, newest first.
SELECT a.id, a.title, a.place_id, pl.kind AS place_kind, pl.name AS place_name,
       a.local_date, a.hour, a.canon_lore_id, a.canon_score, a.member_count, a.created_at,
       (SELECT count(*) FROM perspectives v
        WHERE v.arc_id = a.id AND v.status = 'PERSPECTIVE_STATUS_PUBLISHED'
          AND v.deleted_at IS NULL AND v.hidden_at IS NULL)::integer AS perspective_count,
       -- The city the arc is in, for a listing that spans cities.
       coalesce((SELECT c.name FROM places c
                 WHERE c.id = ANY(a.place_path) AND c.kind = 'PLACE_KIND_CITY' LIMIT 1), '')::text AS city_name,
       -- The opening of the canon's first story, or of the first story when
       -- there is no canon: the line a listing quotes.
       coalesce((SELECT left(pp.body, 240) FROM perspectives v
                 JOIN perspective_parts pp ON pp.perspective_id = v.id
                 WHERE v.arc_id = a.id AND v.status = 'PERSPECTIVE_STATUS_PUBLISHED'
                   AND v.deleted_at IS NULL AND v.hidden_at IS NULL
                 ORDER BY (v.lore_id IS NOT DISTINCT FROM a.canon_lore_id) DESC, v.published_at, pp.position
                 LIMIT 1), '')::text AS excerpt,
       -- The canon lore's name, so a listing can say which lore is canon.
       coalesce((SELECT l.name FROM lores l WHERE l.id = a.canon_lore_id), '')::text AS canon_lore_name
FROM arcs a
JOIN places pl ON pl.id = a.place_id
WHERE NOT EXISTS (SELECT 1 FROM places h WHERE h.id = ANY(a.place_path) AND h.hidden_at IS NOT NULL)
  AND EXISTS (
        SELECT 1 FROM perspectives v
        WHERE v.arc_id = a.id AND v.status = 'PERSPECTIVE_STATUS_PUBLISHED'
          AND v.deleted_at IS NULL AND v.hidden_at IS NULL
          AND (sqlc.narg(genre)::text IS NULL OR v.genre = sqlc.narg(genre)::text))
  AND (sqlc.narg(after_id)::uuid IS NULL
       OR (a.created_at, a.id) < (sqlc.narg(after_created)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY a.created_at DESC, a.id DESC
LIMIT @lim::integer;

-- name: EpicTales :many
-- SOLO and REMOVED perspectives in arcs at the place and every visible
-- descendant, newest published first. Deleted and hidden perspectives are
-- left out. The excerpt is the first 200 characters of the first part.
SELECT v.id, v.arc_id, a.title AS arc_title, a.place_id, pl.kind AS place_kind, pl.name AS place_name,
       a.local_date, a.hour, v.author_id, u.username, v.genre, v.lore_status, v.published_at,
       coalesce((SELECT left(pp.body, 240) FROM perspective_parts pp
                 WHERE pp.perspective_id = v.id ORDER BY pp.position LIMIT 1), '')::text AS excerpt
FROM perspectives v
JOIN arcs a ON a.id = v.arc_id
JOIN places pl ON pl.id = a.place_id
JOIN users u ON u.id = v.author_id
WHERE a.place_path @> ARRAY[@place_id::uuid]
  -- Hiding a place hides every arc below it.
  AND NOT EXISTS (SELECT 1 FROM places h WHERE h.id = ANY(a.place_path) AND h.hidden_at IS NOT NULL)
  AND v.lore_status IN ('LORE_STATUS_SOLO', 'LORE_STATUS_REMOVED')
  AND v.status = 'PERSPECTIVE_STATUS_PUBLISHED'
  AND v.deleted_at IS NULL AND v.hidden_at IS NULL
  AND (sqlc.narg(genre)::text IS NULL OR v.genre = sqlc.narg(genre)::text)
  AND (sqlc.narg(after_id)::uuid IS NULL
       OR (v.published_at, v.id) < (sqlc.narg(after_published)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY v.published_at DESC, v.id DESC
LIMIT @lim::integer;

-- name: FollowPlace :exec
INSERT INTO place_follows (user_id, place_id) VALUES (@user_id::uuid, @place_id::uuid)
ON CONFLICT (user_id, place_id) DO NOTHING;

-- name: UnfollowPlace :exec
DELETE FROM place_follows WHERE user_id = @user_id::uuid AND place_id = @place_id::uuid;

-- name: IsFollowing :one
SELECT EXISTS (SELECT 1 FROM place_follows WHERE user_id = @user_id::uuid AND place_id = @place_id::uuid)::boolean AS following;

-- name: FollowedPlaceIDs :many
-- The places a user follows, most recently followed first.
SELECT place_id FROM place_follows WHERE user_id = @user_id::uuid
ORDER BY created_at DESC, place_id
LIMIT @lim::integer;

-- name: RecentStoriesUnder :many
-- For each given place, readable stories at it or below published since the
-- cutoff, counted up to 100: what a follower has not seen yet this week.
SELECT f.id::uuid AS place_id, rc.recent
FROM unnest(@ids::uuid[]) AS f(id)
CROSS JOIN LATERAL (
    SELECT count(*)::integer AS recent FROM (
        SELECT 1 FROM arcs a JOIN perspectives v ON v.arc_id = a.id
        WHERE a.place_path @> ARRAY[f.id]
          AND NOT EXISTS (SELECT 1 FROM places h WHERE h.id = ANY(a.place_path) AND h.hidden_at IS NOT NULL)
          AND v.status = 'PERSPECTIVE_STATUS_PUBLISHED' AND v.deleted_at IS NULL AND v.hidden_at IS NULL
          AND v.published_at > @since::timestamptz
        LIMIT 100) x
) rc;

-- name: CharactersLabelled :many
-- Up to cap characters anywhere whose label contains the pattern, through
-- characters_label_trgm_idx. Fewer than cap means the word is rare enough to
-- search by label first.
SELECT c.id FROM characters c WHERE c.label ILIKE '%' || @pattern::text || '%' LIMIT @cap::integer;

-- name: SearchCharacters :many
-- Unclaimed characters (no pending or accepted claim) in readable stories
-- at the place or any visible place under it, found through
-- arcs_place_path_idx, whose label contains the pattern. The caller escapes
-- LIKE's wildcards. Voted-out stories cannot be claimed from and are left out.
SELECT c.id, c.label, c.perspective_id, p.author_id, u.username,
       a.id AS arc_id, a.title AS arc_title, a.local_date, a.hour,
       a.place_id, pl.kind AS place_kind, pl.name AS place_name
FROM arcs a
JOIN places pl ON pl.id = a.place_id
JOIN perspectives p ON p.arc_id = a.id
JOIN users u ON u.id = p.author_id
JOIN characters c ON c.perspective_id = p.id
WHERE a.place_path @> ARRAY[@place_id::uuid]
  AND NOT EXISTS (SELECT 1 FROM places h WHERE h.id = ANY(a.place_path) AND h.hidden_at IS NOT NULL)
  AND p.status = 'PERSPECTIVE_STATUS_PUBLISHED'
  AND p.deleted_at IS NULL AND p.hidden_at IS NULL
  AND p.lore_status <> 'LORE_STATUS_REMOVED'
  AND u.deleted_at IS NULL
  AND c.label ILIKE '%' || @pattern::text || '%'
  -- When the caller found the matching labels first (a rare word), only
  -- those; otherwise the place's stories are walked newest first.
  AND (sqlc.narg(ids)::uuid[] IS NULL OR c.id = ANY(sqlc.narg(ids)::uuid[]))
  AND NOT EXISTS (
        SELECT 1 FROM claims cl
        WHERE cl.character_id = c.id AND cl.status IN ('CLAIM_STATUS_PENDING', 'CLAIM_STATUS_ACCEPTED'))
ORDER BY a.local_date DESC, a.hour DESC NULLS LAST, p.published_at, c.position
LIMIT 30;

-- name: SitemapPlaces :many
-- Places a signed-out reader can open that have a readable story at them or
-- below, most recently written first, with when the newest was published:
-- the places listed in the sitemap.
SELECT pid::uuid AS id, max(v.published_at)::timestamptz AS last_mod
FROM arcs a
JOIN perspectives v ON v.arc_id = a.id
CROSS JOIN LATERAL unnest(a.place_path) AS pid
WHERE v.status = 'PERSPECTIVE_STATUS_PUBLISHED' AND v.deleted_at IS NULL AND v.hidden_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM places h WHERE h.id = ANY(a.place_path) AND h.hidden_at IS NOT NULL)
GROUP BY pid
ORDER BY last_mod DESC, pid
LIMIT @lim::integer;
