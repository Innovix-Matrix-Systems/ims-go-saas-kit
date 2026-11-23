-- +goose Up
-- Settings can be ordered by hand inside their group (dev dashboard drag and
-- drop). Stored on the global row next to group_id; NULL = catalog order.
ALTER TABLE settings ADD COLUMN sort_order integer;

-- +goose Down
ALTER TABLE settings DROP COLUMN IF EXISTS sort_order;
