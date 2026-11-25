// Package tracing is the application's OpenTelemetry tracing:
// vendor-neutral spans exported over OTLP to whatever the team runs —
// Grafana Tempo, Jaeger, Honeycomb, Datadog, New Relic or any OTLP backend.
//
// It is off unless OTEL_EXPORTER_OTLP_ENDPOINT (or OTEL_ENABLED=true) is
// set; until then every span is a no-op and costs nothing. The standard
// OTEL_* variables apply (OTEL_SERVICE_NAME, OTEL_TRACES_SAMPLER,
// OTEL_EXPORTER_OTLP_HEADERS, OTEL_RESOURCE_ATTRIBUTES, …).
//
// What is traced without any module code: every HTTP request (named by its
// route), SQL queries, queue jobs (linked to the request that queued them)
// and outbound HTTP (webhooks, SMS). Errors — panics, 5xx answers, failed
// jobs — mark their span and carry the stack. A module adds spans around
// its own steps with Begin (every module service does; see branches).
package tracing

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
)

// instrumentation names the tracer every span of the application uses.
const instrumentation = "github.com/Innovix-Matrix-Systems/ims-go-saas-kit"

// Settings configure tracing.
type Settings struct {
	Enabled     bool
	ServiceName string
	Version     string
	Environment string
	// SampleRatio is the share of new traces the default parent-based
	// sampler keeps. Ignored when SamplerFromEnv (OTEL_TRACES_SAMPLER).
	SampleRatio    float64
	SamplerFromEnv bool
}

// Setup installs the OTLP exporter and the W3C propagator when tracing is
// enabled. The returned shutdown flushes buffered spans (call it on exit).
func Setup(ctx context.Context, settings Settings) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if !settings.Enabled {
		return func(context.Context) error { return nil }, nil
	}
	exporter, err := otlptracehttp.New(ctx) // endpoint, headers and TLS from OTEL_EXPORTER_OTLP_*
	if err != nil {
		return nil, fmt.Errorf("tracing: exporter: %w", err)
	}
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			attribute.String("service.name", settings.ServiceName),
			attribute.String("service.version", settings.Version),
			attribute.String("deployment.environment.name", settings.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("tracing: resource: %w", err)
	}
	options := []sdktrace.TracerProviderOption{sdktrace.WithBatcher(exporter), sdktrace.WithResource(res)}
	if !settings.SamplerFromEnv {
		// A code sampler would override OTEL_TRACES_SAMPLER, so it is set
		// only when that variable is not.
		options = append(options, sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(settings.SampleRatio))))
	}
	provider := sdktrace.NewTracerProvider(options...)
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}

// Start begins a span named name as a child of the span in ctx:
//
//	ctx, span := tracing.Start(ctx, "branches.create", attribute.Int64("tenant.id", tenantID))
//	defer span.End()
func Start(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return otel.Tracer(instrumentation).Start(ctx, name, trace.WithAttributes(attrs...))
}

// Begin starts a span around one service operation and returns the
// function that ends it. Pass that function the method's named error, so
// the outcome lands on the span:
//
//	func (s *Service) Create(ctx context.Context, scope tenancy.Scope, ...) (_ *Branch, err error) {
//		ctx, end := tracing.Begin(ctx, "branches.create", attribute.Int64("tenant.id", scope.EffectiveTenantID()))
//		defer end(&err)
//
// A returned error sets error.type to its apperr code. A failure (a 5xx
// code, or any error that is not an apperr) also marks the span failed with
// the stack; a refusal (validation, not found, forbidden) is recorded
// without failing the span, the way a 4xx answer does not fail a request.
func Begin(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, func(*error)) {
	ctx, span := Start(ctx, name, attrs...)
	return ctx, func(errp *error) {
		if errp != nil && *errp != nil {
			err := *errp
			code := apperr.CodeOf(err)
			span.SetAttributes(attribute.String("error.type", string(code)))
			if apperr.StatusFor(code) >= http.StatusInternalServerError {
				span.RecordError(err, trace.WithStackTrace(true))
				span.SetStatus(codes.Error, err.Error())
			} else {
				span.RecordError(err)
			}
		}
		span.End()
	}
}

// Annotate adds attributes to the span in ctx, for what is only known part
// way through an operation (the id of the row it created, a count).
func Annotate(ctx context.Context, attrs ...attribute.KeyValue) {
	trace.SpanFromContext(ctx).SetAttributes(attrs...)
}

// Fail marks the span in ctx failed with err and returns err, so a service
// can write `return tracing.Fail(ctx, err)`.
func Fail(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	span := trace.SpanFromContext(ctx)
	span.RecordError(err, trace.WithStackTrace(true))
	span.SetStatus(codes.Error, err.Error())
	return err
}

// RecordPanic marks the span in ctx failed by a panic, with its stack.
func RecordPanic(ctx context.Context, value any, stack []byte) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	if stack == nil {
		stack = debug.Stack()
	}
	span.AddEvent("exception", trace.WithAttributes(
		attribute.String("exception.type", "panic"),
		attribute.String("exception.message", fmt.Sprint(value)),
		attribute.String("exception.stacktrace", string(stack)),
	))
	span.SetStatus(codes.Error, fmt.Sprint(value))
}

// TraceID returns the trace id in ctx ("" without a sampled span), for log
// lines and error reports.
func TraceID(ctx context.Context) string {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return ""
	}
	return spanContext.TraceID().String()
}

// Inject writes the trace context of ctx into a carrier (a queued job's
// envelope), so the job's span links to the request that queued it.
func Inject(ctx context.Context) map[string]string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if len(carrier) == 0 {
		return nil
	}
	return carrier
}

// Extract returns ctx carrying the trace context of a carrier.
func Extract(ctx context.Context, carrier map[string]string) context.Context {
	if len(carrier) == 0 {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(carrier))
}
