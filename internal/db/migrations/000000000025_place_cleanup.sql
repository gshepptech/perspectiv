-- +goose Up
-- An admin can mark an imported store (a stadium, a theme park) as a venue
-- that holds places, and merge a duplicate place with no stories into its
-- twin; the audit log names both actions.
ALTER TABLE moderation_audit DROP CONSTRAINT IF EXISTS moderation_audit_action_check;
ALTER TABLE moderation_audit ADD CONSTRAINT moderation_audit_action_check CHECK (action IN (
    'AUDIT_ACTION_HIDE_PERSPECTIVE', 'AUDIT_ACTION_HIDE_PLACE', 'AUDIT_ACTION_SUSPEND_USER',
    'AUDIT_ACTION_DISMISS', 'AUDIT_ACTION_HIDE_LORE_POST', 'AUDIT_ACTION_CLEAR_LORE_NAME',
    'AUDIT_ACTION_ADMIN_GRANT', 'AUDIT_ACTION_ACCESS_CODE_CREATE', 'AUDIT_ACTION_ACCESS_CODE_REVOKE',
    'AUDIT_ACTION_WAITLIST_INVITE', 'AUDIT_ACTION_USER_LOOKUP', 'AUDIT_ACTION_LIFT_SUSPENSION', 'AUDIT_ACTION_RESTORE', 'AUDIT_ACTION_MAKE_VENUE', 'AUDIT_ACTION_MERGE_PLACE'));

-- +goose Down
-- Venues stay venues and merged places stay hidden; only the audit rows go
-- with the names.
ALTER TABLE moderation_audit DISABLE TRIGGER moderation_audit_append_only;
DELETE FROM moderation_audit WHERE action IN ('AUDIT_ACTION_MAKE_VENUE', 'AUDIT_ACTION_MERGE_PLACE');
ALTER TABLE moderation_audit ENABLE TRIGGER moderation_audit_append_only;
ALTER TABLE moderation_audit DROP CONSTRAINT IF EXISTS moderation_audit_action_check;
ALTER TABLE moderation_audit ADD CONSTRAINT moderation_audit_action_check CHECK (action IN (
    'AUDIT_ACTION_HIDE_PERSPECTIVE', 'AUDIT_ACTION_HIDE_PLACE', 'AUDIT_ACTION_SUSPEND_USER',
    'AUDIT_ACTION_DISMISS', 'AUDIT_ACTION_HIDE_LORE_POST', 'AUDIT_ACTION_CLEAR_LORE_NAME',
    'AUDIT_ACTION_ADMIN_GRANT', 'AUDIT_ACTION_ACCESS_CODE_CREATE', 'AUDIT_ACTION_ACCESS_CODE_REVOKE',
    'AUDIT_ACTION_WAITLIST_INVITE', 'AUDIT_ACTION_USER_LOOKUP', 'AUDIT_ACTION_LIFT_SUSPENSION', 'AUDIT_ACTION_RESTORE'));
