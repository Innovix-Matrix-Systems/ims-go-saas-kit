-- +goose Up
CREATE TABLE files (
    id         bigserial PRIMARY KEY,
    uuid       uuid NOT NULL,
    name       text NOT NULL,
    file_name  text NOT NULL,
    disk       text NOT NULL,
    path       text NOT NULL,
    mime_type  text NOT NULL,
    size       bigint NOT NULL,
    type       text NOT NULL DEFAULT 'other',
    owner_type text,
    owner_id   bigint,
    collection text,
    created_by bigint,
    updated_by bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT files_owner_all_or_none CHECK (
        (owner_type IS NULL AND owner_id IS NULL AND collection IS NULL)
        OR (owner_type IS NOT NULL AND owner_id IS NOT NULL AND collection IS NOT NULL)
    )
);

CREATE UNIQUE INDEX files_uuid_unique ON files (uuid);
CREATE INDEX files_type_idx ON files (type);
CREATE INDEX files_owner_idx ON files (owner_type, owner_id, collection);
CREATE UNIQUE INDEX files_profile_unique ON files (owner_type, owner_id, collection)
    WHERE owner_type = 'user' AND collection = 'profile';

-- +goose Down
DROP TABLE files;
