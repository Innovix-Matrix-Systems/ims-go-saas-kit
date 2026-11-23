-- +goose Up
-- Renamed the file resource from "upload" to "file".
-- Retire the legacy permission rows from earlier phases; role assignments
-- cascade away. Fresh databases are unaffected.
DELETE FROM permissions
WHERE name IN ('upload.view', 'upload.view.all', 'upload.create', 'upload.delete');

-- +goose Down
-- Legacy permission rows are not restored (they are superseded by file.*).
SELECT 1;
