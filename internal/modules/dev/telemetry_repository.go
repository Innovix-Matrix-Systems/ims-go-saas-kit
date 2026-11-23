package dev

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
)

// TelemetryRequestSummary is the list representation of a captured request.
type TelemetryRequestSummary struct {
	ID            int64   `json:"id"`
	RequestID     string  `json:"request_id"`
	Method        string  `json:"method"`
	Path          string  `json:"path"`
	Route         string  `json:"route"`
	Status        int32   `json:"status"`
	DurationMS    float64 `json:"duration_ms"`
	RequestBytes  int64   `json:"request_bytes"`
	ResponseBytes int64   `json:"response_bytes"`
	UserID        *int64  `json:"user_id"`
	// ImpersonatorID is set when the request was made while acting as
	// user_id (X-Acting-User).
	ImpersonatorID *int64    `json:"impersonator_id"`
	TenantID       *int64    `json:"tenant_id"`
	IP             *string   `json:"ip"`
	CreatedAt      time.Time `json:"created_at"`
}

// TelemetryRequest is the full request record with redacted payloads.
type TelemetryRequest struct {
	TelemetryRequestSummary
	BranchID     *int64          `json:"branch_id"`
	UserAgent    *string         `json:"user_agent"`
	Headers      json.RawMessage `json:"headers"`
	RequestBody  json.RawMessage `json:"request_body"`
	ResponseBody json.RawMessage `json:"response_body"`
}

// TelemetryQuery is one captured SQL statement.
type TelemetryQuery struct {
	ID         int64           `json:"id"`
	RequestID  *string         `json:"request_id"`
	SQL        string          `json:"sql"`
	Args       json.RawMessage `json:"args"`
	DurationMS float64         `json:"duration_ms"`
	RowCount   int64           `json:"row_count"`
	Error      *string         `json:"error"`
	CreatedAt  time.Time       `json:"created_at"`
}

// TelemetryCacheEvent is one cache operation.
type TelemetryCacheEvent struct {
	ID         int64     `json:"id"`
	RequestID  *string   `json:"request_id"`
	Operation  string    `json:"operation"`
	Key        string    `json:"key"`
	Hit        *bool     `json:"hit"`
	DurationMS float64   `json:"duration_ms"`
	CreatedAt  time.Time `json:"created_at"`
}

// TelemetryQueueEvent is one queue dispatch or execution outcome.
type TelemetryQueueEvent struct {
	ID         int64     `json:"id"`
	RequestID  *string   `json:"request_id"`
	Event      string    `json:"event"`
	Queue      string    `json:"queue"`
	Kind       string    `json:"kind"`
	JobID      *int64    `json:"job_id"`
	DurationMS float64   `json:"duration_ms"`
	Error      *string   `json:"error"`
	CreatedAt  time.Time `json:"created_at"`
}

// TelemetryLog is one captured log record.
type TelemetryLog struct {
	ID         int64           `json:"id"`
	RequestID  *string         `json:"request_id"`
	Level      string          `json:"level"`
	Message    string          `json:"message"`
	Attributes json.RawMessage `json:"attributes"`
	CreatedAt  time.Time       `json:"created_at"`
}

// TelemetryException is one captured panic/exception.
type TelemetryException struct {
	ID        int64     `json:"id"`
	RequestID *string   `json:"request_id"`
	Class     string    `json:"class"`
	Message   string    `json:"message"`
	Stack     *string   `json:"stack"`
	CreatedAt time.Time `json:"created_at"`
}

// TelemetryRequestDetail bundles a request with everything correlated to it.
type TelemetryRequestDetail struct {
	Request     TelemetryRequest      `json:"request"`
	Queries     []TelemetryQuery      `json:"queries"`
	CacheEvents []TelemetryCacheEvent `json:"cache_events"`
	QueueEvents []TelemetryQueueEvent `json:"queue_events"`
	Logs        []TelemetryLog        `json:"logs"`
	Exceptions  []TelemetryException  `json:"exceptions"`
}

// RequestFilter filters the request list.
type RequestFilter struct {
	BeforeID  int64
	Limit     int32
	Method    string
	Status    int32
	Search    string
	TenantID  int64
	UserID    int64
	RequestID string
}

func mapTelemetryRequestSummary(row dbgen.DevRequest) TelemetryRequestSummary {
	return TelemetryRequestSummary{
		ID:             row.ID,
		RequestID:      row.RequestID,
		Method:         row.Method,
		Path:           row.Path,
		Route:          row.Route,
		Status:         row.Status,
		DurationMS:     row.DurationMs,
		RequestBytes:   row.RequestBytes,
		ResponseBytes:  row.ResponseBytes,
		UserID:         row.UserID,
		ImpersonatorID: row.ImpersonatorID,
		TenantID:       row.TenantID,
		IP:             row.Ip,
		CreatedAt:      row.CreatedAt.Time,
	}
}

func mapTelemetryRequest(row dbgen.DevRequest) TelemetryRequest {
	return TelemetryRequest{
		TelemetryRequestSummary: mapTelemetryRequestSummary(row),
		BranchID:                row.BranchID,
		UserAgent:               row.UserAgent,
		Headers:                 rawJSON(row.Headers),
		RequestBody:             rawJSON(row.RequestBody),
		ResponseBody:            rawJSON(row.ResponseBody),
	}
}

func mapTelemetryQuery(row dbgen.DevQuery) TelemetryQuery {
	return TelemetryQuery{
		ID:         row.ID,
		RequestID:  row.RequestID,
		SQL:        row.Sql,
		Args:       rawJSON(row.Args),
		DurationMS: row.DurationMs,
		RowCount:   row.RowCount,
		Error:      row.Error,
		CreatedAt:  row.CreatedAt.Time,
	}
}

func mapTelemetryCacheEvent(row dbgen.DevCacheEvent) TelemetryCacheEvent {
	return TelemetryCacheEvent{
		ID:         row.ID,
		RequestID:  row.RequestID,
		Operation:  row.Operation,
		Key:        row.Key,
		Hit:        row.Hit,
		DurationMS: row.DurationMs,
		CreatedAt:  row.CreatedAt.Time,
	}
}

func mapTelemetryQueueEvent(row dbgen.DevQueueEvent) TelemetryQueueEvent {
	return TelemetryQueueEvent{
		ID:         row.ID,
		RequestID:  row.RequestID,
		Event:      row.Event,
		Queue:      row.Queue,
		Kind:       row.Kind,
		JobID:      row.JobID,
		DurationMS: row.DurationMs,
		Error:      row.Error,
		CreatedAt:  row.CreatedAt.Time,
	}
}

func mapTelemetryLog(row dbgen.DevLog) TelemetryLog {
	return TelemetryLog{
		ID:         row.ID,
		RequestID:  row.RequestID,
		Level:      row.Level,
		Message:    row.Message,
		Attributes: rawJSON(row.Attributes),
		CreatedAt:  row.CreatedAt.Time,
	}
}

func mapTelemetryException(row dbgen.DevException) TelemetryException {
	return TelemetryException{
		ID:        row.ID,
		RequestID: row.RequestID,
		Class:     row.Class,
		Message:   row.Message,
		Stack:     row.Stack,
		CreatedAt: row.CreatedAt.Time,
	}
}

func rawJSON(value []byte) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	return json.RawMessage(value)
}

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

// TelemetryRepository reads and prunes the telemetry tables.
type TelemetryRepository interface {
	ListRequests(ctx context.Context, filter RequestFilter) ([]TelemetryRequestSummary, error)
	GetRequestDetail(ctx context.Context, id int64) (*TelemetryRequestDetail, error)
	ListLogs(ctx context.Context, beforeID int64, limit int32, level, requestID, search string) ([]TelemetryLog, error)
	GetLog(ctx context.Context, id int64) (*TelemetryLog, error)
	ListExceptions(ctx context.Context, beforeID int64, limit int32, requestID string) ([]TelemetryException, error)
	GetException(ctx context.Context, id int64) (*TelemetryException, error)
	Prune(ctx context.Context, retentionDays int) (map[string]int64, error)
	// Traffic, CountExceptionsSince and ExceptionGroups feed the Overview.
	Traffic(ctx context.Context, since time.Time) ([]TrafficMinute, TrafficTotals, error)
	CountExceptionsSince(ctx context.Context, since time.Time) (int64, error)
	ExceptionGroups(ctx context.Context, since time.Time, limit int32) ([]ExceptionGroup, error)
}

type telemetryRepository struct {
	queries *dbgen.Queries
}

// NewTelemetryRepository creates the sqlc-backed telemetry repository.
func NewTelemetryRepository(queries *dbgen.Queries) TelemetryRepository {
	return &telemetryRepository{queries: queries}
}

func (r *telemetryRepository) ListRequests(ctx context.Context, filter RequestFilter) ([]TelemetryRequestSummary, error) {
	rows, err := r.queries.ListDevRequests(ctx, dbgen.ListDevRequestsParams{
		BeforeID:  filter.BeforeID,
		Method:    filter.Method,
		Status:    filter.Status,
		Search:    filter.Search,
		TenantID:  filter.TenantID,
		UserID:    filter.UserID,
		RequestID: filter.RequestID,
		RowLimit:  filter.Limit,
	})
	if err != nil {
		return nil, database.MapError("dev.list_requests", err)
	}
	out := make([]TelemetryRequestSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapTelemetryRequestSummary(row))
	}
	return out, nil
}

func (r *telemetryRepository) GetRequestDetail(ctx context.Context, id int64) (*TelemetryRequestDetail, error) {
	row, err := r.queries.GetDevRequestByID(ctx, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, database.MapError("dev.get_request", err)
	}
	detail := &TelemetryRequestDetail{
		Request:     mapTelemetryRequest(row),
		Queries:     []TelemetryQuery{},
		CacheEvents: []TelemetryCacheEvent{},
		QueueEvents: []TelemetryQueueEvent{},
		Logs:        []TelemetryLog{},
		Exceptions:  []TelemetryException{},
	}
	requestID := row.RequestID
	queries, err := r.queries.ListDevQueriesByRequestID(ctx, &requestID)
	if err != nil {
		return nil, database.MapError("dev.list_queries", err)
	}
	for _, item := range queries {
		detail.Queries = append(detail.Queries, mapTelemetryQuery(item))
	}
	cacheEvents, err := r.queries.ListDevCacheEventsByRequestID(ctx, &requestID)
	if err != nil {
		return nil, database.MapError("dev.list_cache_events", err)
	}
	for _, item := range cacheEvents {
		detail.CacheEvents = append(detail.CacheEvents, mapTelemetryCacheEvent(item))
	}
	queueEvents, err := r.queries.ListDevQueueEventsByRequestID(ctx, &requestID)
	if err != nil {
		return nil, database.MapError("dev.list_queue_events", err)
	}
	for _, item := range queueEvents {
		detail.QueueEvents = append(detail.QueueEvents, mapTelemetryQueueEvent(item))
	}
	logs, err := r.queries.ListDevLogsByRequestID(ctx, &requestID)
	if err != nil {
		return nil, database.MapError("dev.list_request_logs", err)
	}
	for _, item := range logs {
		detail.Logs = append(detail.Logs, mapTelemetryLog(item))
	}
	exceptions, err := r.queries.ListDevExceptionsByRequestID(ctx, &requestID)
	if err != nil {
		return nil, database.MapError("dev.list_request_exceptions", err)
	}
	for _, item := range exceptions {
		detail.Exceptions = append(detail.Exceptions, mapTelemetryException(item))
	}
	return detail, nil
}

func (r *telemetryRepository) ListLogs(ctx context.Context, beforeID int64, limit int32, level, requestID, search string) ([]TelemetryLog, error) {
	rows, err := r.queries.ListDevLogs(ctx, dbgen.ListDevLogsParams{
		BeforeID:  beforeID,
		Level:     level,
		RequestID: requestID,
		Search:    search,
		RowLimit:  limit,
	})
	if err != nil {
		return nil, database.MapError("dev.list_logs", err)
	}
	out := make([]TelemetryLog, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapTelemetryLog(row))
	}
	return out, nil
}

func (r *telemetryRepository) GetLog(ctx context.Context, id int64) (*TelemetryLog, error) {
	row, err := r.queries.GetDevLogByID(ctx, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, database.MapError("dev.get_log", err)
	}
	mapped := mapTelemetryLog(row)
	return &mapped, nil
}

func (r *telemetryRepository) ListExceptions(ctx context.Context, beforeID int64, limit int32, requestID string) ([]TelemetryException, error) {
	rows, err := r.queries.ListDevExceptions(ctx, dbgen.ListDevExceptionsParams{
		BeforeID:  beforeID,
		RequestID: requestID,
		RowLimit:  limit,
	})
	if err != nil {
		return nil, database.MapError("dev.list_exceptions", err)
	}
	out := make([]TelemetryException, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapTelemetryException(row))
	}
	return out, nil
}

func (r *telemetryRepository) GetException(ctx context.Context, id int64) (*TelemetryException, error) {
	row, err := r.queries.GetDevExceptionByID(ctx, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, database.MapError("dev.get_exception", err)
	}
	mapped := mapTelemetryException(row)
	return &mapped, nil
}

func (r *telemetryRepository) Prune(ctx context.Context, retentionDays int) (map[string]int64, error) {
	days := int32(retentionDays)
	deleted := map[string]int64{}
	steps := []struct {
		name string
		run  func() (int64, error)
	}{
		{"requests", func() (int64, error) { return r.queries.PruneDevRequests(ctx, days) }},
		{"queries", func() (int64, error) { return r.queries.PruneDevQueries(ctx, days) }},
		{"cache_events", func() (int64, error) { return r.queries.PruneDevCacheEvents(ctx, days) }},
		{"queue_events", func() (int64, error) { return r.queries.PruneDevQueueEvents(ctx, days) }},
		{"logs", func() (int64, error) { return r.queries.PruneDevLogs(ctx, days) }},
		{"exceptions", func() (int64, error) { return r.queries.PruneDevExceptions(ctx, days) }},
	}
	for _, step := range steps {
		count, err := step.run()
		if err != nil {
			return deleted, database.MapError("dev.prune_"+step.name, err)
		}
		deleted[step.name] = count
	}
	return deleted, nil
}

// TrafficMinute is one minute of captured requests.
type TrafficMinute struct {
	Minute       time.Time `json:"minute"`
	Requests     int64     `json:"requests"`
	ClientErrors int64     `json:"client_errors"`
	ServerErrors int64     `json:"server_errors"`
	P95Ms        float64   `json:"p95_ms"`
}

// TrafficTotals sums a traffic window.
type TrafficTotals struct {
	Requests     int64   `json:"requests"`
	ClientErrors int64   `json:"client_errors"`
	ServerErrors int64   `json:"server_errors"`
	P95Ms        float64 `json:"p95_ms"`
}

// ExceptionGroup is the exceptions of one class in a window.
type ExceptionGroup struct {
	Class         string    `json:"class"`
	Occurrences   int64     `json:"occurrences"`
	LatestID      int64     `json:"latest_id"`
	LatestMessage string    `json:"latest_message"`
	FirstSeen     time.Time `json:"first_seen"`
	LastSeen      time.Time `json:"last_seen"`
}

func (r *telemetryRepository) Traffic(ctx context.Context, since time.Time) ([]TrafficMinute, TrafficTotals, error) {
	rows, err := r.queries.DevTrafficByMinute(ctx, timestamp(since))
	if err != nil {
		return nil, TrafficTotals{}, database.MapError("dev.traffic", err)
	}
	minutes := make([]TrafficMinute, 0, len(rows))
	for _, row := range rows {
		minutes = append(minutes, TrafficMinute{
			Minute:       row.Minute.Time,
			Requests:     row.Requests,
			ClientErrors: row.ClientErrors,
			ServerErrors: row.ServerErrors,
			P95Ms:        row.P95Ms,
		})
	}
	totals, err := r.queries.DevTrafficTotals(ctx, timestamp(since))
	if err != nil {
		return nil, TrafficTotals{}, database.MapError("dev.traffic_totals", err)
	}
	return minutes, TrafficTotals{
		Requests:     totals.Requests,
		ClientErrors: totals.ClientErrors,
		ServerErrors: totals.ServerErrors,
		P95Ms:        totals.P95Ms,
	}, nil
}

func (r *telemetryRepository) CountExceptionsSince(ctx context.Context, since time.Time) (int64, error) {
	count, err := r.queries.CountDevExceptionsSince(ctx, timestamp(since))
	if err != nil {
		return 0, database.MapError("dev.count_exceptions", err)
	}
	return count, nil
}

func (r *telemetryRepository) ExceptionGroups(ctx context.Context, since time.Time, limit int32) ([]ExceptionGroup, error) {
	rows, err := r.queries.ListDevExceptionGroups(ctx, dbgen.ListDevExceptionGroupsParams{Since: timestamp(since), RowLimit: limit})
	if err != nil {
		return nil, database.MapError("dev.exception_groups", err)
	}
	out := make([]ExceptionGroup, 0, len(rows))
	for _, row := range rows {
		out = append(out, ExceptionGroup{
			Class:         row.Class,
			Occurrences:   row.Occurrences,
			LatestID:      row.LatestID,
			LatestMessage: row.LatestMessage,
			FirstSeen:     row.FirstSeen.Time,
			LastSeen:      row.LastSeen.Time,
		})
	}
	return out, nil
}
