package tracing

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// ── HTTP server ────────────────────────────────────────────────────────

// RequestInfo reports who a request ran as once it is done (user, tenant,
// impersonator), for span attributes. httpx provides it; the indirection
// keeps this package free of httpx.
type RequestInfo func(ctx context.Context) (userID, tenantID, impersonatorID int64)

// TrustParent reports whether a request's traceparent may be continued
// (httpx.FromTrustedProxy: it came through a trusted proxy).
type TrustParent func(r *http.Request) bool

// Middleware starts a server span per request and names it by route after
// routing: "POST /api/v1/branch". 5xx answers mark the span failed.
//
// A trace the caller sent (traceparent) is continued when trust allows it
// (nil trusts every caller). Otherwise the span starts a new trace linked to
// the caller's, so an outside client cannot choose what gets sampled.
func Middleware(info RequestInfo, trust TrustParent) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			options := []trace.SpanStartOption{
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("http.request.method", r.Method),
					attribute.String("url.path", r.URL.Path),
				),
			}
			if remote := trace.SpanContextFromContext(ctx); remote.IsValid() && trust != nil && !trust(r) {
				options = append(options, trace.WithNewRoot(), trace.WithLinks(trace.Link{SpanContext: remote}))
			}
			ctx, span := otel.Tracer(instrumentation).Start(ctx, r.Method, options...)
			defer span.End()
			if !span.IsRecording() {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(recorder, r.WithContext(ctx))

			route := r.URL.Path
			if routeContext := chi.RouteContext(r.Context()); routeContext != nil && routeContext.RoutePattern() != "" {
				route = routeContext.RoutePattern()
			}
			span.SetName(r.Method + " " + route)
			span.SetAttributes(
				attribute.String("http.route", route),
				attribute.Int("http.response.status_code", recorder.status),
			)
			if info != nil {
				user, tenant, impersonator := info(ctx)
				if user > 0 {
					span.SetAttributes(attribute.Int64("enduser.id", user))
				}
				if tenant > 0 {
					span.SetAttributes(attribute.Int64("tenant.id", tenant))
				}
				if impersonator > 0 {
					span.SetAttributes(attribute.Int64("impersonator.id", impersonator))
				}
			}
			if recorder.status >= 500 {
				span.SetStatus(codes.Error, http.StatusText(recorder.status))
			}
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.wrote, s.status = true, code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wrote = true
	return s.ResponseWriter.Write(b)
}

// Flush forwards flushes when supported (inner wrappers reach the real
// writer through this one).
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// ── outbound HTTP ──────────────────────────────────────────────────────

// Transport wraps an HTTP transport with client spans that pass the trace
// on (traceparent), so the receiver can continue it.
func Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return roundTripper{base: base}
}

type roundTripper struct{ base http.RoundTripper }

func (t roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, span := otel.Tracer(instrumentation).Start(req.Context(), req.Method+" "+req.URL.Host,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("http.request.method", req.Method),
			attribute.String("url.scheme", req.URL.Scheme),
			// The host only: webhook URLs often carry their secret in the
			// path (Slack, Discord) and APIs in the query.
			attribute.String("server.address", req.URL.Hostname()),
		))
	defer span.End()
	req = req.Clone(ctx)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	res, err := t.base.RoundTrip(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(attribute.Int("http.response.status_code", res.StatusCode))
	if res.StatusCode >= 500 {
		span.SetStatus(codes.Error, res.Status)
	}
	return res, nil
}

// ── SQL ────────────────────────────────────────────────────────────────

// QueryTracer is a pgx tracer that records each query as a client span:
// the statement, never its arguments (they may hold personal data).
type QueryTracer struct{}

type querySpanKey struct{}

const maxStatement = 2000

// TraceQueryStart implements pgx.QueryTracer.
func (QueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !trace.SpanFromContext(ctx).SpanContext().IsValid() {
		return ctx // no trace to join: don't start one per query
	}
	statement := strings.TrimSpace(data.SQL)
	if len(statement) > maxStatement {
		statement = statement[:maxStatement]
	}
	ctx, span := otel.Tracer(instrumentation).Start(ctx, queryName(statement), trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system.name", "postgresql"),
			attribute.String("db.query.text", statement),
		))
	return context.WithValue(ctx, querySpanKey{}, span)
}

// TraceQueryEnd implements pgx.QueryTracer.
func (QueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span, ok := ctx.Value(querySpanKey{}).(trace.Span)
	if !ok {
		return
	}
	if data.Err != nil && data.Err != pgx.ErrNoRows {
		span.RecordError(data.Err)
		span.SetStatus(codes.Error, data.Err.Error())
	}
	span.End()
}

// queryName is the sqlc query name ("-- name: GetUser :one") or the verb.
func queryName(statement string) string {
	if rest, ok := strings.CutPrefix(statement, "-- name: "); ok {
		if name, _, found := strings.Cut(rest, " "); found {
			return "db " + name
		}
	}
	verb, _, _ := strings.Cut(statement, " ")
	return "db " + strings.ToUpper(verb)
}

// ── logs ───────────────────────────────────────────────────────────────

// LogHandler adds trace_id and span_id to every log line written inside a
// span, so a log search jumps to the trace and back.
func LogHandler(next slog.Handler) slog.Handler { return logHandler{next: next} }

type logHandler struct{ next slog.Handler }

func (h logHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h logHandler) Handle(ctx context.Context, record slog.Record) error {
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.IsValid() {
		record.AddAttrs(
			slog.String("trace_id", spanContext.TraceID().String()),
			slog.String("span_id", spanContext.SpanID().String()),
		)
	}
	return h.next.Handle(ctx, record)
}

func (h logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return logHandler{next: h.next.WithAttrs(attrs)}
}

func (h logHandler) WithGroup(name string) slog.Handler {
	return logHandler{next: h.next.WithGroup(name)}
}
