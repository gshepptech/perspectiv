-- +goose Up
-- Changing a signed-in account's email: a change_email token carries the new
-- address until its owner confirms it from that inbox.
ALTER TABLE email_tokens ADD COLUMN IF NOT EXISTS new_email text NULL;
ALTER TABLE email_tokens DROP CONSTRAINT IF EXISTS email_tokens_purpose_check;
ALTER TABLE email_tokens ADD CONSTRAINT email_tokens_purpose_check
    CHECK (purpose IN ('verify_email', 'password_reset', 'change_email'));
ALTER TABLE email_tokens ADD CONSTRAINT email_tokens_new_email_check
    CHECK ((purpose = 'change_email') = (new_email IS NOT NULL));

-- +goose Down
DELETE FROM email_tokens WHERE purpose = 'change_email';
ALTER TABLE email_tokens DROP CONSTRAINT IF EXISTS email_tokens_new_email_check;
ALTER TABLE email_tokens DROP CONSTRAINT IF EXISTS email_tokens_purpose_check;
ALTER TABLE email_tokens ADD CONSTRAINT email_tokens_purpose_check
    CHECK (purpose IN ('verify_email', 'password_reset'));
ALTER TABLE email_tokens DROP COLUMN IF EXISTS new_email;
