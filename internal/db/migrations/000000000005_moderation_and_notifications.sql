-- Reports, the moderation audit trail, the inbox and notification
-- preferences.

-- +goose Up
CREATE TABLE IF NOT EXISTS reports (
    id              uuid        PRIMARY KEY DEFAULT uuidv7(),
    reporter_id     uuid        NOT NULL REFERENCES users (id),
    target_kind     text        NOT NULL
                                CONSTRAINT reports_target_kind_check CHECK (target_kind IN (
                                    'REPORT_TARGET_KIND_PERSPECTIVE', 'REPORT_TARGET_KIND_PLACE',
                                    'REPORT_TARGET_KIND_USER')),
    -- A perspectives, places or users id, by target_kind.
    target_id       uuid        NOT NULL,
    reason          text        NOT NULL
                                CONSTRAINT reports_reason_check CHECK (reason IN (
                                    'REPORT_REASON_REAL_NAME_OR_HANDLE_OF_NON_USER', 'REPORT_REASON_HARASSMENT',
                                    'REPORT_REASON_HATEFUL_CONTENT', 'REPORT_REASON_SEXUAL_CONTENT',
                                    'REPORT_REASON_SPAM', 'REPORT_REASON_OTHER')),
    details         text        NOT NULL DEFAULT '',
    status          text        NOT NULL DEFAULT 'REPORT_STATUS_OPEN'
                                CONSTRAINT reports_status_check CHECK (status IN (
                                    'REPORT_STATUS_OPEN', 'REPORT_STATUS_RESOLVED')),
    resolved_by     uuid        NULL REFERENCES users (id),
    resolution      text        NULL
                                CONSTRAINT reports_resolution_check CHECK (resolution IN (
                                    'MODERATION_ACTION_HIDE_PERSPECTIVE', 'MODERATION_ACTION_HIDE_PLACE',
                                    'MODERATION_ACTION_SUSPEND_USER', 'MODERATION_ACTION_DISMISS')),
    resolution_reason text      NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    resolved_at     timestamptz NULL,
    -- A resolved report records who resolved it, how and when.
    CONSTRAINT reports_resolved_check CHECK (
        (status = 'REPORT_STATUS_RESOLVED') = (resolved_by IS NOT NULL AND resolution IS NOT NULL AND resolved_at IS NOT NULL))
);

-- The admin queue, oldest first within a status.
CREATE INDEX IF NOT EXISTS reports_status_created_idx ON reports (status, created_at, id);
CREATE INDEX IF NOT EXISTS reports_target_idx ON reports (target_kind, target_id);
CREATE INDEX IF NOT EXISTS reports_reporter_id_idx ON reports (reporter_id);

-- Every moderation action and admin grant, append-only. actor_id is NULL for
-- an action taken from the command line rather than by a signed-in admin.
-- action is a ModerationAction value name or another action name the
-- moderation code defines (for example an admin grant).
CREATE TABLE IF NOT EXISTS moderation_audit (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_id    uuid        NULL REFERENCES users (id),
    action      text        NOT NULL CONSTRAINT moderation_audit_action_check CHECK (action <> ''),
    target_kind text        NOT NULL
                            CONSTRAINT moderation_audit_target_kind_check CHECK (target_kind IN (
                                'REPORT_TARGET_KIND_PERSPECTIVE', 'REPORT_TARGET_KIND_PLACE',
                                'REPORT_TARGET_KIND_USER')),
    target_id   uuid        NOT NULL,
    report_id   uuid        NULL REFERENCES reports (id),
    reason      text        NOT NULL DEFAULT '',
    at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS moderation_audit_target_idx ON moderation_audit (target_kind, target_id, at DESC);

CREATE OR REPLACE TRIGGER moderation_audit_append_only
    BEFORE UPDATE OR DELETE ON moderation_audit
    FOR EACH ROW EXECUTE FUNCTION reject_audit_change();

-- An inbox entry. payload follows this JSON Schema, which the CHECK below
-- enforces at write time:
--   {
--     "type": "object",
--     "additionalProperties": false,
--     "properties": {
--       "actor_id":       {"type": "string", "format": "uuid"},
--       "arc_id":         {"type": "string", "format": "uuid"},
--       "perspective_id": {"type": "string", "format": "uuid"},
--       "claim_id":       {"type": "string", "format": "uuid"},
--       "lore_id":        {"type": "string", "format": "uuid"},
--       "report_id":      {"type": "string", "format": "uuid"}
--     }
--   }
-- A key is present only when it applies to the notification's kind.
CREATE TABLE IF NOT EXISTS notifications (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    user_id    uuid        NOT NULL REFERENCES users (id),
    kind       text        NOT NULL
                           CONSTRAINT notifications_kind_check CHECK (kind IN (
                               'NOTIFICATION_KIND_CLAIM_RECEIVED', 'NOTIFICATION_KIND_CLAIM_ACCEPTED',
                               'NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC', 'NOTIFICATION_KIND_VOTE_OUT_OPENED',
                               'NOTIFICATION_KIND_CANON_CHANGE', 'NOTIFICATION_KIND_REPORT_RESOLVED')),
    payload    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    read_at    timestamptz NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT notifications_payload_check CHECK (
        jsonb_typeof(payload) = 'object'
        AND payload - ARRAY['actor_id', 'arc_id', 'perspective_id', 'claim_id', 'lore_id', 'report_id'] = '{}'::jsonb
        AND NOT jsonb_path_exists(payload,
            '$.* ? (@.type() != "string" || !(@ like_regex "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"))'))
);

-- The inbox, newest first, with keyset pagination on (created_at, id).
CREATE INDEX IF NOT EXISTS notifications_inbox_idx ON notifications (user_id, created_at DESC, id DESC);
-- The unread count.
CREATE INDEX IF NOT EXISTS notifications_unread_idx ON notifications (user_id) WHERE read_at IS NULL;
-- Lookups by referenced id, for example every notification about one arc.
CREATE INDEX IF NOT EXISTS notifications_payload_idx ON notifications USING gin (payload jsonb_path_ops);

-- Per-kind toggles. A missing row means both toggles are on.
CREATE TABLE IF NOT EXISTS notification_preferences (
    user_id uuid    NOT NULL REFERENCES users (id),
    kind    text    NOT NULL
                    CONSTRAINT notification_preferences_kind_check CHECK (kind IN (
                        'NOTIFICATION_KIND_CLAIM_RECEIVED', 'NOTIFICATION_KIND_CLAIM_ACCEPTED',
                        'NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC', 'NOTIFICATION_KIND_VOTE_OUT_OPENED',
                        'NOTIFICATION_KIND_CANON_CHANGE', 'NOTIFICATION_KIND_REPORT_RESOLVED')),
    in_app  boolean NOT NULL DEFAULT true,
    email   boolean NOT NULL DEFAULT true,
    PRIMARY KEY (user_id, kind)
);

-- +goose Down
DROP TABLE IF EXISTS notification_preferences;
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS moderation_audit;
DROP TABLE IF EXISTS reports;
