-- +goose Up
CREATE TABLE key_presence_samples (
  id bigserial PRIMARY KEY,
  access_key_id uuid NOT NULL REFERENCES access_keys(id) ON DELETE CASCADE,
  taken_at timestamptz NOT NULL DEFAULT now(),
  people int NOT NULL,
  connections int NOT NULL,
  nodes jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX key_presence_samples_key_time_idx ON key_presence_samples(access_key_id, taken_at);
CREATE INDEX key_presence_samples_taken_at_idx ON key_presence_samples(taken_at);

-- +goose Down
DROP TABLE key_presence_samples;
