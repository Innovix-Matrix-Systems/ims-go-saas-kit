-- name: PublishRealtimeEvent :exec
-- Sends a live-update event to every server process listening on
-- ims_realtime (internal/platform/pubsub). Delivered when the surrounding
-- transaction commits; dropped if it rolls back.
SELECT pg_notify('ims_realtime', @payload::text);
