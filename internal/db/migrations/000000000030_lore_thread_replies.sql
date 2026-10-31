-- +goose Up
-- A lore's thread grows replies, upvotes, edits and deletes.
--
-- A post is top-level (reply_to_id NULL) or a reply to a top-level post.
-- reply_to_id always names the top-level post, so a thread is two levels
-- deep; answered_id names the post the reply answered, which is the
-- top-level post or another reply under it. upvotes is the count of
-- lore_post_votes rows, kept by the lore service in the vote's transaction
-- so the thread can be keyset-paged by score.
ALTER TABLE lore_posts
    ADD COLUMN IF NOT EXISTS reply_to_id uuid        NULL REFERENCES lore_posts (id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS answered_id uuid        NULL REFERENCES lore_posts (id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS edited_at   timestamptz NULL,
    ADD COLUMN IF NOT EXISTS upvotes     integer     NOT NULL DEFAULT 0;
ALTER TABLE lore_posts DROP CONSTRAINT IF EXISTS lore_posts_reply_check;
ALTER TABLE lore_posts ADD CONSTRAINT lore_posts_reply_check
    CHECK ((reply_to_id IS NULL) = (answered_id IS NULL) AND reply_to_id IS DISTINCT FROM id);
ALTER TABLE lore_posts DROP CONSTRAINT IF EXISTS lore_posts_upvotes_check;
ALTER TABLE lore_posts ADD CONSTRAINT lore_posts_upvotes_check CHECK (upvotes >= 0);

-- Top-level posts, paged best first (Top) or newest first (New); replies
-- under one post, oldest first.
DROP INDEX IF EXISTS lore_posts_lore_created_idx;
CREATE INDEX IF NOT EXISTS lore_posts_top_idx ON lore_posts (lore_id, upvotes DESC, created_at, id) WHERE reply_to_id IS NULL;
CREATE INDEX IF NOT EXISTS lore_posts_new_idx ON lore_posts (lore_id, created_at DESC, id DESC) WHERE reply_to_id IS NULL;
CREATE INDEX IF NOT EXISTS lore_posts_replies_idx ON lore_posts (reply_to_id, created_at, id) WHERE reply_to_id IS NOT NULL;

-- One upvote per person per post; there is no downvote.
CREATE TABLE IF NOT EXISTS lore_post_votes (
    post_id    uuid        NOT NULL REFERENCES lore_posts (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (post_id, user_id)
);
-- The member's data export lists their votes.
CREATE INDEX IF NOT EXISTS lore_post_votes_user_idx ON lore_post_votes (user_id);

-- One thread post as readers see it. removed is true for a post its author
-- deleted or whose author deleted their account; the thread keeps such a
-- top-level post as "[deleted]" while it has readable replies. replying_to
-- is the username behind the reply this post answered, or '' when it
-- answered its top-level post or that reply is no longer readable. reply_count
-- counts a top-level post's readable replies.
CREATE OR REPLACE VIEW lore_thread_posts AS
SELECT lp.id, lp.lore_id, lp.reply_to_id, lp.answered_id, lp.author_id, u.username,
       lp.body, lp.created_at, lp.edited_at, lp.upvotes, lp.hidden_at,
       (lp.deleted_at IS NOT NULL OR u.deleted_at IS NOT NULL)::boolean AS removed,
       EXISTS (SELECT 1 FROM lore_members m JOIN perspectives p ON p.id = m.perspective_id
               WHERE m.lore_id = lp.lore_id AND p.author_id = lp.author_id) AS by_member,
       coalesce((SELECT au.username FROM lore_posts a JOIN users au ON au.id = a.author_id
                 WHERE a.id = lp.answered_id AND a.id <> lp.reply_to_id
                   AND a.deleted_at IS NULL AND a.hidden_at IS NULL AND au.deleted_at IS NULL), '')::text AS replying_to,
       (SELECT count(*) FROM lore_posts r JOIN users ru ON ru.id = r.author_id
        WHERE r.reply_to_id = lp.id
          AND r.deleted_at IS NULL AND r.hidden_at IS NULL AND ru.deleted_at IS NULL)::integer AS reply_count
FROM lore_posts lp
JOIN users u ON u.id = lp.author_id;

ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_kind_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_kind_check CHECK (kind IN (
    'NOTIFICATION_KIND_CLAIM_RECEIVED', 'NOTIFICATION_KIND_CLAIM_ACCEPTED',
    'NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC', 'NOTIFICATION_KIND_VOTE_OUT_OPENED',
    'NOTIFICATION_KIND_CANON_CHANGE', 'NOTIFICATION_KIND_REPORT_RESOLVED',
    'NOTIFICATION_KIND_INVITE_ANSWERED',
    'NOTIFICATION_KIND_NEW_STORY_AT_FOLLOWED_PLACE',
    'NOTIFICATION_KIND_NEW_STORY_BY_FOLLOWED_WRITER',
    'NOTIFICATION_KIND_THREAD_REPLY'));
ALTER TABLE notification_preferences DROP CONSTRAINT IF EXISTS notification_preferences_kind_check;
ALTER TABLE notification_preferences ADD CONSTRAINT notification_preferences_kind_check CHECK (kind IN (
    'NOTIFICATION_KIND_CLAIM_RECEIVED', 'NOTIFICATION_KIND_CLAIM_ACCEPTED',
    'NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC', 'NOTIFICATION_KIND_VOTE_OUT_OPENED',
    'NOTIFICATION_KIND_CANON_CHANGE', 'NOTIFICATION_KIND_REPORT_RESOLVED',
    'NOTIFICATION_KIND_INVITE_ANSWERED',
    'NOTIFICATION_KIND_NEW_STORY_AT_FOLLOWED_PLACE',
    'NOTIFICATION_KIND_NEW_STORY_BY_FOLLOWED_WRITER',
    'NOTIFICATION_KIND_THREAD_REPLY'));

-- +goose Down
DELETE FROM notifications WHERE kind = 'NOTIFICATION_KIND_THREAD_REPLY';
DELETE FROM notification_preferences WHERE kind = 'NOTIFICATION_KIND_THREAD_REPLY';
-- A queued summary line of the dropped kind would fail the older email worker.
DELETE FROM digest_items WHERE data->>'kind' = 'NOTIFICATION_KIND_THREAD_REPLY';
ALTER TABLE notification_preferences DROP CONSTRAINT IF EXISTS notification_preferences_kind_check;
ALTER TABLE notification_preferences ADD CONSTRAINT notification_preferences_kind_check CHECK (kind IN (
    'NOTIFICATION_KIND_CLAIM_RECEIVED', 'NOTIFICATION_KIND_CLAIM_ACCEPTED',
    'NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC', 'NOTIFICATION_KIND_VOTE_OUT_OPENED',
    'NOTIFICATION_KIND_CANON_CHANGE', 'NOTIFICATION_KIND_REPORT_RESOLVED',
    'NOTIFICATION_KIND_INVITE_ANSWERED',
    'NOTIFICATION_KIND_NEW_STORY_AT_FOLLOWED_PLACE',
    'NOTIFICATION_KIND_NEW_STORY_BY_FOLLOWED_WRITER'));
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_kind_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_kind_check CHECK (kind IN (
    'NOTIFICATION_KIND_CLAIM_RECEIVED', 'NOTIFICATION_KIND_CLAIM_ACCEPTED',
    'NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC', 'NOTIFICATION_KIND_VOTE_OUT_OPENED',
    'NOTIFICATION_KIND_CANON_CHANGE', 'NOTIFICATION_KIND_REPORT_RESOLVED',
    'NOTIFICATION_KIND_INVITE_ANSWERED',
    'NOTIFICATION_KIND_NEW_STORY_AT_FOLLOWED_PLACE',
    'NOTIFICATION_KIND_NEW_STORY_BY_FOLLOWED_WRITER'));
DROP VIEW IF EXISTS lore_thread_posts;
DROP TABLE IF EXISTS lore_post_votes;
DROP INDEX IF EXISTS lore_posts_replies_idx;
DROP INDEX IF EXISTS lore_posts_new_idx;
DROP INDEX IF EXISTS lore_posts_top_idx;
CREATE INDEX IF NOT EXISTS lore_posts_lore_created_idx ON lore_posts (lore_id, created_at, id);
-- Replies become flat posts again; the thread was flat before this
-- migration, so nothing reads the dropped columns.
ALTER TABLE lore_posts DROP CONSTRAINT IF EXISTS lore_posts_upvotes_check;
ALTER TABLE lore_posts DROP CONSTRAINT IF EXISTS lore_posts_reply_check;
ALTER TABLE lore_posts
    DROP COLUMN IF EXISTS upvotes,
    DROP COLUMN IF EXISTS edited_at,
    DROP COLUMN IF EXISTS answered_id,
    DROP COLUMN IF EXISTS reply_to_id;
