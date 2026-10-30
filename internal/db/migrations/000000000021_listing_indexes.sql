-- +goose Up
-- Indexes a query-plan audit at beta scale (225k perspectives, 638k places)
-- called for.
--
-- Hidden places are few; every listing asks "is anything on this path
-- hidden?", which without this reads every place.
CREATE INDEX IF NOT EXISTS places_hidden_idx ON places (id) WHERE hidden_at IS NOT NULL;
-- A place's tales, newest first (EpicTales): 40 ms of sequential scan to 2 ms.
CREATE INDEX IF NOT EXISTS perspectives_tales_recent_idx ON perspectives (published_at DESC, id DESC)
    WHERE lore_status IN ('LORE_STATUS_SOLO', 'LORE_STATUS_REMOVED')
      AND status = 'PERSPECTIVE_STATUS_PUBLISHED' AND deleted_at IS NULL AND hidden_at IS NULL;
-- Accounts due a daily summary (DigestDue), in the order they are taken.
CREATE INDEX IF NOT EXISTS users_digest_due_idx ON users (digest_sent_at NULLS FIRST, id)
    WHERE email_digest AND deleted_at IS NULL;

-- A one- or two-letter place search matches name prefixes; the trigram
-- index cannot serve fewer than three characters.
CREATE INDEX IF NOT EXISTS places_name_prefix_idx ON places (lower(name) text_pattern_ops);

-- +goose Down
DROP INDEX IF EXISTS places_name_prefix_idx;
DROP INDEX IF EXISTS users_digest_due_idx;
DROP INDEX IF EXISTS perspectives_tales_recent_idx;
DROP INDEX IF EXISTS places_hidden_idx;
