package dev

import (
	"context"
	"encoding/json"
	"time"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/telemetry"
)

// TelemetrySink persists collector batches into the dev_* tables.
type TelemetrySink struct {
	queries *dbgen.Queries
	timeout time.Duration
}

// NewTelemetrySink creates the sqlc-backed telemetry sink.
func NewTelemetrySink(queries *dbgen.Queries) *TelemetrySink {
	return &TelemetrySink{queries: queries, timeout: 15 * time.Second}
}

func (s *TelemetrySink) ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), s.timeout)
}

// InsertRequests implements telemetry.Sink.
func (s *TelemetrySink) InsertRequests(entries []telemetry.RequestEntry) error {
	ctx, cancel := s.ctx()
	defer cancel()
	for _, entry := range entries {
		headers, err := json.Marshal(entry.Headers)
		if err != nil {
			headers = []byte("{}")
		}
		if _, err := s.queries.InsertDevRequest(ctx, dbgen.InsertDevRequestParams{
			RequestID:      entry.RequestID,
			Method:         entry.Method,
			Path:           entry.Path,
			Route:          entry.Route,
			Status:         int32(entry.Status),
			DurationMs:     entry.DurationMS,
			RequestBytes:   entry.RequestBytes,
			ResponseBytes:  entry.ResponseBytes,
			UserID:         entry.UserID,
			ImpersonatorID: entry.ImpersonatorID,
			TenantID:       entry.TenantID,
			BranchID:       entry.BranchID,
			Ip:             strPtr(entry.IP),
			UserAgent:      strPtr(entry.UserAgent),
			Headers:        headers,
			RequestBody:    entry.RequestBody,
			ResponseBody:   entry.ResponseBody,
			CreatedAt:      timestamp(entry.CreatedAt),
		}); err != nil {
			return err
		}
	}
	return nil
}

// InsertQueries implements telemetry.Sink.
func (s *TelemetrySink) InsertQueries(entries []telemetry.QueryEntry) error {
	ctx, cancel := s.ctx()
	defer cancel()
	for _, entry := range entries {
		if err := s.queries.InsertDevQuery(ctx, dbgen.InsertDevQueryParams{
			RequestID:  strPtr(entry.RequestID),
			Sql:        entry.SQL,
			Args:       entry.Args,
			DurationMs: entry.DurationMS,
			RowCount:   entry.RowCount,
			Error:      strPtr(entry.Error),
			CreatedAt:  timestamp(entry.CreatedAt),
		}); err != nil {
			return err
		}
	}
	return nil
}

// InsertCacheEvents implements telemetry.Sink.
func (s *TelemetrySink) InsertCacheEvents(entries []telemetry.CacheEventEntry) error {
	ctx, cancel := s.ctx()
	defer cancel()
	for _, entry := range entries {
		if err := s.queries.InsertDevCacheEvent(ctx, dbgen.InsertDevCacheEventParams{
			RequestID:  strPtr(entry.RequestID),
			Operation:  entry.Operation,
			Key:        entry.Key,
			Hit:        entry.Hit,
			DurationMs: entry.DurationMS,
			CreatedAt:  timestamp(entry.CreatedAt),
		}); err != nil {
			return err
		}
	}
	return nil
}

// InsertQueueEvents implements telemetry.Sink.
func (s *TelemetrySink) InsertQueueEvents(entries []telemetry.QueueEventEntry) error {
	ctx, cancel := s.ctx()
	defer cancel()
	for _, entry := range entries {
		if err := s.queries.InsertDevQueueEvent(ctx, dbgen.InsertDevQueueEventParams{
			RequestID:  strPtr(entry.RequestID),
			Event:      entry.Event,
			Queue:      entry.Queue,
			Kind:       entry.Kind,
			JobID:      entry.JobID,
			DurationMs: entry.DurationMS,
			Error:      strPtr(entry.Error),
			CreatedAt:  timestamp(entry.CreatedAt),
		}); err != nil {
			return err
		}
	}
	return nil
}

// InsertLogs implements telemetry.Sink.
func (s *TelemetrySink) InsertLogs(entries []telemetry.LogEntry) error {
	ctx, cancel := s.ctx()
	defer cancel()
	for _, entry := range entries {
		if err := s.queries.InsertDevLog(ctx, dbgen.InsertDevLogParams{
			RequestID:  strPtr(entry.RequestID),
			Level:      entry.Level,
			Message:    entry.Message,
			Attributes: entry.Attributes,
			CreatedAt:  timestamp(entry.CreatedAt),
		}); err != nil {
			return err
		}
	}
	return nil
}

// InsertExceptions implements telemetry.Sink.
func (s *TelemetrySink) InsertExceptions(entries []telemetry.ExceptionEntry) error {
	ctx, cancel := s.ctx()
	defer cancel()
	for _, entry := range entries {
		if err := s.queries.InsertDevException(ctx, dbgen.InsertDevExceptionParams{
			RequestID: strPtr(entry.RequestID),
			Class:     entry.Class,
			Message:   entry.Message,
			Stack:     strPtr(entry.Stack),
			CreatedAt: timestamp(entry.CreatedAt),
		}); err != nil {
			return err
		}
	}
	return nil
}

func strPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
