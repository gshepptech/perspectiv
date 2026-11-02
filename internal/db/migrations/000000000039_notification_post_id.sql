-- +goose Up
-- A THREAD_REPLY notification names the reply (post_id), so the inbox opens
-- the lore at that post rather than at the top of the page. The payload
-- check gains the key; every value is still a uuid string.
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_payload_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_payload_check CHECK (
    jsonb_typeof(payload) = 'object'
    AND payload - ARRAY['actor_id', 'arc_id', 'perspective_id', 'claim_id', 'lore_id', 'report_id', 'post_id'] = '{}'::jsonb
    AND NOT jsonb_path_exists(payload,
        '$.* ? (@.type() != "string" || !(@ like_regex "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"))'));

-- +goose Down
-- The older check rejects the key, so it leaves every payload first; those
-- notifications then open the lore at its top, as they did before.
UPDATE notifications SET payload = payload - 'post_id' WHERE payload ? 'post_id';
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_payload_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_payload_check CHECK (
    jsonb_typeof(payload) = 'object'
    AND payload - ARRAY['actor_id', 'arc_id', 'perspective_id', 'claim_id', 'lore_id', 'report_id'] = '{}'::jsonb
    AND NOT jsonb_path_exists(payload,
        '$.* ? (@.type() != "string" || !(@ like_regex "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"))'));
