-- +goose Up
ALTER TABLE nodes ADD COLUMN tls_domains text[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE nodes DROP COLUMN tls_domains;
