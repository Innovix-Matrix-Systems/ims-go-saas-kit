package telemetry

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
)

// LazyTracer is attached to the database pools before the collector exists
// (the collector's sink needs those same pools). Attach binds the real
// collector once the platform is built.
type LazyTracer struct {
	tracer atomic.Pointer[Tracer]
}

// NewLazyTracer creates an unattached tracer (all calls no-op).
func NewLazyTracer() *LazyTracer { return &LazyTracer{} }

// Attach binds the collector-backed tracer.
func (l *LazyTracer) Attach(collector *Collector) {
	l.tracer.Store(NewTracer(collector))
}

// TraceQueryStart implements pgx.QueryTracer.
func (l *LazyTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if tracer := l.tracer.Load(); tracer != nil {
		return tracer.TraceQueryStart(ctx, conn, data)
	}
	return ctx
}

// TraceQueryEnd implements pgx.QueryTracer.
func (l *LazyTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	if tracer := l.tracer.Load(); tracer != nil {
		tracer.TraceQueryEnd(ctx, conn, data)
	}
}

type queryStartKey struct{}

type queryStart struct {
	at   time.Time
	sql  string
	args []any
}

// Tracer is a pgx QueryTracer that captures statements executed inside a
// captured request context.
type Tracer struct {
	collector *Collector
}

// NewTracer creates the pgx query tracer.
func NewTracer(collector *Collector) *Tracer {
	return &Tracer{collector: collector}
}

// TraceQueryStart implements pgx.QueryTracer.
func (t *Tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if t == nil || t.collector == nil || !t.collector.capture.Enabled || !t.collector.capture.Capture.SQL {
		return ctx
	}
	if !CaptureFromContext(ctx) {
		return ctx
	}
	return context.WithValue(ctx, queryStartKey{}, queryStart{at: time.Now(), sql: data.SQL, args: data.Args})
}

// TraceQueryEnd implements pgx.QueryTracer.
func (t *Tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if t == nil || t.collector == nil {
		return
	}
	start, ok := ctx.Value(queryStartKey{}).(queryStart)
	if !ok {
		return
	}
	errorText := ""
	if data.Err != nil {
		errorText = data.Err.Error()
	}
	t.collector.Query(QueryEntry{
		RequestID:  httpx.RequestIDFromContext(ctx),
		SQL:        start.sql,
		Args:       t.collector.redactor.Args(start.sql, start.args),
		DurationMS: float64(time.Since(start.at).Microseconds()) / 1000.0,
		RowCount:   data.CommandTag.RowsAffected(),
		Error:      errorText,
		CreatedAt:  time.Now(),
	})
}
