package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocsServedWhenEnabled(t *testing.T) {
	t.Setenv("DOCS_ENABLED", "true")
	router := newTestRouter(t)

	ui := doRequest(t, router, http.MethodGet, "/docs", "")
	if ui.Code != http.StatusOK {
		t.Fatalf("GET /docs status = %d", ui.Code)
	}
	if ct := ui.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("GET /docs content-type = %q", ct)
	}

	spec := doRequest(t, router, http.MethodGet, "/docs/openapi.yaml", "")
	if spec.Code != http.StatusOK {
		t.Fatalf("GET /docs/openapi.yaml status = %d", spec.Code)
	}
	if !strings.HasPrefix(spec.Body.String(), "openapi:") {
		t.Errorf("spec does not look like OpenAPI YAML: %.40q", spec.Body.String())
	}
}

func TestDocsDisabledReturnsNotFound(t *testing.T) {
	t.Setenv("DOCS_ENABLED", "false")
	router := newTestRouter(t)

	rec := doRequest(t, router, http.MethodGet, "/docs", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
		t.Errorf("content-type = %q", ct)
	}
}

func TestMetricsDisabledByDefault(t *testing.T) {
	router := newTestRouter(t)
	rec := doRequest(t, router, http.MethodGet, "/metrics", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestMetricsExposedWhenEnabled(t *testing.T) {
	t.Setenv("METRICS_ENABLED", "true")
	t.Setenv("METRICS_TOKEN", "scrape-secret")
	router := newTestRouter(t)

	doRequest(t, router, http.MethodGet, "/up", "")

	// Without the token (and not from loopback) /metrics stays hidden.
	if rec := doRequest(t, router, http.MethodGet, "/metrics", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("anonymous status = %d, want 404", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer scrape-secret")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Errorf("content-type = %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "ims_http_requests_total") {
		t.Errorf("metrics body missing request counter:\n%s", body)
	}
	if !strings.Contains(body, `route="/up"`) {
		t.Errorf("metrics body missing /up route label:\n%s", body)
	}
}

func TestSecurityHeadersApplied(t *testing.T) {
	router := newTestRouter(t)
	rec := doRequest(t, router, http.MethodGet, "/up", "")
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q", got)
	}
}

func TestCORSDisabledByDefault(t *testing.T) {
	router := newTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/up", nil)
	req.Header.Set("Origin", "https://app.example.test")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("unexpected CORS header when disabled: %q", got)
	}
}

func TestCORSAllowedOriginPreflight(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.test")
	router := newTestRouter(t)

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/system/info", nil)
	req.Header.Set("Origin", "https://app.example.test")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.test" {
		t.Errorf("Access-Control-Allow-Origin = %q", got)
	}

	// A disallowed origin is rejected.
	bad := httptest.NewRequest(http.MethodOptions, "/api/v1/system/info", nil)
	bad.Header.Set("Origin", "https://evil.example.test")
	bad.Header.Set("Access-Control-Request-Method", http.MethodGet)
	badRec := httptest.NewRecorder()
	router.ServeHTTP(badRec, bad)
	if badRec.Code != http.StatusForbidden {
		t.Errorf("disallowed preflight status = %d, want 403", badRec.Code)
	}
}
