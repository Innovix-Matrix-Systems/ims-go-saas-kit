package queue

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database/dbtest"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/tracing"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// A queued job continues the trace of the request that queued it (26.9).
func TestRiverEngineCarriesTrace(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Clean(t, pool, "river_job")

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	previous, previousPropagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		otel.SetTextMapPropagator(previousPropagator)
	})

	registry := NewRegistry()
	seen := make(chan trace.SpanContext, 1)
	if err := registry.Register(Definition{
		Kind:  "test.river.trace",
		Queue: QueueDefault,
		Handle: func(ctx context.Context, _ []byte) error {
			seen <- trace.SpanContextFromContext(ctx)
			return nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	engine, err := NewRiverEngine(pool, registry, testQueueConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatalf("NewRiverEngine() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	requestCtx, request := tracing.Start(tenancy.WithScope(ctx, tenancy.Scope{TenantID: 1}), "request")
	if err := engine.Enqueue(requestCtx, "test.river.trace", nil, EnqueueOptions{}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	request.End()
	if err := engine.Once(ctx, 20*time.Second); err != nil {
		t.Fatalf("Once() error = %v", err)
	}

	select {
	case job := <-seen:
		if job.TraceID() != request.SpanContext().TraceID() {
			t.Fatalf("job trace = %s, want the request's %s", job.TraceID(), request.SpanContext().TraceID())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not run")
	}
	var found bool
	for _, span := range exporter.GetSpans() {
		if span.Name == "job test.river.trace" && span.Parent.SpanID() == request.SpanContext().SpanID() {
			found = true
		}
	}
	if !found {
		t.Fatalf("no job span under the request; recorded %d spans", len(exporter.GetSpans()))
	}
}
