-- Up-votes, remove votes and emoji reacts. There is no downvote: a votes row
-- is an up-vote and clearing it deletes the row.

-- +goose Up
CREATE TABLE IF NOT EXISTS votes (
    perspective_id uuid        NOT NULL REFERENCES perspectives (id),
    user_id        uuid        NOT NULL REFERENCES users (id),
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (perspective_id, user_id)
);

CREATE INDEX IF NOT EXISTS votes_user_id_idx ON votes (user_id);

-- A vote to remove a perspective from its lore, cast by a member of that lore.
CREATE TABLE IF NOT EXISTS remove_votes (
    perspective_id uuid        NOT NULL REFERENCES perspectives (id),
    user_id        uuid        NOT NULL REFERENCES users (id),
    lore_id        uuid        NOT NULL REFERENCES lores (id),
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (perspective_id, user_id)
);

CREATE INDEX IF NOT EXISTS remove_votes_lore_id_idx ON remove_votes (lore_id);

-- A user holds at most one of each emoji on a perspective.
CREATE TABLE IF NOT EXISTS reacts (
    perspective_id uuid        NOT NULL REFERENCES perspectives (id),
    user_id        uuid        NOT NULL REFERENCES users (id),
    emoji          text        NOT NULL
                               CONSTRAINT reacts_emoji_check CHECK (emoji IN (
                                   'EMOJI_HEART', 'EMOJI_LAUGH', 'EMOJI_WOW',
                                   'EMOJI_SAD', 'EMOJI_FIRE', 'EMOJI_CLAP')),
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (perspective_id, user_id, emoji)
);

CREATE INDEX IF NOT EXISTS reacts_user_id_idx ON reacts (user_id);

-- Vote totals feed lore rank scores, so adding or clearing a vote marks the
-- perspective's arc for the rank job. A trigger, rather than the vote code,
-- guarantees no vote write can skip the mark. clock_timestamp() keeps the
-- mark later than any rank pass that started before this write.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION mark_arc_lore_dirty_on_vote() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    target uuid;
BEGIN
    IF TG_OP = 'DELETE' THEN
        target := OLD.perspective_id;
    ELSE
        target := NEW.perspective_id;
    END IF;
    UPDATE arcs SET lore_dirty_at = clock_timestamp()
    WHERE id = (SELECT arc_id FROM perspectives WHERE id = target);
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE OR REPLACE TRIGGER votes_mark_arc_lore_dirty
    AFTER INSERT OR DELETE ON votes
    FOR EACH ROW EXECUTE FUNCTION mark_arc_lore_dirty_on_vote();

-- +goose Down
DROP TABLE IF EXISTS reacts;
DROP TABLE IF EXISTS remove_votes;
DROP TABLE IF EXISTS votes;
DROP FUNCTION IF EXISTS mark_arc_lore_dirty_on_vote();
