-- +goose Up
-- totp_pending_enc holds the secret of an enrolment that has not been confirmed
-- yet. It is kept apart from totp_secret_enc so that starting a new enrolment can
-- never invalidate the second factor that is currently protecting the account: the
-- pending secret only becomes the live one once the user has proved, with a code,
-- that their authenticator app really has it.
ALTER TABLE admin_users ADD COLUMN totp_pending_enc bytea;

-- +goose Down
ALTER TABLE admin_users DROP COLUMN totp_pending_enc;
