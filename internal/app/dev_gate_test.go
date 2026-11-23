package app

import (
	"net/http"
	"strings"
	"testing"
)

func TestDevDashboardDisabledReturnsNotFound(t *testing.T) {
	t.Setenv("DEV_DASHBOARD_ENABLED", "false")
	router := newTestRouter(t)

	for _, path := range []string{"/dev", "/dev/api/me", "/dev/requests"} {
		rec := doRequest(t, router, http.MethodGet, path, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404 (body %s)", path, rec.Code, rec.Body.String())
		}
	}
}

func TestDevDashboardServesSPA(t *testing.T) {
	router := newTestRouter(t)

	rec := doRequest(t, router, http.MethodGet, "/dev", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("content-type = %q, want text/html", contentType)
	}
}

func TestDevDashboardRequiresAuthentication(t *testing.T) {
	router := newTestRouter(t)

	rec := doRequest(t, router, http.MethodGet, "/dev/api/me", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
}
