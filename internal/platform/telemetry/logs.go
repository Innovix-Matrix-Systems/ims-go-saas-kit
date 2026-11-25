package telemetry

import (
	"context"
	"log/slog"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/httpx"
)

// LogHandler wraps an slog.Handler and captures records at or above
// capturedLevel into telemetry when the request context is captured.
type LogHandler struct {
	inner     slog.Handler
	collector *Collector
}

const capturedLevel = slog.LevelWarn

// WrapLogHandler wraps inner when log capture is enabled.
func WrapLogHandler(inner slog.Handler, collector *Collector) slog.Handler {
	if collector == nil || !collector.capture.Enabled || !collector.capture.Capture.Logs {
		return inner
	}
	return &LogHandler{inner: inner, collector: collector}
}

// Enabled implements slog.Handler.
func (h *LogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle implements slog.Handler: capture first (never altering the record),
// then delegate.
func (h *LogHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Level >= capturedLevel && CaptureFromContext(ctx) {
		attributes := map[string]any{}
		record.Attrs(func(attr slog.Attr) bool {
			attributes[attr.Key] = attr.Value.Resolve().Any()
			return true
		})
		h.collector.Log(LogEntry{
			RequestID:  httpx.RequestIDFromContext(ctx),
			Level:      record.Level.String(),
			Message:    record.Message,
			Attributes: h.collector.redactor.AnyJSON(attributes),
			CreatedAt:  record.Time,
		})
	}
	return h.inner.Handle(ctx, record)
}

// WithAttrs implements slog.Handler.
func (h *LogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &LogHandler{inner: h.inner.WithAttrs(attrs), collector: h.collector}
}

// WithGroup implements slog.Handler.
func (h *LogHandler) WithGroup(name string) slog.Handler {
	return &LogHandler{inner: h.inner.WithGroup(name), collector: h.collector}
}
