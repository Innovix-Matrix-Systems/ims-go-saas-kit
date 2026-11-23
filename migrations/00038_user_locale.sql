-- +goose Up
-- The language a user chose (e.g. "en", "bn"): the client app follows it on
-- every device. NULL means "use the browser's or the application's default".
ALTER TABLE users ADD COLUMN locale text;

-- +goose Down
ALTER TABLE users DROP COLUMN locale;
