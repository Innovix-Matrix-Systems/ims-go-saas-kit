package app

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	api "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/api"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/httpx"
)

// loadSpec parses and validates the embedded canonical OpenAPI document.
func loadSpec(t *testing.T) *openapi3.T {
	t.Helper()
	doc, err := ValidateSpec(api.Spec)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return doc
}

// TestOpenAPISpecIsValid guards the contract: the embedded document must parse
// and pass kin-openapi's structural validation.
func TestOpenAPISpecIsValid(t *testing.T) {
	doc := loadSpec(t)
	if len(doc.Paths.Map()) == 0 {
		t.Fatal("spec has no paths")
	}
	if doc.Components == nil || doc.Components.Schemas["Problem"] == nil {
		t.Fatal("spec is missing the Problem schema")
	}
}

// TestSpecMatchesRoutes asserts there is no drift between the canonical spec
// and the live chi route table (paths and methods, both directions).
func TestSpecMatchesRoutes(t *testing.T) {
	t.Setenv("METRICS_ENABLED", "true")
	t.Setenv("DOCS_ENABLED", "true")
	doc := loadSpec(t)

	mismatches, err := RouteDrift(newTestRouter(t), doc)
	if err != nil {
		t.Fatalf("%v", err)
	}
	for _, m := range mismatches {
		if m.In == "spec" {
			t.Errorf("spec declares %q %q but no matching route is registered", m.Method, m.Path)
		} else {
			t.Errorf("route %q %q is registered but missing from the OpenAPI spec", m.Method, m.Path)
		}
	}
}

// TestResponsesValidateAgainstSpec runs a representative set of live requests
// and validates each response body against the schema declared in the spec,
// asserting the correct content type on success and problem responses.
func TestResponsesValidateAgainstSpec(t *testing.T) {
	doc := loadSpec(t)
	router := newTestRouter(t)

	cases := []struct {
		name        string
		method      string
		path        string
		body        string
		status      int
		schema      string
		contentType string
	}{
		{"system info", http.MethodGet, "/api/v1/system/info", "", http.StatusOK, "SystemInfoEnvelope", "application/json"},
		{"system echo", http.MethodPost, "/api/v1/system/echo", `{"message":"hello"}`, http.StatusOK, "EchoEnvelope", "application/json"},
		{"echo validation", http.MethodPost, "/api/v1/system/echo", `{"message":"x"}`, http.StatusUnprocessableEntity, "Problem", "application/problem+json"},
		{"unauthenticated", http.MethodGet, "/api/v1/user", "", http.StatusUnauthorized, "Problem", "application/problem+json"},
		{"not found", http.MethodGet, "/api/v1/nope", "", http.StatusNotFound, "Problem", "application/problem+json"},
		{"method not allowed", http.MethodDelete, "/api/v1/system/info", "", http.StatusMethodNotAllowed, "Problem", "application/problem+json"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doRequest(t, router, tc.method, tc.path, tc.body)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tc.status, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, tc.contentType) {
				t.Errorf("content-type = %q, want %q", ct, tc.contentType)
			}
			var payload any
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			ref := doc.Components.Schemas[tc.schema]
			if ref == nil || ref.Value == nil {
				t.Fatalf("spec is missing schema %q", tc.schema)
			}
			if err := ref.Value.VisitJSON(payload); err != nil {
				t.Errorf("response does not match %s: %v\nbody: %s", tc.schema, err, rec.Body.String())
			}
		})
	}
}

// TestPanicIsMaskedOutsideDebug asserts that recovered panics never leak
// internals in production: the detail is generic, a trace_id is present, and
// no debug block or panic text is emitted.
func TestPanicIsMaskedOutsideDebug(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	responder := httpx.NewResponder("https://api.example.test", "production", false)

	handler := httpx.RequestID(httpx.Recover(logger, responder)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("database password is hunter2")
	})))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
		t.Errorf("content-type = %q", ct)
	}
	body := rec.Body.String()
	if strings.Contains(body, "hunter2") {
		t.Errorf("panic detail leaked: %s", body)
	}
	var problem struct {
		Detail  string `json:"detail"`
		TraceID string `json:"trace_id"`
		Debug   any    `json:"debug"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if problem.TraceID == "" {
		t.Error("trace_id is empty")
	}
	if problem.Debug != nil {
		t.Error("debug block present outside debug")
	}
	if problem.Detail != "An unexpected error occurred." {
		t.Errorf("detail = %q", problem.Detail)
	}
}
