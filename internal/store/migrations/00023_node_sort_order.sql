-- +goose Up
ALTER TABLE nodes ADD COLUMN sort_order integer NOT NULL DEFAULT 0;
UPDATE nodes n SET sort_order = o.pos
FROM (SELECT id, row_number() OVER (ORDER BY created_at, id) AS pos FROM nodes) o
WHERE n.id = o.id;

-- +goose Down
ALTER TABLE nodes DROP COLUMN sort_order;
