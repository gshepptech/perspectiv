-- +goose Up
-- The audit log names its own actions and target kinds (AuditAction and
-- AuditTargetKind), so it can record admin actions that are not report
-- resolutions: beta access codes, waitlist invites and user lookups. An
-- access code is keyed by text, so a row names its target by target_id or,
-- for a code, by target_ref, never both.
ALTER TABLE moderation_audit DISABLE TRIGGER moderation_audit_append_only;

ALTER TABLE moderation_audit DROP CONSTRAINT IF EXISTS moderation_audit_target_kind_check;
ALTER TABLE moderation_audit DROP CONSTRAINT IF EXISTS moderation_audit_action_check;
UPDATE moderation_audit SET action = 'AUDIT_ACTION_ADMIN_GRANT' WHERE action = 'ADMIN_GRANT';
UPDATE moderation_audit SET action = replace(action, 'MODERATION_ACTION_', 'AUDIT_ACTION_')
WHERE action LIKE 'MODERATION_ACTION_%';
UPDATE moderation_audit SET target_kind = replace(target_kind, 'REPORT_TARGET_KIND_', 'AUDIT_TARGET_KIND_');

ALTER TABLE moderation_audit ALTER COLUMN target_id DROP NOT NULL;
ALTER TABLE moderation_audit ADD COLUMN IF NOT EXISTS target_ref text NOT NULL DEFAULT '';
ALTER TABLE moderation_audit ADD CONSTRAINT moderation_audit_target_check
    CHECK ((target_id IS NULL) <> (target_ref = ''));
ALTER TABLE moderation_audit ADD CONSTRAINT moderation_audit_action_check CHECK (action IN (
    'AUDIT_ACTION_HIDE_PERSPECTIVE', 'AUDIT_ACTION_HIDE_PLACE', 'AUDIT_ACTION_SUSPEND_USER',
    'AUDIT_ACTION_DISMISS', 'AUDIT_ACTION_HIDE_LORE_POST', 'AUDIT_ACTION_CLEAR_LORE_NAME',
    'AUDIT_ACTION_ADMIN_GRANT', 'AUDIT_ACTION_ACCESS_CODE_CREATE', 'AUDIT_ACTION_ACCESS_CODE_REVOKE',
    'AUDIT_ACTION_WAITLIST_INVITE', 'AUDIT_ACTION_USER_LOOKUP'));
ALTER TABLE moderation_audit ADD CONSTRAINT moderation_audit_target_kind_check CHECK (target_kind IN (
    'AUDIT_TARGET_KIND_PERSPECTIVE', 'AUDIT_TARGET_KIND_PLACE', 'AUDIT_TARGET_KIND_USER',
    'AUDIT_TARGET_KIND_LORE_POST', 'AUDIT_TARGET_KIND_LORE_NAME', 'AUDIT_TARGET_KIND_ACCESS_CODE',
    'AUDIT_TARGET_KIND_WAITLIST_ENTRY'));

ALTER TABLE moderation_audit ENABLE TRIGGER moderation_audit_append_only;

-- The log reads newest first by id, whole (the primary key) or by the admin
-- who acted.
CREATE INDEX IF NOT EXISTS moderation_audit_actor_idx ON moderation_audit (actor_id, id DESC);

-- +goose Down
-- Rows for actions the old schema cannot name go with them.
DROP INDEX IF EXISTS moderation_audit_actor_idx;
ALTER TABLE moderation_audit DISABLE TRIGGER moderation_audit_append_only;
DELETE FROM moderation_audit WHERE action IN (
    'AUDIT_ACTION_ACCESS_CODE_CREATE', 'AUDIT_ACTION_ACCESS_CODE_REVOKE',
    'AUDIT_ACTION_WAITLIST_INVITE', 'AUDIT_ACTION_USER_LOOKUP');
ALTER TABLE moderation_audit DROP CONSTRAINT IF EXISTS moderation_audit_target_kind_check;
ALTER TABLE moderation_audit DROP CONSTRAINT IF EXISTS moderation_audit_action_check;
ALTER TABLE moderation_audit DROP CONSTRAINT IF EXISTS moderation_audit_target_check;
ALTER TABLE moderation_audit DROP COLUMN IF EXISTS target_ref;
ALTER TABLE moderation_audit ALTER COLUMN target_id SET NOT NULL;
UPDATE moderation_audit SET action = 'ADMIN_GRANT' WHERE action = 'AUDIT_ACTION_ADMIN_GRANT';
UPDATE moderation_audit SET action = replace(action, 'AUDIT_ACTION_', 'MODERATION_ACTION_')
WHERE action LIKE 'AUDIT_ACTION_%';
UPDATE moderation_audit SET target_kind = replace(target_kind, 'AUDIT_TARGET_KIND_', 'REPORT_TARGET_KIND_');
ALTER TABLE moderation_audit ADD CONSTRAINT moderation_audit_action_check CHECK (action <> '');
ALTER TABLE moderation_audit ADD CONSTRAINT moderation_audit_target_kind_check CHECK (target_kind IN (
    'REPORT_TARGET_KIND_PERSPECTIVE', 'REPORT_TARGET_KIND_PLACE', 'REPORT_TARGET_KIND_USER',
    'REPORT_TARGET_KIND_LORE_POST', 'REPORT_TARGET_KIND_LORE_NAME'));
ALTER TABLE moderation_audit ENABLE TRIGGER moderation_audit_append_only;
