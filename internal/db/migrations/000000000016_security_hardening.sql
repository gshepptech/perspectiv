-- +goose Up
-- A member's invite link admits a bounded number of sign-ups while sign-up
-- is invite-only; signups counts them.
ALTER TABLE invites ADD COLUMN IF NOT EXISTS signups integer NOT NULL DEFAULT 0
    CONSTRAINT invites_signups_check CHECK (signups >= 0);
-- When a moderator last cleared a lore's name; naming is locked for a while
-- after, so a cleared name cannot be put straight back.
ALTER TABLE lores ADD COLUMN IF NOT EXISTS name_cleared_at timestamptz NULL;
-- Description search matches anywhere in a label (ILIKE '%...%'); a trigram
-- index serves it.
CREATE INDEX IF NOT EXISTS characters_label_trgm_idx ON characters USING gin (label gin_trgm_ops);

-- +goose Down
DROP INDEX IF EXISTS characters_label_trgm_idx;
ALTER TABLE lores DROP COLUMN IF EXISTS name_cleared_at;
ALTER TABLE invites DROP COLUMN IF EXISTS signups;
