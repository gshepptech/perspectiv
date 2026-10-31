-- +goose Up
-- What a report's target said when it was filed, so a moderator reads the
-- reported words even after the author edits them (a post edited to "ok"
-- before review, then edited back after a dismissal).
ALTER TABLE reports ADD COLUMN IF NOT EXISTS reported_text text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE reports DROP COLUMN IF EXISTS reported_text;
