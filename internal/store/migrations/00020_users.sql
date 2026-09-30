-- +goose Up
ALTER TABLE access_keys ADD COLUMN disabled_at timestamptz;
ALTER TABLE access_keys ADD COLUMN expired_at timestamptz;
ALTER TABLE access_keys ADD COLUMN last_seen_at timestamptz;
ALTER TABLE access_keys ADD COLUMN sub_slug text;
CREATE UNIQUE INDEX access_keys_sub_slug_idx ON access_keys(sub_slug) WHERE sub_slug IS NOT NULL;
ALTER TABLE subscription_tokens ADD COLUMN token_enc bytea;
ALTER TABLE subscription_tokens ADD COLUMN id uuid NOT NULL DEFAULT gen_random_uuid();
CREATE UNIQUE INDEX subscription_tokens_id_idx ON subscription_tokens(id);

-- +goose Down
DROP INDEX subscription_tokens_id_idx;
ALTER TABLE subscription_tokens DROP COLUMN id;
ALTER TABLE subscription_tokens DROP COLUMN token_enc;
DROP INDEX access_keys_sub_slug_idx;
ALTER TABLE access_keys DROP COLUMN sub_slug;
ALTER TABLE access_keys DROP COLUMN last_seen_at;
ALTER TABLE access_keys DROP COLUMN expired_at;
ALTER TABLE access_keys DROP COLUMN disabled_at;
