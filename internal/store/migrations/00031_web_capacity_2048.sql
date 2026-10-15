-- +goose Up
ALTER TABLE nodes DROP CONSTRAINT IF EXISTS nodes_telemt_web_capacity_check;
ALTER TABLE nodes ADD CONSTRAINT nodes_telemt_web_capacity_check
  CHECK (telemt_web_capacity IN (128, 256, 512, 1024, 2048));

-- +goose Down
UPDATE nodes SET telemt_web_capacity = 1024 WHERE telemt_web_capacity = 2048;
ALTER TABLE nodes DROP CONSTRAINT IF EXISTS nodes_telemt_web_capacity_check;
ALTER TABLE nodes ADD CONSTRAINT nodes_telemt_web_capacity_check
  CHECK (telemt_web_capacity IN (128, 256, 512, 1024));
