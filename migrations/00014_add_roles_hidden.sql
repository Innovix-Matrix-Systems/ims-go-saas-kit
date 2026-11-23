-- +goose Up
-- Hidden roles (e.g. the seeded Developer role) are never returned by role
-- listings; they remain assignable by system code.
ALTER TABLE roles ADD COLUMN hidden boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE roles DROP COLUMN hidden;
