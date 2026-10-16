-- Arcs, perspectives with their parts and characters, claims, lores and the
-- canon audit trail.
--
-- Write ownership (enforced in code, recorded here for readers):
--   lores.status, lores.rank_score, lores.upvotes, arcs.canon_lore_id,
--   arcs.canon_score, arcs.member_count and canon_changes are written only by
--   the rank job. arcs.lore_dirty_at is set by every write that changes lore
--   inputs and cleared only by the rank job.

-- +goose Up
-- An arc is one place and one place-local day (hour NULL), or one hour child
-- of that day arc (hour 0 to 23, parent_arc_id = the day arc).
CREATE TABLE IF NOT EXISTS arcs (
    id            uuid             PRIMARY KEY DEFAULT uuidv7(),
    place_id      uuid             NOT NULL REFERENCES places (id),
    local_date    date             NOT NULL,
    hour          smallint         NULL CONSTRAINT arcs_hour_check CHECK (hour BETWEEN 0 AND 23),
    parent_arc_id uuid             NULL REFERENCES arcs (id),
    title         text             NOT NULL CONSTRAINT arcs_title_check CHECK (char_length(title) BETWEEN 3 AND 80),
    created_by    uuid             NOT NULL REFERENCES users (id),
    -- The current canon lore; the foreign key is added below, after lores.
    canon_lore_id uuid             NULL,
    canon_score   double precision NOT NULL DEFAULT 0,
    member_count  integer          NOT NULL DEFAULT 0,
    lore_dirty_at timestamptz      NULL,
    created_at    timestamptz      NOT NULL DEFAULT now(),
    -- A day arc has neither an hour nor a parent; an hour arc has both.
    CONSTRAINT arcs_hour_parent_check CHECK ((hour IS NULL) = (parent_arc_id IS NULL))
);

-- Exactly one arc per place, day and hour. NULLS NOT DISTINCT makes two day
-- arcs (hour NULL) for one place and day collide, so concurrent first
-- writers converge on one row.
CREATE UNIQUE INDEX IF NOT EXISTS arcs_place_date_hour_key
    ON arcs (place_id, local_date, hour) NULLS NOT DISTINCT;

-- Epic RANKED: arcs with a canon first, then canon score, member count, newest.
CREATE INDEX IF NOT EXISTS arcs_epic_ranked_idx
    ON arcs (place_id, (canon_lore_id IS NOT NULL) DESC, canon_score DESC, member_count DESC, created_at DESC, id DESC);
-- Epic NEWEST.
CREATE INDEX IF NOT EXISTS arcs_epic_newest_idx ON arcs (place_id, created_at DESC, id DESC);
-- A day arc's hour children in hour order.
CREATE INDEX IF NOT EXISTS arcs_parent_arc_id_hour_idx ON arcs (parent_arc_id, hour) WHERE parent_arc_id IS NOT NULL;
-- The rank job's work queue.
CREATE INDEX IF NOT EXISTS arcs_lore_dirty_at_idx ON arcs (lore_dirty_at) WHERE lore_dirty_at IS NOT NULL;

-- A perspective is one user's entry in one arc. A draft has no arc yet; it
-- records the place, day, optional hour and title it will publish under.
CREATE TABLE IF NOT EXISTS perspectives (
    id               uuid        PRIMARY KEY DEFAULT uuidv7(),
    arc_id           uuid        NULL REFERENCES arcs (id),
    author_id        uuid        NOT NULL REFERENCES users (id),
    place_id         uuid        NULL REFERENCES places (id),
    local_date       date        NULL,
    hour             smallint    NULL CONSTRAINT perspectives_hour_check CHECK (hour BETWEEN 0 AND 23),
    title_if_new_arc text        NULL,
    genre            text        NULL
                                 CONSTRAINT perspectives_genre_check CHECK (genre IN (
                                     'GENRE_DRAMA', 'GENRE_COMEDY', 'GENRE_ROMANCE', 'GENRE_MYSTERY',
                                     'GENRE_HORROR', 'GENRE_SLICE_OF_LIFE', 'GENRE_ACTION')),
    status           text        NOT NULL DEFAULT 'PERSPECTIVE_STATUS_DRAFT'
                                 CONSTRAINT perspectives_status_check CHECK (status IN (
                                     'PERSPECTIVE_STATUS_DRAFT', 'PERSPECTIVE_STATUS_PUBLISHED')),
    lore_status      text        NULL
                                 CONSTRAINT perspectives_lore_status_check CHECK (lore_status IN (
                                     'LORE_STATUS_PENDING', 'LORE_STATUS_IN_LORE',
                                     'LORE_STATUS_SOLO', 'LORE_STATUS_REMOVED')),
    -- The lore this perspective belongs to while IN_LORE; the foreign key is
    -- added below, after lores.
    lore_id          uuid        NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    published_at     timestamptz NULL,
    updated_at       timestamptz NOT NULL DEFAULT now(),
    deleted_at       timestamptz NULL,
    hidden_at        timestamptz NULL,
    -- A draft is outside every arc and every lore.
    CONSTRAINT perspectives_draft_check CHECK (
        status <> 'PERSPECTIVE_STATUS_DRAFT' OR (arc_id IS NULL AND lore_status IS NULL AND published_at IS NULL)),
    -- A published perspective is in an arc with one genre and a lore status.
    CONSTRAINT perspectives_published_check CHECK (
        status <> 'PERSPECTIVE_STATUS_PUBLISHED'
        OR (arc_id IS NOT NULL AND genre IS NOT NULL AND lore_status IS NOT NULL AND published_at IS NOT NULL))
);

-- One perspective per user per arc. Drafts (arc_id NULL) do not collide.
CREATE UNIQUE INDEX IF NOT EXISTS perspectives_arc_author_key ON perspectives (arc_id, author_id);
-- The author's own perspectives, newest edit first.
CREATE INDEX IF NOT EXISTS perspectives_author_updated_idx ON perspectives (author_id, updated_at DESC, id DESC);
-- The arc page: an arc's perspectives by lore status.
CREATE INDEX IF NOT EXISTS perspectives_arc_lore_status_idx
    ON perspectives (arc_id, lore_status, published_at DESC) WHERE arc_id IS NOT NULL;
-- Epic TALES: SOLO and REMOVED perspectives, newest first.
CREATE INDEX IF NOT EXISTS perspectives_tales_idx
    ON perspectives (arc_id, published_at DESC, id DESC)
    WHERE lore_status IN ('LORE_STATUS_SOLO', 'LORE_STATUS_REMOVED');
CREATE INDEX IF NOT EXISTS perspectives_lore_id_idx ON perspectives (lore_id) WHERE lore_id IS NOT NULL;

-- A perspective's pages. The word limit is checked at write time in code.
CREATE TABLE IF NOT EXISTS perspective_parts (
    perspective_id uuid     NOT NULL REFERENCES perspectives (id) ON DELETE CASCADE,
    position       smallint NOT NULL CONSTRAINT perspective_parts_position_check CHECK (position BETWEEN 1 AND 10),
    body           text     NOT NULL,
    PRIMARY KEY (perspective_id, position)
);

-- People described in a perspective. Position is unique per perspective but
-- checked at commit, so an edit can reorder characters in place.
CREATE TABLE IF NOT EXISTS characters (
    id             uuid        PRIMARY KEY DEFAULT uuidv7(),
    perspective_id uuid        NOT NULL REFERENCES perspectives (id) ON DELETE CASCADE,
    label          text        NOT NULL CONSTRAINT characters_label_check CHECK (btrim(label) <> ''),
    position       smallint    NOT NULL CONSTRAINT characters_position_check CHECK (position BETWEEN 1 AND 10),
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT characters_perspective_position_key UNIQUE (perspective_id, position) DEFERRABLE INITIALLY DEFERRED
);

-- A claim: claimant_user_id says the character was them, from
-- claimant_perspective_id in the same arc. Deleting a character drops its
-- claims; code refuses to delete one with an ACCEPTED claim.
CREATE TABLE IF NOT EXISTS claims (
    id                      uuid        PRIMARY KEY DEFAULT uuidv7(),
    character_id            uuid        NOT NULL REFERENCES characters (id) ON DELETE CASCADE,
    claimant_user_id        uuid        NOT NULL REFERENCES users (id),
    claimant_perspective_id uuid        NOT NULL REFERENCES perspectives (id),
    status                  text        NOT NULL DEFAULT 'CLAIM_STATUS_PENDING'
                                        CONSTRAINT claims_status_check CHECK (status IN (
                                            'CLAIM_STATUS_PENDING', 'CLAIM_STATUS_ACCEPTED', 'CLAIM_STATUS_REJECTED')),
    created_at              timestamptz NOT NULL DEFAULT now(),
    resolved_at             timestamptz NULL,
    -- A claimant claims a character once; after a rejection they cannot again.
    CONSTRAINT claims_character_claimant_key UNIQUE (character_id, claimant_user_id)
);

-- A character holds at most one PENDING or ACCEPTED claim.
CREATE UNIQUE INDEX IF NOT EXISTS claims_active_character_key
    ON claims (character_id) WHERE status IN ('CLAIM_STATUS_PENDING', 'CLAIM_STATUS_ACCEPTED');
CREATE INDEX IF NOT EXISTS claims_claimant_user_created_idx ON claims (claimant_user_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS claims_claimant_perspective_idx ON claims (claimant_perspective_id);

-- A lore is a connected group of perspectives linked by accepted claims in
-- one arc. status is its standing: at most one CANON per arc.
CREATE TABLE IF NOT EXISTS lores (
    id           uuid             PRIMARY KEY DEFAULT uuidv7(),
    arc_id       uuid             NOT NULL REFERENCES arcs (id),
    status       text             NOT NULL DEFAULT 'LORE_STANDING_SUBLORE'
                                  CONSTRAINT lores_status_check CHECK (status IN (
                                      'LORE_STANDING_CANON', 'LORE_STANDING_SUBLORE')),
    member_count integer          NOT NULL DEFAULT 0,
    upvotes      integer          NOT NULL DEFAULT 0,
    rank_score   double precision NOT NULL DEFAULT 0,
    computed_at  timestamptz      NULL,
    created_at   timestamptz      NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS lores_one_canon_per_arc_key
    ON lores (arc_id) WHERE status = 'LORE_STANDING_CANON';
CREATE INDEX IF NOT EXISTS lores_arc_rank_idx ON lores (arc_id, rank_score DESC, id);

-- Membership: a perspective is in at most one lore.
CREATE TABLE IF NOT EXISTS lore_members (
    lore_id        uuid NOT NULL REFERENCES lores (id) ON DELETE CASCADE,
    perspective_id uuid NOT NULL REFERENCES perspectives (id),
    PRIMARY KEY (lore_id, perspective_id),
    CONSTRAINT lore_members_perspective_key UNIQUE (perspective_id)
);

ALTER TABLE arcs DROP CONSTRAINT IF EXISTS arcs_canon_lore_id_fkey;
ALTER TABLE arcs ADD CONSTRAINT arcs_canon_lore_id_fkey FOREIGN KEY (canon_lore_id) REFERENCES lores (id);
ALTER TABLE perspectives DROP CONSTRAINT IF EXISTS perspectives_lore_id_fkey;
ALTER TABLE perspectives ADD CONSTRAINT perspectives_lore_id_fkey FOREIGN KEY (lore_id) REFERENCES lores (id);

-- Audit of every canon change in an arc, append-only.
CREATE TABLE IF NOT EXISTS canon_changes (
    id           bigint           GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    arc_id       uuid             NOT NULL REFERENCES arcs (id),
    from_lore_id uuid             NULL REFERENCES lores (id),
    to_lore_id   uuid             NULL REFERENCES lores (id),
    from_score   double precision NULL,
    to_score     double precision NULL,
    changed_at   timestamptz      NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS canon_changes_arc_changed_idx ON canon_changes (arc_id, changed_at DESC);

-- Audit tables refuse UPDATE and DELETE.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION reject_audit_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME USING ERRCODE = 'insufficient_privilege';
END;
$$;
-- +goose StatementEnd

CREATE OR REPLACE TRIGGER canon_changes_append_only
    BEFORE UPDATE OR DELETE ON canon_changes
    FOR EACH ROW EXECUTE FUNCTION reject_audit_change();

-- +goose Down
DROP TABLE IF EXISTS canon_changes;
DROP FUNCTION IF EXISTS reject_audit_change();
ALTER TABLE IF EXISTS perspectives DROP CONSTRAINT IF EXISTS perspectives_lore_id_fkey;
ALTER TABLE IF EXISTS arcs DROP CONSTRAINT IF EXISTS arcs_canon_lore_id_fkey;
DROP TABLE IF EXISTS lore_members;
DROP TABLE IF EXISTS lores;
DROP TABLE IF EXISTS claims;
DROP TABLE IF EXISTS characters;
DROP TABLE IF EXISTS perspective_parts;
DROP TABLE IF EXISTS perspectives;
DROP TABLE IF EXISTS arcs;
