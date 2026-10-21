-- +goose Up
-- A lore's name, chosen by any of its members ("The Lot 7 tailgate"); the
-- latest name wins and named_by says who set it. Unnamed lores are titled by
-- their people.
ALTER TABLE lores ADD COLUMN IF NOT EXISTS name text NULL
    CONSTRAINT lores_name_check CHECK (char_length(btrim(name)) BETWEEN 1 AND 60);
ALTER TABLE lores ADD COLUMN IF NOT EXISTS named_by uuid NULL REFERENCES users (id);

-- +goose Down
ALTER TABLE lores DROP COLUMN IF EXISTS named_by;
ALTER TABLE lores DROP COLUMN IF EXISTS name;
