package httpx

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/tracing"
)

type contextKey string

const requestIDKey contextKey = "request_id"

// RequestIDFromContext returns the request id stored by RequestID.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// WithRequestID stores a request id in the context.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID assigns or propagates an X-Request-ID header and stores it in the
// request context.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !validRequestID(id) {
			// Missing or garbage (too long, control characters, log
			// injection): use our own id instead of echoing it (M24).
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(WithRequestID(r.Context(), id)))
	})
}

// validRequestID accepts 1–128 characters of [A-Za-z0-9._:-].
func validRequestID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == ':', c == '-':
		default:
			return false
		}
	}
	return true
}

// ResponseRecorder captures status and byte counts for logging and recover.
type ResponseRecorder struct {
	http.ResponseWriter
	Status int
	Bytes  int64
	wrote  bool
}

// WriteHeader records the status code and forwards it once.
func (r *ResponseRecorder) WriteHeader(code int) {
	if r.wrote {
		return
	}
	r.wrote = true
	r.Status = code
	r.ResponseWriter.WriteHeader(code)
}

// Write records the byte count and forwards the payload.
func (r *ResponseRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(b)
	r.Bytes += int64(n)
	return n, err
}

// WroteHeader reports whether a status has already been sent.
func (r *ResponseRecorder) WroteHeader() bool {
	return r.wrote
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (r *ResponseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// Flush forwards flushes when supported.
func (r *ResponseRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Recover converts panics into masked 500 problem responses and logs them with
// the full stack.
func Recover(logger *slog.Logger, rsp *Responder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				panicValue := recover()
				if panicValue == nil {
					return
				}
				stack := debug.Stack()
				tracing.RecordPanic(r.Context(), panicValue, stack)
				logger.Error("panic recovered",
					"request_id", RequestIDFromContext(r.Context()),
					"method", r.Method,
					"path", r.URL.Path,
					"panic", fmt.Sprint(panicValue),
					"stack", string(stack),
				)
				if rec, ok := w.(*ResponseRecorder); ok && rec.WroteHeader() {
					// Part of the response is already on the wire; abort the
					// connection so the client sees a broken response rather
					// than a truncated "success" (M23).
					panic(http.ErrAbortHandler)
				}
				rsp.Panic(w, r, panicValue, stack)
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// AccessLog logs every request with method, route, status and duration.
func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &ResponseRecorder{ResponseWriter: w, Status: http.StatusOK}
			start := time.Now()
			ctx, info := WithRequestInfo(r.Context())
			r = r.WithContext(ctx)
			next.ServeHTTP(rec, r)
			attrs := []any{
				"request_id", RequestIDFromContext(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.Status,
				"bytes", rec.Bytes,
				"duration_ms", float64(time.Since(start).Microseconds()) / 1000.0,
				"ip", r.RemoteAddr,
			}
			if routeCtx := chi.RouteContext(r.Context()); routeCtx != nil && routeCtx.RoutePattern() != "" {
				attrs = append(attrs, "route", routeCtx.RoutePattern())
			}
			if info.UserID != 0 {
				attrs = append(attrs, "user_id", info.UserID)
			}
			if info.ImpersonatorID != 0 {
				attrs = append(attrs, "impersonator_id", info.ImpersonatorID)
			}
			if info.TenantID != 0 {
				attrs = append(attrs, "tenant_id", info.TenantID)
			}
			if info.MCPTool != "" {
				attrs = append(attrs, "via", "mcp", "mcp_tool", info.MCPTool)
			}
			switch {
			case rec.Status >= 500:
				logger.ErrorContext(r.Context(), "http request", attrs...)
			case rec.Status >= 400:
				logger.WarnContext(r.Context(), "http request", attrs...)
			default:
				logger.InfoContext(r.Context(), "http request", attrs...)
			}
		})
	}
}

// Timeout bounds handler execution with a context deadline and writes a 503
// problem when the deadline elapses without a response.
func Timeout(d time.Duration, rsp *Responder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if d <= 0 {
				next.ServeHTTP(w, r)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
			if ctx.Err() == context.DeadlineExceeded {
				if tracker, ok := w.(interface{ WroteHeader() bool }); ok && tracker.WroteHeader() {
					return
				}
				rsp.Error(w, r, apperr.New(apperr.ServiceUnavailable, "The request exceeded the processing deadline."))
			}
		})
	}
}

// SecurityHeaders applies baseline hardening headers to every response. HSTS
// is only emitted over HTTPS (directly or behind a TLS-terminating proxy).
// A Content-Security-Policy is intentionally omitted so the API docs UI can
// load its CDN assets.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Permitted-Cross-Domain-Policies", "none")
		if requestScheme(r) == "https" {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// CORS applies the configured cross-origin policy. With no allowed origins it
// is a no-op and emits no CORS headers. Preflight OPTIONS requests are
// answered directly when the origin is allowed.
func CORS(origins, methods, headers []string, maxAge time.Duration) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(origins))
	allowAll := false
	for _, o := range origins {
		if o == "*" {
			allowAll = true
		}
		allowed[strings.ToLower(o)] = struct{}{}
	}
	methodsHeader := strings.Join(methods, ", ")
	headersHeader := strings.Join(headers, ", ")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(origins) == 0 {
				next.ServeHTTP(w, r)
				return
			}
			origin := r.Header.Get("Origin")
			allowedOrigin := ""
			if origin != "" {
				if allowAll {
					allowedOrigin = "*"
				} else if _, ok := allowed[strings.ToLower(origin)]; ok {
					allowedOrigin = origin
				}
			}
			preflight := r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""
			if allowedOrigin == "" {
				if preflight {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			h := w.Header()
			h.Set("Access-Control-Allow-Origin", allowedOrigin)
			h.Add("Vary", "Origin")
			if allowedOrigin != "*" {
				h.Set("Access-Control-Allow-Credentials", "true")
			}

			if preflight {
				if methodsHeader != "" {
					h.Set("Access-Control-Allow-Methods", methodsHeader)
				}
				if headersHeader != "" {
					h.Set("Access-Control-Allow-Headers", headersHeader)
				}
				if maxAge > 0 {
					h.Set("Access-Control-Max-Age", strconv.Itoa(int(maxAge.Seconds())))
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func newRequestID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("req_%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// IsLoopback reports whether the request comes from this machine (after
// RealIP has applied trusted proxy headers).
func IsLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ProtectOps guards an operational endpoint (e.g. /metrics): with a token
// it requires "Authorization: Bearer <token>"; without one it only answers
// loopback callers. Anything else gets a plain 404 (the surface stays hidden).
func ProtectOps(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" {
			given := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(given), []byte(token)) == 1 {
				next.ServeHTTP(w, r)
				return
			}
		} else if IsLoopback(r) {
			next.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})
}
