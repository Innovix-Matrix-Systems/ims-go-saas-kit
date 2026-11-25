package app_test

import (
	"context"
	"net/http"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

// OpenTelemetry: with tracing on, a request is one trace — the
// route-named server span, the module's own span and the SQL under it.
func TestRequestIsTraced(t *testing.T) {
	t.Setenv("OTEL_ENABLED", "true")
	t.Setenv("HTTP_TRUSTED_PROXIES", "192.0.2.0/24") // httptest's peer: its traceparent is continued
	app := testkit.New(t)
	exporter := recordSpans(t)

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

// TestServiceOperationsAreTraced: each service write is its own span under
// the request's server span, and a refusal is recorded on that span (its
// apperr code in error.type) without marking it failed.
func TestServiceOperationsAreTraced(t *testing.T) {
	t.Setenv("OTEL_ENABLED", "true")
	app := testkit.New(t)
	exporter := recordSpans(t)
	admin := app.User(t)

	app.Guest().Post("/api/v1/login", map[string]any{"email": admin.Email, "password": testkit.Password, "device": "web"}).
		Status(t, http.StatusOK)
	app.As(admin).Post("/api/v1/role", map[string]any{"name": "Traced Role"}).Status(t, http.StatusCreated)
	app.As(admin).Patch("/api/v1/branch/999999", map[string]any{"name": "Nowhere"}).Status(t, http.StatusNotFound)

	spans := exporter.GetSpans()
	for _, want := range []struct{ server, operation string }{
		{"POST /api/v1/login", "authn.login"},
		{"POST /api/v1/role", "access.create_role"},
		{"PATCH /api/v1/branch/{id}", "branches.update"},
	} {
		operation, ok := childSpan(spans, want.server, want.operation)
		if !ok {
			t.Errorf("no %s span under %s", want.operation, want.server)
			continue
		}
		if want.operation == "branches.update" {
			kind := ""
			for _, kv := range operation.Attributes {
				if kv.Key == "error.type" {
					kind = kv.Value.AsString()
				}
			}
			if kind != "RESOURCE_NOT_FOUND" || operation.Status.Code == codes.Error {
				t.Errorf("refused update: error.type %q, status %v; want the code, not a failed span", kind, operation.Status.Code)
			}
		} else if operation.Status.Code == codes.Error {
			t.Errorf("%s failed: %s", want.operation, operation.Status.Description)
		}
	}
}

// recordSpans swaps the OTLP exporter Setup installed for one the test can
// read; the previous provider comes back after the test.
func recordSpans(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
	})
	return exporter
}

// childSpan finds the span named operation whose parent is the span named server.
func childSpan(spans tracetest.SpanStubs, server, operation string) (tracetest.SpanStub, bool) {
	for _, parent := range spans {
		if parent.Name != server {
			continue
		}
		for _, span := range spans {
			if span.Name == operation && span.Parent.SpanID() == parent.SpanContext.SpanID() {
				return span, true
			}
		}
	}
	return tracetest.SpanStub{}, false
}

func keys(spans map[string]tracetest.SpanStub) []string {
	names := make([]string, 0, len(spans))
	for name := range spans {
		names = append(names, name)
	}
	return names
}
