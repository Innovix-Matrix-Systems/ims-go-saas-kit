package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/i18n"
)

// Problem is an RFC 9457 problem+json payload rendered for every API error.
// The errors extension carries validation field errors; trace_id and
// debug are only populated for 5xx / non-production debugging.
type Problem struct {
	Type       string              `json:"type"`
	Title      string              `json:"title"`
	Status     int                 `json:"status"`
	Detail     string              `json:"detail,omitempty"`
	Instance   string              `json:"instance,omitempty"`
	ErrorCode  string              `json:"error_code,omitempty"`
	Timestamp  string              `json:"timestamp,omitempty"`
	TraceID    string              `json:"trace_id,omitempty"`
	RetryAfter int                 `json:"retry_after,omitempty"`
	Errors     map[string][]string `json:"errors,omitempty"`
	Meta       map[string]any      `json:"meta,omitempty"`
	Debug      *Debug              `json:"debug,omitempty"`
}

// Debug carries panic diagnostics and is only emitted outside production with
// debugging enabled.
type Debug struct {
	Exception string   `json:"exception"`
	Stack     []string `json:"stack,omitempty"`
}

// ProblemBuilder renders application errors as RFC 9457 problems.
type ProblemBuilder struct {
	BaseURL string
	Env     string
	Debug   bool
}

// NewProblemBuilder creates a problem builder for the given deployment.
func NewProblemBuilder(baseURL, env string, debug bool) ProblemBuilder {
	return ProblemBuilder{BaseURL: baseURL, Env: env, Debug: debug}
}

// FromError maps any error to a Problem. Non-application errors become masked
// 500 responses.
func (b ProblemBuilder) FromError(r *http.Request, err error) Problem {
	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		appErr = apperr.Wrap(apperr.InternalServerError, "An unexpected error occurred.", err)
	}

	status := appErr.Status
	if status == 0 {
		status = apperr.StatusFor(appErr.Code)
	}

	var ctx context.Context
	if r != nil {
		ctx = r.Context()
	}

	p := Problem{
		Type:      b.typeURL(appErr.Code),
		Title:     titleFor(ctx, appErr.Code, status),
		Status:    status,
		ErrorCode: string(appErr.Code),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if r != nil {
		p.Instance = r.URL.Path
	}

	detail := appErr.Message
	if appErr.MessageKey != "" {
		detail = i18n.TOr(ctx, appErr.MessageKey, appErr.Message, appErr.Params)
	}
	if status >= 500 {
		p.TraceID = traceFromRequest(r)
		// The maintenance message is written for API clients (`ims down
		// --message`); every other 5xx detail may leak internals.
		if !b.debugAllowed() && appErr.Code != apperr.MaintenanceMode {
			detail = "An unexpected error occurred."
		}
	}
	p.Detail = detail
	if len(appErr.Fields) > 0 {
		p.Errors = appErr.Fields
	}
	if appErr.RetryAfter > 0 {
		p.RetryAfter = appErr.RetryAfter
	}
	if len(appErr.Meta) > 0 {
		p.Meta = appErr.Meta
	}
	return p
}

// FromPanic renders a recovered panic as an internal-error Problem, including
// a debug block outside production when debugging is enabled.
func (b ProblemBuilder) FromPanic(r *http.Request, panicValue any, stack []byte) Problem {
	var ctx context.Context
	if r != nil {
		ctx = r.Context()
	}
	p := Problem{
		Type:      b.typeURL(apperr.InternalServerError),
		Title:     titleFor(ctx, apperr.InternalServerError, http.StatusInternalServerError),
		Status:    http.StatusInternalServerError,
		Detail:    "An unexpected error occurred.",
		ErrorCode: string(apperr.InternalServerError),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		TraceID:   traceFromRequest(r),
	}
	if r != nil {
		p.Instance = r.URL.Path
	}
	if b.debugAllowed() {
		lines := strings.Split(string(stack), "\n")
		if len(lines) > 20 {
			lines = lines[:20]
		}
		p.Debug = &Debug{
			Exception: fmt.Sprintf("%T: %v", panicValue, panicValue),
			Stack:     lines,
		}
	}
	return p
}

// WriteJSON serializes a Problem with the correct content type and status.
func (b ProblemBuilder) WriteJSON(w http.ResponseWriter, p Problem) {
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

func (b ProblemBuilder) debugAllowed() bool {
	return b.Debug && !strings.EqualFold(b.Env, "production")
}

func (b ProblemBuilder) typeURL(code apperr.Code) string {
	base := strings.TrimRight(b.BaseURL, "/")
	if base == "" {
		base = "about:blank"
		return base
	}
	return base + "/errors/" + slug(string(code))
}

func titleFor(ctx context.Context, code apperr.Code, status int) string {
	return i18n.TOr(ctx, "http-statuses."+strconv.Itoa(status), fallbackTitle(code, status), nil)
}

func fallbackTitle(code apperr.Code, status int) string {
	switch code {
	case apperr.ValidationFailed:
		return "Validation Failed"
	case apperr.BusinessLogicError:
		return "Business Logic Validation Failed"
	case apperr.TenantContextNotResolved:
		return "Tenant Context Not Resolved"
	}
	if text := http.StatusText(status); text != "" {
		return text
	}
	return "Error"
}

func slug(raw string) string {
	return strings.ToLower(strings.ReplaceAll(raw, "_", "-"))
}

func traceFromRequest(r *http.Request) string {
	if r != nil {
		if id := RequestIDFromContext(r.Context()); id != "" {
			return id
		}
	}
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("err_%d", time.Now().UnixNano())
	}
	return "err_" + hex.EncodeToString(buf)
}
