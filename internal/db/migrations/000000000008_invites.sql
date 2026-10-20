-- +goose Up
-- An invite is a link an author shares with people who were there too. It
-- names their perspective and, optionally, one of its characters ("were you
-- the guy in the yellow raincoat?"). One link per perspective and character.
CREATE TABLE IF NOT EXISTS invites (
    code           text        PRIMARY KEY CONSTRAINT invites_code_check CHECK (code ~ '^[a-z0-9]{10}$'),
    perspective_id uuid        NOT NULL REFERENCES perspectives (id),
    inviter_id     uuid        NOT NULL REFERENCES users (id),
    character_id   uuid        NULL REFERENCES characters (id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS invites_perspective_character_key
    ON invites (perspective_id, character_id) NULLS NOT DISTINCT;

-- The invite an author followed to write this perspective, if any.
ALTER TABLE perspectives ADD COLUMN IF NOT EXISTS invite_code text NULL REFERENCES invites (code);

ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_kind_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_kind_check CHECK (kind IN (
    'NOTIFICATION_KIND_CLAIM_RECEIVED', 'NOTIFICATION_KIND_CLAIM_ACCEPTED',
    'NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC', 'NOTIFICATION_KIND_VOTE_OUT_OPENED',
    'NOTIFICATION_KIND_CANON_CHANGE', 'NOTIFICATION_KIND_REPORT_RESOLVED',
    'NOTIFICATION_KIND_INVITE_ANSWERED'));
ALTER TABLE notification_preferences DROP CONSTRAINT IF EXISTS notification_preferences_kind_check;
ALTER TABLE notification_preferences ADD CONSTRAINT notification_preferences_kind_check CHECK (kind IN (
    'NOTIFICATION_KIND_CLAIM_RECEIVED', 'NOTIFICATION_KIND_CLAIM_ACCEPTED',
    'NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC', 'NOTIFICATION_KIND_VOTE_OUT_OPENED',
    'NOTIFICATION_KIND_CANON_CHANGE', 'NOTIFICATION_KIND_REPORT_RESOLVED',
    'NOTIFICATION_KIND_INVITE_ANSWERED'));

-- +goose Down
DELETE FROM notifications WHERE kind = 'NOTIFICATION_KIND_INVITE_ANSWERED';
DELETE FROM notification_preferences WHERE kind = 'NOTIFICATION_KIND_INVITE_ANSWERED';
ALTER TABLE notification_preferences DROP CONSTRAINT IF EXISTS notification_preferences_kind_check;
ALTER TABLE notification_preferences ADD CONSTRAINT notification_preferences_kind_check CHECK (kind IN (
    'NOTIFICATION_KIND_CLAIM_RECEIVED', 'NOTIFICATION_KIND_CLAIM_ACCEPTED',
    'NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC', 'NOTIFICATION_KIND_VOTE_OUT_OPENED',
    'NOTIFICATION_KIND_CANON_CHANGE', 'NOTIFICATION_KIND_REPORT_RESOLVED'));
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_kind_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_kind_check CHECK (kind IN (
    'NOTIFICATION_KIND_CLAIM_RECEIVED', 'NOTIFICATION_KIND_CLAIM_ACCEPTED',
    'NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC', 'NOTIFICATION_KIND_VOTE_OUT_OPENED',
    'NOTIFICATION_KIND_CANON_CHANGE', 'NOTIFICATION_KIND_REPORT_RESOLVED'));
ALTER TABLE perspectives DROP COLUMN IF EXISTS invite_code;
DROP TABLE IF EXISTS invites;
