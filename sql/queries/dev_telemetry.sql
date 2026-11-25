-- name: InsertDevRequest :one
INSERT INTO dev_requests (request_id, method, path, route, status, duration_ms, request_bytes,
                          response_bytes, user_id, impersonator_id, tenant_id, branch_id, ip, user_agent,
                          headers, request_body, response_body, created_at)
VALUES (@request_id, @method, @path, @route, @status, @duration_ms, @request_bytes,
        @response_bytes, @user_id, @impersonator_id, @tenant_id, @branch_id, @ip, @user_agent,
        @headers, @request_body, @response_body, @created_at)
RETURNING id;

-- name: InsertDevQuery :exec
INSERT INTO dev_queries (request_id, sql, args, duration_ms, row_count, error, created_at)
VALUES (@request_id, @sql, @args, @duration_ms, @row_count, @error, @created_at);

-- name: InsertDevCacheEvent :exec
INSERT INTO dev_cache_events (request_id, operation, key, hit, duration_ms, created_at)
VALUES (@request_id, @operation, @key, @hit, @duration_ms, @created_at);

-- name: InsertDevQueueEvent :exec
INSERT INTO dev_queue_events (request_id, event, queue, kind, job_id, duration_ms, error, created_at)
VALUES (@request_id, @event, @queue, @kind, @job_id, @duration_ms, @error, @created_at);

-- name: InsertDevLog :exec
INSERT INTO dev_logs (request_id, level, message, attributes, created_at)
VALUES (@request_id, @level, @message, @attributes, @created_at);

-- name: InsertDevException :exec
INSERT INTO dev_exceptions (request_id, class, message, stack, created_at)
VALUES (@request_id, @class, @message, @stack, @created_at);

-- name: GetDevRequestByID :one
SELECT * FROM dev_requests WHERE id = @id;

-- name: GetDevLogByID :one
SELECT * FROM dev_logs WHERE id = @id;

-- name: GetDevExceptionByID :one
SELECT * FROM dev_exceptions WHERE id = @id;

-- name: ListDevRequests :many
SELECT * FROM dev_requests
WHERE (@before_id::bigint = 0 OR id < @before_id::bigint)
  AND (@method::text = '' OR method = @method::text)
  AND (@status::integer = 0 OR status = @status::integer)
  AND (@search::text = '' OR path ILIKE '%' || @search::text || '%')
  AND (@tenant_id::bigint = 0 OR tenant_id = @tenant_id::bigint)
  AND (@user_id::bigint = 0 OR user_id = @user_id::bigint)
  AND (@request_id::text = '' OR request_id = @request_id::text)
ORDER BY id DESC
LIMIT @row_limit;

-- name: ListDevQueriesByRequestID :many
SELECT * FROM dev_queries WHERE request_id = @request_id ORDER BY id;

-- name: ListDevCacheEventsByRequestID :many
SELECT * FROM dev_cache_events WHERE request_id = @request_id ORDER BY id;

-- name: ListDevQueueEventsByRequestID :many
SELECT * FROM dev_queue_events WHERE request_id = @request_id ORDER BY id;

-- name: ListDevLogsByRequestID :many
SELECT * FROM dev_logs WHERE request_id = @request_id ORDER BY id;

-- name: ListDevExceptionsByRequestID :many
SELECT * FROM dev_exceptions WHERE request_id = @request_id ORDER BY id;

-- name: ListDevLogs :many
SELECT * FROM dev_logs
WHERE (@before_id::bigint = 0 OR id < @before_id::bigint)
  AND (@level::text = '' OR level = @level::text)
  AND (@request_id::text = '' OR request_id = @request_id::text)
  AND (@search::text = '' OR message ILIKE '%' || @search::text || '%'
       OR coalesce(attributes::text, '') ILIKE '%' || @search::text || '%'
       OR coalesce(request_id, '') ILIKE @search::text || '%')
ORDER BY id DESC
LIMIT @row_limit;

-- name: ListDevExceptions :many
SELECT * FROM dev_exceptions
WHERE (@before_id::bigint = 0 OR id < @before_id::bigint)
  AND (@request_id::text = '' OR request_id = @request_id::text)
ORDER BY id DESC
LIMIT @row_limit;

-- name: PruneDevRequests :execrows
DELETE FROM dev_requests WHERE created_at < now() - make_interval(days => @retention_days::int);

-- name: PruneDevQueries :execrows
DELETE FROM dev_queries WHERE created_at < now() - make_interval(days => @retention_days::int);

-- name: PruneDevCacheEvents :execrows
DELETE FROM dev_cache_events WHERE created_at < now() - make_interval(days => @retention_days::int);

-- name: PruneDevQueueEvents :execrows
DELETE FROM dev_queue_events WHERE created_at < now() - make_interval(days => @retention_days::int);

-- name: PruneDevLogs :execrows
DELETE FROM dev_logs WHERE created_at < now() - make_interval(days => @retention_days::int);

-- name: PruneDevExceptions :execrows
DELETE FROM dev_exceptions WHERE created_at < now() - make_interval(days => @retention_days::int);

-- name: DevTrafficByMinute :many
-- Overview: captured requests per minute since @since, with errors and p95.
SELECT date_trunc('minute', created_at)::timestamptz AS minute,
       count(*)::bigint AS requests,
       (count(*) FILTER (WHERE status >= 400 AND status < 500))::bigint AS client_errors,
       (count(*) FILTER (WHERE status >= 500))::bigint AS server_errors,
       (percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms))::double precision AS p95_ms
FROM dev_requests
WHERE created_at >= @since::timestamptz
GROUP BY 1
ORDER BY 1;

-- name: DevTrafficTotals :one
SELECT count(*)::bigint AS requests,
       (count(*) FILTER (WHERE status >= 400 AND status < 500))::bigint AS client_errors,
       (count(*) FILTER (WHERE status >= 500))::bigint AS server_errors,
       coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms), 0)::double precision AS p95_ms
FROM dev_requests
WHERE created_at >= @since::timestamptz;

-- name: CountDevExceptionsSince :one
SELECT count(*)::bigint FROM dev_exceptions WHERE created_at >= @since::timestamptz;

-- name: ListDevExceptionGroups :many
-- Overview: exceptions since @since grouped by class, newest group first.
SELECT class,
       count(*)::bigint AS occurrences,
       max(id)::bigint AS latest_id,
       (array_agg(message ORDER BY id DESC))[1]::text AS latest_message,
       min(created_at)::timestamptz AS first_seen,
       max(created_at)::timestamptz AS last_seen
FROM dev_exceptions
WHERE created_at >= @since::timestamptz
GROUP BY class
ORDER BY last_seen DESC
LIMIT @row_limit;

-- name: DeleteDevTelemetryForUser :one
-- Captured requests (headers, bodies, IP) the user made, or made while
-- acting as someone, or whose bodies carry the user's e-mail address (signing
-- in, before anyone is authenticated), and every row linked to them by
-- request id. Dashboard telemetry is local tooling, so it goes.
WITH requests AS (
    DELETE FROM dev_requests
    WHERE user_id = @user_id::bigint OR impersonator_id = @user_id::bigint
       OR (@email::text <> '' AND (strpos(lower(request_body::text), lower(@email::text)) > 0
                                OR strpos(lower(response_body::text), lower(@email::text)) > 0))
    RETURNING request_id
), queries AS (
    DELETE FROM dev_queries WHERE request_id IN (SELECT request_id FROM requests)
), cache_events AS (
    DELETE FROM dev_cache_events WHERE request_id IN (SELECT request_id FROM requests)
), queue_events AS (
    DELETE FROM dev_queue_events WHERE request_id IN (SELECT request_id FROM requests)
), logs AS (
    DELETE FROM dev_logs WHERE request_id IN (SELECT request_id FROM requests)
), exceptions AS (
    DELETE FROM dev_exceptions WHERE request_id IN (SELECT request_id FROM requests)
)
SELECT count(*)::bigint FROM requests;

-- name: DevRequestStatusMix :one
-- Requests panel: captured requests since @since by status class.
SELECT (count(*) FILTER (WHERE status < 300))::bigint AS success,
       (count(*) FILTER (WHERE status >= 300 AND status < 400))::bigint AS redirect,
       (count(*) FILTER (WHERE status >= 400 AND status < 500))::bigint AS client_errors,
       (count(*) FILTER (WHERE status >= 500))::bigint AS server_errors
FROM dev_requests
WHERE created_at >= @since::timestamptz;

-- name: DevBusiestRoutes :many
-- Requests panel: the routes with the most captured requests since @since.
SELECT method,
       (CASE WHEN route = '' THEN path ELSE route END)::text AS route,
       count(*)::bigint AS requests,
       (count(*) FILTER (WHERE status >= 400))::bigint AS errors,
       (percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms))::double precision AS p95_ms
FROM dev_requests
WHERE created_at >= @since::timestamptz
GROUP BY 1, 2
ORDER BY requests DESC, route
LIMIT @row_limit;

-- name: DevSlowestRoutes :many
-- Requests panel: the routes with the highest p95 since @since.
SELECT method,
       (CASE WHEN route = '' THEN path ELSE route END)::text AS route,
       count(*)::bigint AS requests,
       (count(*) FILTER (WHERE status >= 400))::bigint AS errors,
       (percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms))::double precision AS p95_ms
FROM dev_requests
WHERE created_at >= @since::timestamptz
GROUP BY 1, 2
ORDER BY p95_ms DESC, route
LIMIT @row_limit;

-- name: DevExceptionsByHour :many
-- Exceptions panel: captured exceptions per hour since @since.
SELECT date_trunc('hour', created_at, 'UTC')::timestamptz AS hour,
       count(*)::bigint AS occurrences
FROM dev_exceptions
WHERE created_at >= @since::timestamptz
GROUP BY 1
ORDER BY 1;
