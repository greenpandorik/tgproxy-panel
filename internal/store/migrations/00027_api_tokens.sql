-- +goose Up
CREATE TABLE api_tokens (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 admin_user_id uuid NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
 name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
 prefix text NOT NULL,
 token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
 scopes text[] NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 last_used_at timestamptz,
 revoked_at timestamptz
);
CREATE INDEX api_tokens_owner_created_idx ON api_tokens (admin_user_id, created_at DESC);
-- +goose Down
DROP TABLE api_tokens;
