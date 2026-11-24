-- +goose Up
-- Notification channels: a notification is always in the
-- in-app feed ("database") and can also be delivered by mail, SMS or push
-- through the delivery system. Users opt out per event type and channel;
-- a missing row means "deliver".
ALTER TABLE notifications
    ADD COLUMN channels text[] NOT NULL DEFAULT '{database}';

CREATE TABLE notification_preferences (
    user_id    bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    event_type text NOT NULL,
    channel    text NOT NULL,
    enabled    boolean NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, event_type, channel)
);

-- +goose Down
DROP TABLE IF EXISTS notification_preferences;
ALTER TABLE notifications DROP COLUMN IF EXISTS channels;
