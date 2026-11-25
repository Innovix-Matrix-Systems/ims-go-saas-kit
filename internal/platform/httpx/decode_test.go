package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
)

type bindTarget struct {
	Name string `json:"name"`
}

func newBindRequest(body, contentType string) (*http.Request, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req, httptest.NewRecorder()
}

func TestBindJSONValid(t *testing.T) {
	req, rec := newBindRequest(`{"name":"ok"}`, "application/json")
	var dst bindTarget
	if err := NewBinder(1024, true).JSON(rec, req, &dst); err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	if dst.Name != "ok" {
		t.Errorf("dst.Name = %q, want ok", dst.Name)
	}
}

func TestBindJSONMalformed(t *testing.T) {
	req, rec := newBindRequest(`{"name":}`, "application/json")
	var dst bindTarget
	if err := NewBinder(1024, true).JSON(rec, req, &dst); !apperr.Is(err, apperr.MalformedJSON) {
		t.Fatalf("JSON() error = %v, want MALFORMED_JSON", err)
	}
}

func TestBindJSONTrailingDocument(t *testing.T) {
	req, rec := newBindRequest(`{"name":"a"}{"name":"b"}`, "application/json")
	var dst bindTarget
	if err := NewBinder(1024, true).JSON(rec, req, &dst); !apperr.Is(err, apperr.MalformedJSON) {
		t.Fatalf("JSON() error = %v, want MALFORMED_JSON", err)
	}
}

func TestBindJSONUnknownField(t *testing.T) {
	req, rec := newBindRequest(`{"name":"a","extra":1}`, "application/json")
	var dst bindTarget
	err := NewBinder(1024, true).JSON(rec, req, &dst)
	if !apperr.Is(err, apperr.ValidationFailed) {
		t.Fatalf("JSON() error = %v, want VALIDATION_FAILED", err)
	}
	var appErr *apperr.Error
	if !asAppError(err, &appErr) {
		t.Fatalf("error is not *apperr.Error: %v", err)
	}
	if _, ok := appErr.Fields["extra"]; !ok {
		t.Errorf("Fields = %v, want key %q", appErr.Fields, "extra")
	}
}

func TestBindJSONUnknownFieldAllowedWhenLenient(t *testing.T) {
	req, rec := newBindRequest(`{"name":"a","extra":1}`, "application/json")
	var dst bindTarget
	if err := NewBinder(1024, false).JSON(rec, req, &dst); err != nil {
		t.Fatalf("JSON() lenient error = %v, want nil", err)
	}
}

func TestBindJSONTooLarge(t *testing.T) {
	body := `{"name":"` + strings.Repeat("x", 100) + `"}`
	req, rec := newBindRequest(body, "application/json")
	var dst bindTarget
	if err := NewBinder(16, true).JSON(rec, req, &dst); !apperr.Is(err, apperr.RequestEntityTooLarge) {
		t.Fatalf("JSON() error = %v, want REQUEST_ENTITY_TOO_LARGE", err)
	}
}

func TestBindJSONUnsupportedMediaType(t *testing.T) {
	req, rec := newBindRequest(`name=ok`, "text/plain")
	var dst bindTarget
	if err := NewBinder(1024, true).JSON(rec, req, &dst); !apperr.Is(err, apperr.UnsupportedMediaType) {
		t.Fatalf("JSON() error = %v, want UNSUPPORTED_MEDIA_TYPE", err)
	}
}

func TestBindJSONEmptyBody(t *testing.T) {
	req, rec := newBindRequest("", "application/json")
	var dst bindTarget
	if err := NewBinder(1024, true).JSON(rec, req, &dst); !apperr.Is(err, apperr.MalformedRequest) {
		t.Fatalf("JSON() error = %v, want MALFORMED_REQUEST", err)
	}
}

func asAppError(err error, target **apperr.Error) bool {
	for err != nil {
		if ae, ok := err.(*apperr.Error); ok {
			*target = ae
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}
