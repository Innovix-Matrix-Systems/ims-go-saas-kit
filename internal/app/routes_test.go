package app

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
)

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	t.Setenv("APP_ENV_FILE", "")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps, cleanup, err := NewDependencies(cfg, logger)
	if err != nil {
		t.Fatalf("NewDependencies() error = %v", err)
	}
	t.Cleanup(cleanup)
	return deps.Router()
}

func doRequest(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestSystemInfoEndpoint(t *testing.T) {
	rec := doRequest(t, newTestRouter(t), http.MethodGet, "/api/v1/system/info", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Data["name"] == "" || body.Data["version"] == "" {
		t.Errorf("data = %v", body.Data)
	}
}

func TestEchoEndpointValid(t *testing.T) {
	rec := doRequest(t, newTestRouter(t), http.MethodPost, "/api/v1/system/echo", `{"message":"  hello  "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			Message string `json:"message"`
			Length  int    `json:"length"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Data.Message != "hello" || body.Data.Length != 5 {
		t.Errorf("data = %+v", body.Data)
	}
}

func TestEchoEndpointValidationFailure(t *testing.T) {
	rec := doRequest(t, newTestRouter(t), http.MethodPost, "/api/v1/system/echo", `{"message":"x"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
		t.Errorf("content type = %q", ct)
	}
	var body struct {
		ErrorCode string              `json:"error_code"`
		Errors    map[string][]string `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ErrorCode != "VALIDATION_FAILED" {
		t.Errorf("error_code = %q", body.ErrorCode)
	}
	if len(body.Errors["message"]) == 0 {
		t.Errorf("errors = %v", body.Errors)
	}
}

func TestUnknownRouteReturnsProblem(t *testing.T) {
	rec := doRequest(t, newTestRouter(t), http.MethodGet, "/api/v1/nope", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "RESOURCE_NOT_FOUND") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestMethodNotAllowedReturnsProblem(t *testing.T) {
	rec := doRequest(t, newTestRouter(t), http.MethodDelete, "/api/v1/system/info", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "METHOD_NOT_ALLOWED") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

// TestHealthEndpoint runs without a database in CI's build-test job, so the
// database check may fail: readiness then answers 503 (fail closed). Either
// way the status must agree with the body.
func TestHealthEndpoint(t *testing.T) {
	rec := doRequest(t, newTestRouter(t), http.MethodGet, "/api/health", "")
	if rec.Code != http.StatusOK && rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 200 or 503", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("cache-control = %q", cc)
	}
	var body struct {
		FinishedAt   int64 `json:"finishedAt"`
		CheckResults []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"checkResults"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.FinishedAt == 0 || len(body.CheckResults) == 0 {
		t.Errorf("body = %s", rec.Body.String())
	}
	failed := false
	for _, check := range body.CheckResults {
		failed = failed || check.Status == "failed" || check.Status == "crashed"
	}
	if rec.Code == http.StatusServiceUnavailable && !failed {
		t.Errorf("503 without a failed check: %s", rec.Body.String())
	}
}

func TestUpEndpoint(t *testing.T) {
	rec := doRequest(t, newTestRouter(t), http.MethodGet, "/up", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
