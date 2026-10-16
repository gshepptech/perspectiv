-- The place hierarchy as an adjacency list: STATE > CITY > TOWN > MALL >
-- STORE. Ancestors and descendants are read with recursive CTEs over
-- parent_id. lat and lng are stored from the import and unused in v1.

-- +goose Up
-- pg_trgm is a trusted extension, so the database owner may create it.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS places (
    id         uuid             PRIMARY KEY DEFAULT uuidv7(),
    kind       text             NOT NULL
                                CONSTRAINT places_kind_check CHECK (kind IN (
                                    'PLACE_KIND_STATE', 'PLACE_KIND_CITY', 'PLACE_KIND_TOWN',
                                    'PLACE_KIND_MALL', 'PLACE_KIND_STORE')),
    name       text             NOT NULL CONSTRAINT places_name_check CHECK (btrim(name) <> ''),
    parent_id  uuid             NULL REFERENCES places (id),
    -- IANA time zone id, resolved once at import or inherited from the parent.
    timezone   text             NOT NULL CONSTRAINT places_timezone_check CHECK (timezone <> ''),
    lat        double precision NULL,
    lng        double precision NULL,
    source     text             NOT NULL
                                CONSTRAINT places_source_check CHECK (source IN (
                                    'PLACE_SOURCE_OVERTURE', 'PLACE_SOURCE_FOURSQUARE', 'PLACE_SOURCE_USER')),
    -- The row's id in its source dataset; NULL for user-added places.
    source_id  text             NULL,
    user_added boolean          NOT NULL DEFAULT false,
    hidden_at  timestamptz      NULL,
    created_by uuid             NULL REFERENCES users (id),
    created_at timestamptz      NOT NULL DEFAULT now(),
    -- Only a STATE is a root.
    CONSTRAINT places_root_is_state_check CHECK ((parent_id IS NULL) = (kind = 'PLACE_KIND_STATE'))
);

-- One name per parent, compared case-insensitively with whitespace runs
-- collapsed. NULLS NOT DISTINCT makes two states with one name collide too.
CREATE UNIQUE INDEX IF NOT EXISTS places_parent_name_key
    ON places (parent_id, lower(regexp_replace(btrim(name), '\s+', ' ', 'g'))) NULLS NOT DISTINCT;

-- Re-imports upsert by source id.
CREATE UNIQUE INDEX IF NOT EXISTS places_source_id_key
    ON places (source, source_id) WHERE source_id IS NOT NULL;

-- Drill-down lists a parent's children by name.
CREATE INDEX IF NOT EXISTS places_parent_id_name_idx ON places (parent_id, lower(name), id);

-- Typeahead search: substring and prefix matches on the lowercased name.
CREATE INDEX IF NOT EXISTS places_name_trgm_idx ON places USING gin (lower(name) gin_trgm_ops);

-- +goose Down
DROP TABLE IF EXISTS places;
DROP EXTENSION IF EXISTS pg_trgm;
