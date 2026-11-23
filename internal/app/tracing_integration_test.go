package app_test

import (
	"context"
	"net/http"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

// OpenTelemetry (phase 26.9): with tracing on, a request is one trace — the
// route-named server span, the module's own span and the SQL under it.
func TestRequestIsTraced(t *testing.T) {
	t.Setenv("OTEL_ENABLED", "true")
	t.Setenv("HTTP_TRUSTED_PROXIES", "192.0.2.0/24") // httptest's peer: its traceparent is continued
	app := testkit.New(t)

	// Swap the OTLP exporter Setup installed for one the test can read.
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
	})

	admin := app.SuperAdmin(t)
	app.As(admin).InTenant(testkit.DefaultTenant, app.DefaultBranch(t, testkit.DefaultTenant)).
		Header("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01").
		Post("/api/v1/branch", map[string]any{"name": "Traced"}).Status(t, http.StatusCreated)

	spans := map[string]tracetest.SpanStub{}
	for _, span := range exporter.GetSpans() {
		if span.SpanContext.TraceID().String() == "4bf92f3577b34da6a3ce929d0e0e4736" {
			spans[span.Name] = span
		}
	}
	server, ok := spans["POST /api/v1/branch"]
	if !ok {
		t.Fatalf("no server span named by route; traced %v", keys(spans))
	}
	attrs := map[string]string{}
	for _, kv := range server.Attributes {
		attrs[string(kv.Key)] = kv.Value.String()
	}
	if attrs["http.response.status_code"] != "201" || attrs["enduser.id"] == "" || attrs["tenant.id"] != "1" {
		t.Errorf("server span attributes = %v", attrs)
	}
	create, ok := spans["branches.create"]
	if !ok || create.Parent.SpanID() != server.SpanContext.SpanID() {
		t.Fatalf("branches.create is not under the server span; traced %v", keys(spans))
	}
	var underCreate int
	for name, span := range spans {
		if len(name) > 3 && name[:3] == "db " && span.Parent.SpanID() == create.SpanContext.SpanID() {
			underCreate++
		}
	}
	if underCreate == 0 {
		t.Errorf("no SQL spans under branches.create; traced %v", keys(spans))
	}
}

func keys(spans map[string]tracetest.SpanStub) []string {
	names := make([]string, 0, len(spans))
	for name := range spans {
		names = append(names, name)
	}
	return names
}
