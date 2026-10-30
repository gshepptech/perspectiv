-- +goose Up
-- Thread posts and lore names can be reported: an admin hides a post or
-- clears a name.
ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_target_kind_check;
ALTER TABLE reports ADD CONSTRAINT reports_target_kind_check CHECK (target_kind IN (
    'REPORT_TARGET_KIND_PERSPECTIVE', 'REPORT_TARGET_KIND_PLACE', 'REPORT_TARGET_KIND_USER', 'REPORT_TARGET_KIND_LORE_POST', 'REPORT_TARGET_KIND_LORE_NAME'));
ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_resolution_check;
ALTER TABLE reports ADD CONSTRAINT reports_resolution_check CHECK (resolution IN (
    'MODERATION_ACTION_HIDE_PERSPECTIVE', 'MODERATION_ACTION_HIDE_PLACE', 'MODERATION_ACTION_SUSPEND_USER', 'MODERATION_ACTION_DISMISS', 'MODERATION_ACTION_HIDE_LORE_POST', 'MODERATION_ACTION_CLEAR_LORE_NAME'));
ALTER TABLE moderation_audit DROP CONSTRAINT IF EXISTS moderation_audit_target_kind_check;
ALTER TABLE moderation_audit ADD CONSTRAINT moderation_audit_target_kind_check CHECK (target_kind IN (
    'REPORT_TARGET_KIND_PERSPECTIVE', 'REPORT_TARGET_KIND_PLACE', 'REPORT_TARGET_KIND_USER', 'REPORT_TARGET_KIND_LORE_POST', 'REPORT_TARGET_KIND_LORE_NAME'));

-- +goose Down
-- Reports and audit rows about thread posts and lore names go with the kinds.
-- The audit table is append-only; a rollback is the one writer allowed past it.
ALTER TABLE moderation_audit DISABLE TRIGGER moderation_audit_append_only;
DELETE FROM moderation_audit WHERE target_kind IN ('REPORT_TARGET_KIND_LORE_POST', 'REPORT_TARGET_KIND_LORE_NAME');
ALTER TABLE moderation_audit ENABLE TRIGGER moderation_audit_append_only;
DELETE FROM reports WHERE target_kind IN ('REPORT_TARGET_KIND_LORE_POST', 'REPORT_TARGET_KIND_LORE_NAME');
ALTER TABLE moderation_audit DROP CONSTRAINT IF EXISTS moderation_audit_target_kind_check;
ALTER TABLE moderation_audit ADD CONSTRAINT moderation_audit_target_kind_check CHECK (target_kind IN ('REPORT_TARGET_KIND_PERSPECTIVE', 'REPORT_TARGET_KIND_PLACE', 'REPORT_TARGET_KIND_USER'));
ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_resolution_check;
ALTER TABLE reports ADD CONSTRAINT reports_resolution_check CHECK (resolution IN ('MODERATION_ACTION_HIDE_PERSPECTIVE', 'MODERATION_ACTION_HIDE_PLACE', 'MODERATION_ACTION_SUSPEND_USER', 'MODERATION_ACTION_DISMISS'));
ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_target_kind_check;
ALTER TABLE reports ADD CONSTRAINT reports_target_kind_check CHECK (target_kind IN ('REPORT_TARGET_KIND_PERSPECTIVE', 'REPORT_TARGET_KIND_PLACE', 'REPORT_TARGET_KIND_USER'));
