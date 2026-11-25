package tracing

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// record installs an in-memory exporter that samples everything; the
// previous provider comes back after the test.
func record(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	previous, previousPropagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
		otel.SetTextMapPropagator(previousPropagator)
	})
	return exporter
}

func spanNamed(t *testing.T, exporter *tracetest.InMemoryExporter, name string) tracetest.SpanStub {
	t.Helper()
	var names []string
	for _, span := range exporter.GetSpans() {
		if span.Name == name {
			return span
		}
		names = append(names, span.Name)
	}
	t.Fatalf("no span %q; recorded %v", name, names)
	return tracetest.SpanStub{}
}

func attr(span tracetest.SpanStub, key string) (string, bool) {
	for _, kv := range span.Attributes {
		if string(kv.Key) == key {
			return kv.Value.String(), true
		}
	}
	return "", false
}

func TestMiddlewareNamesSpanByRouteAndMarks5xx(t *testing.T) {
	exporter := record(t)
	router := chi.NewRouter()
	router.Use(Middleware(func(context.Context) (int64, int64, int64) { return 7, 3, 0 }, nil))
	router.Get("/api/v1/branch/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	router.Get("/api/v1/user/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/branch/42", nil))
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/user/9", nil))
	if notFound := spanNamed(t, exporter, "GET /api/v1/user/{id}"); notFound.Status.Code == codes.Error {
		t.Error("a 404 marked the span failed (4xx is the caller's error)")
	}

	span := spanNamed(t, exporter, "GET /api/v1/branch/{id}")
	if span.SpanKind != trace.SpanKindServer {
		t.Errorf("kind = %v, want server", span.SpanKind)
	}
	if span.Status.Code != codes.Error {
		t.Errorf("status = %v, want error for a 500", span.Status.Code)
	}
	if got, _ := attr(span, "http.response.status_code"); got != "500" {
		t.Errorf("status_code = %q, want 500", got)
	}
	if got, _ := attr(span, "enduser.id"); got != "7" {
		t.Errorf("enduser.id = %q, want 7", got)
	}
	if got, _ := attr(span, "tenant.id"); got != "3" {
		t.Errorf("tenant.id = %q, want 3", got)
	}
	if _, ok := attr(span, "impersonator.id"); ok {
		t.Error("impersonator.id set without an impersonator")
	}
}

func TestMiddlewareContinuesOnlyTrustedCallerTraces(t *testing.T) {
	exporter := record(t)
	const parent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	handler := Middleware(nil, func(r *http.Request) bool { return r.Header.Get("X-Via-Proxy") == "yes" })(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))

	trusted := httptest.NewRequest(http.MethodPost, "/trusted", nil)
	trusted.Header.Set("traceparent", parent)
	trusted.Header.Set("X-Via-Proxy", "yes")
	handler.ServeHTTP(httptest.NewRecorder(), trusted)
	outside := httptest.NewRequest(http.MethodPost, "/outside", nil)
	outside.Header.Set("traceparent", parent)
	handler.ServeHTTP(httptest.NewRecorder(), outside)

	continued := spanNamed(t, exporter, "POST /trusted")
	if got := continued.SpanContext.TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trusted trace id = %s, want the caller's", got)
	}
	if continued.Status.Code == codes.Error {
		t.Error("a 204 marked the span failed")
	}
	fresh := spanNamed(t, exporter, "POST /outside")
	if fresh.SpanContext.TraceID().String() == "4bf92f3577b34da6a3ce929d0e0e4736" || fresh.Parent.IsValid() {
		t.Error("an untrusted caller's trace was continued")
	}
	if len(fresh.Links) != 1 || fresh.Links[0].SpanContext.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("links = %+v, want one link to the caller's trace", fresh.Links)
	}
}

func TestInjectExtractLinksJobToRequest(t *testing.T) {
	exporter := record(t)
	ctx, parent := Start(context.Background(), "request")
	carrier := Inject(ctx)
	parent.End()
	if carrier["traceparent"] == "" {
		t.Fatalf("carrier = %v, want a traceparent", carrier)
	}

	_, job := Start(Extract(context.Background(), carrier), "job demo")
	job.End()

	request, queued := spanNamed(t, exporter, "request"), spanNamed(t, exporter, "job demo")
	if queued.Parent.SpanID() != request.SpanContext.SpanID() || queued.SpanContext.TraceID() != request.SpanContext.TraceID() {
		t.Error("the job span is not a child of the request span")
	}
	if Inject(context.Background()) != nil {
		t.Error("Inject without a span should return nil (nothing stored in the job)")
	}
}

func TestTransportInjectsTraceparent(t *testing.T) {
	exporter := record(t)
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	ctx, parent := Start(context.Background(), "deliver")
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/services/T01/pathsecret?token=querysecret", nil)
	res, err := (&http.Client{Transport: Transport(nil)}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	parent.End()

	if seen == "" {
		t.Fatal("the receiver got no traceparent")
	}
	client := spanNamed(t, exporter, "POST "+strings.TrimPrefix(server.URL, "http://"))
	if client.SpanKind != trace.SpanKindClient || client.Status.Code != codes.Error {
		t.Errorf("kind = %v status = %v, want a failed client span for a 502", client.SpanKind, client.Status.Code)
	}
	for _, kv := range client.Attributes {
		if value := kv.Value.String(); strings.Contains(value, "secret") || strings.Contains(value, "/services") {
			t.Errorf("attribute %s = %q leaks the URL path or query", kv.Key, value)
		}
	}
	if host, _ := attr(client, "server.address"); host != "127.0.0.1" {
		t.Errorf("server.address = %q, want the host", host)
	}
}

func TestQueryTracerJoinsOnlyExistingTraces(t *testing.T) {
	exporter := record(t)
	tracer := QueryTracer{}
	sql := "-- name: GetBranch :one\nSELECT * FROM branches WHERE id = $1"

	ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: sql})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	if n := len(exporter.GetSpans()); n != 0 {
		t.Fatalf("a query outside a trace recorded %d spans, want 0", n)
	}

	parentCtx, parent := Start(context.Background(), "request")
	ctx = tracer.TraceQueryStart(parentCtx, nil, pgx.TraceQueryStartData{SQL: sql, Args: []any{"private"}})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: pgx.ErrNoRows})
	ctx = tracer.TraceQueryStart(parentCtx, nil, pgx.TraceQueryStartData{SQL: "UPDATE branches SET name = $1"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: errors.New("deadlock")})
	parent.End()

	named := spanNamed(t, exporter, "db GetBranch")
	if named.Status.Code == codes.Error {
		t.Error("no rows marked the query failed")
	}
	for _, kv := range named.Attributes {
		if strings.Contains(kv.Value.String(), "private") {
			t.Errorf("attribute %s carries a query argument", kv.Key)
		}
	}
	if verb := spanNamed(t, exporter, "db UPDATE"); verb.Status.Code != codes.Error {
		t.Error("a failed query is not marked failed")
	}
}

func TestLogHandlerAddsTraceIDs(t *testing.T) {
	record(t)
	var out bytes.Buffer
	logger := slog.New(LogHandler(slog.NewJSONHandler(&out, nil))).With("component", "test")

	ctx, span := Start(context.Background(), "request")
	logger.InfoContext(ctx, "inside")
	span.End()
	logger.InfoContext(context.Background(), "outside")

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if !strings.Contains(lines[0], `"trace_id":"`+TraceID(ctx)+`"`) || !strings.Contains(lines[0], `"span_id"`) {
		t.Errorf("inside line = %s, want trace and span ids", lines[0])
	}
	if strings.Contains(lines[1], "trace_id") {
		t.Errorf("outside line = %s, want no trace id", lines[1])
	}
}

func TestFailAndRecordPanic(t *testing.T) {
	exporter := record(t)
	ctx, span := Start(context.Background(), "work")
	if err := Fail(ctx, nil); err != nil {
		t.Errorf("Fail(nil) = %v", err)
	}
	boom := errors.New("boom")
	if err := Fail(ctx, boom); !errors.Is(err, boom) {
		t.Errorf("Fail returned %v, want the error", err)
	}
	span.End()
	if work := spanNamed(t, exporter, "work"); work.Status.Code != codes.Error || len(work.Events) == 0 {
		t.Errorf("work span status = %v events = %d, want a recorded error", work.Status.Code, len(work.Events))
	}

	ctx, span = Start(context.Background(), "panicking")
	RecordPanic(ctx, "nil map", []byte("goroutine 1"))
	span.End()
	panicked := spanNamed(t, exporter, "panicking")
	if panicked.Status.Code != codes.Error || panicked.Events[0].Name != "exception" {
		t.Errorf("panic span = %+v", panicked.Status)
	}
}

func TestBeginRecordsTheOutcome(t *testing.T) {
	exporter := record(t)
	operation := func(name string, result error) {
		_, end := Begin(context.Background(), name, attribute.Int64("tenant.id", 7))
		err := result
		end(&err)
	}
	operation("ok", nil)
	operation("refused", apperr.New(apperr.ResourceNotFound, "Branch not found."))
	operation("broken", errors.New("connection reset"))

	ok := spanNamed(t, exporter, "ok")
	if ok.Status.Code == codes.Error || len(ok.Events) != 0 {
		t.Errorf("ok span = %v with %d events, want unset and none", ok.Status.Code, len(ok.Events))
	}
	if id, _ := attr(ok, "tenant.id"); id != "7" {
		t.Errorf("tenant.id = %q, want 7", id)
	}
	refused := spanNamed(t, exporter, "refused")
	if kind, _ := attr(refused, "error.type"); kind != "RESOURCE_NOT_FOUND" || refused.Status.Code == codes.Error || len(refused.Events) != 1 {
		t.Errorf("refused span: error.type %q status %v events %d, want the code recorded without failing", kind, refused.Status.Code, len(refused.Events))
	}
	broken := spanNamed(t, exporter, "broken")
	if kind, _ := attr(broken, "error.type"); kind != "INTERNAL_SERVER_ERROR" || broken.Status.Code != codes.Error {
		t.Errorf("broken span: error.type %q status %v, want a failed span", kind, broken.Status.Code)
	}
}

func TestAnnotateAddsToTheCurrentSpan(t *testing.T) {
	exporter := record(t)
	ctx, end := Begin(context.Background(), "create")
	Annotate(ctx, attribute.Int64("branch.id", 42))
	end(nil)
	if id, _ := attr(spanNamed(t, exporter, "create"), "branch.id"); id != "42" {
		t.Errorf("branch.id = %q, want 42", id)
	}
}

func TestSetupDisabledIsNoop(t *testing.T) {
	shutdown, err := Setup(context.Background(), Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown = %v", err)
	}
	if TraceID(context.Background()) != "" {
		t.Error("TraceID without a span should be empty")
	}
}

// A flush from an inner wrapper must reach the real writer through the span's
// recorder (the repo's wrappers find the next flusher by type assertion).
func TestMiddlewareForwardsFlush(t *testing.T) {
	record(t)
	handler := Middleware(nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("the span's response writer is not an http.Flusher")
		}
		_, _ = w.Write([]byte("chunk"))
		flusher.Flush()
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream", nil))
	if !rec.Flushed {
		t.Error("the flush did not reach the underlying writer")
	}
}
