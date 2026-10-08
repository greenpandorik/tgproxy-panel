-- +goose Up
CREATE TABLE notification_states (
 node_id uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 kind text NOT NULL,
 state jsonb NOT NULL DEFAULT '{}',
 PRIMARY KEY(node_id,kind)
);
CREATE TABLE notification_findings (
 node_id uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 source text NOT NULL,
 kind text NOT NULL,
 failed boolean NOT NULL,
 known boolean NOT NULL,
 observed_at timestamptz NOT NULL,
 PRIMARY KEY(node_id,kind)
);

CREATE TABLE notification_candidates (
 node_id uuid PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
 phase text NOT NULL,
 since timestamptz NOT NULL,
 observed_at timestamptz NOT NULL
);

-- +goose Down
DROP TABLE notification_candidates;
DROP TABLE notification_findings;
DROP TABLE notification_states;
