-- +goose Up
-- The browser and system a session signed in from, as a short label
-- ("Firefox on macOS"), so the account's device list is recognisable. The
-- User-Agent itself is not kept.
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS device text NOT NULL DEFAULT ''
    CONSTRAINT sessions_device_check CHECK (char_length(device) <= 60);

-- +goose Down
ALTER TABLE sessions DROP COLUMN IF EXISTS device;
