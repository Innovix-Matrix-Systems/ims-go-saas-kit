-- +goose Up
-- A payment's currency is the default currency at the time it was made, set
-- by the application; the column no longer assumes taka.
ALTER TABLE payments ALTER COLUMN currency DROP DEFAULT;

-- +goose Down
ALTER TABLE payments ALTER COLUMN currency SET DEFAULT 'BDT';
