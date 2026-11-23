package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
)

func TestProblemFromValidationError(t *testing.T) {
	builder := NewProblemBuilder("https://api.test", "local", false)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)

	problem := builder.FromError(req, apperr.Validation(map[string][]string{
		"customer_id": {"The customer id field is required."},
	}))

	if problem.Status != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", problem.Status)
	}
	if problem.Type != "https://api.test/errors/validation-failed" {
		t.Errorf("type = %q", problem.Type)
	}
	if problem.Title != "Unprocessable Entity" {
		t.Errorf("title = %q", problem.Title)
	}
	if problem.Detail != "The given data was invalid." {
		t.Errorf("detail = %q", problem.Detail)
	}
	if problem.ErrorCode != string(apperr.ValidationFailed) {
		t.Errorf("error_code = %q", problem.ErrorCode)
	}
	if len(problem.Errors["customer_id"]) != 1 {
		t.Errorf("errors = %v", problem.Errors)
	}
	if problem.Instance != "/api/v1/orders" {
		t.Errorf("instance = %q", problem.Instance)
	}
}

func TestProblemMasksInternalInProduction(t *testing.T) {
	builder := NewProblemBuilder("https://api.test", "production", false)
	problem := builder.FromError(nil, apperr.Wrap(apperr.InternalServerError, "database exploded", nil))

	if problem.Detail != "An unexpected error occurred." {
		t.Errorf("detail = %q, want masked", problem.Detail)
	}
	if problem.TraceID == "" {
		t.Error("trace_id is empty, want generated for 5xx")
	}
}

func TestProblemKeepsDetailInDebug(t *testing.T) {
	builder := NewProblemBuilder("https://api.test", "local", true)
	problem := builder.FromError(nil, apperr.Wrap(apperr.InternalServerError, "database exploded", nil))

	if problem.Detail != "database exploded" {
		t.Errorf("detail = %q, want unmasked in debug", problem.Detail)
	}
}

func TestProblemWriteSetsContentTypeAndStatus(t *testing.T) {
	builder := NewProblemBuilder("https://api.test", "local", false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil)

	builder.WriteJSON(rec, builder.FromError(req, apperr.New(apperr.ResourceNotFound, "The requested resource was not found.")))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
		t.Errorf("content type = %q", ct)
	}
	var decoded Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if decoded.ErrorCode != string(apperr.ResourceNotFound) {
		t.Errorf("error_code = %q", decoded.ErrorCode)
	}
}

func TestProblemFromPanicIncludesDebugOutsideProduction(t *testing.T) {
	builder := NewProblemBuilder("https://api.test", "local", true)
	problem := builder.FromPanic(nil, "boom", []byte("goroutine 1 [running]:"))

	if problem.Status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", problem.Status)
	}
	if problem.Debug == nil {
		t.Fatal("debug block missing")
	}
	if !strings.Contains(problem.Debug.Exception, "boom") {
		t.Errorf("exception = %q", problem.Debug.Exception)
	}
}

func TestProblemPanicHidesDebugInProduction(t *testing.T) {
	builder := NewProblemBuilder("https://api.test", "production", true)
	problem := builder.FromPanic(nil, "boom", []byte("stack"))

	if problem.Debug != nil {
		t.Error("debug block present in production")
	}
}
