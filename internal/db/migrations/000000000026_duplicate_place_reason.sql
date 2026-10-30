-- +goose Up
-- A member can report a place as a duplicate of another listing, for an
-- admin to merge.
ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_reason_check;
ALTER TABLE reports ADD CONSTRAINT reports_reason_check CHECK (reason IN (
    'REPORT_REASON_REAL_NAME_OR_HANDLE_OF_NON_USER', 'REPORT_REASON_HARASSMENT',
    'REPORT_REASON_HATEFUL_CONTENT', 'REPORT_REASON_SEXUAL_CONTENT',
    'REPORT_REASON_SPAM', 'REPORT_REASON_OTHER', 'REPORT_REASON_DUPLICATE_PLACE'));

-- +goose Down
-- Duplicate reports go with the reason; the audit rows of resolved ones
-- stay, since they name the action, not the reason.
DELETE FROM reports WHERE reason = 'REPORT_REASON_DUPLICATE_PLACE'
    AND NOT EXISTS (SELECT 1 FROM moderation_audit a WHERE a.report_id = reports.id);
UPDATE reports SET reason = 'REPORT_REASON_OTHER' WHERE reason = 'REPORT_REASON_DUPLICATE_PLACE';
ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_reason_check;
ALTER TABLE reports ADD CONSTRAINT reports_reason_check CHECK (reason IN (
    'REPORT_REASON_REAL_NAME_OR_HANDLE_OF_NON_USER', 'REPORT_REASON_HARASSMENT',
    'REPORT_REASON_HATEFUL_CONTENT', 'REPORT_REASON_SEXUAL_CONTENT',
    'REPORT_REASON_SPAM', 'REPORT_REASON_OTHER'));
