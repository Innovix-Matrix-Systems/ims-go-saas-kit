package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func clientFS() http.FileSystem {
	return http.FS(fstest.MapFS{
		"index.html":      {Data: []byte("<!doctype html><title>app</title>")},
		"assets/app-1.js": {Data: []byte("console.log(1)")},
		"appearance.js":   {Data: []byte("/* boot */")},
		"favicon.svg":     {Data: []byte("<svg></svg>")},
	})
}

func TestClientServesShellWithCSP(t *testing.T) {
	h := NewClient(clientFS())
	for _, path := range []string{"/", "/login", "/accept-invitation", "/org/members/7", "/index.html"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>app</title>") {
			t.Fatalf("GET %s = %d %q, want the shell", path, rec.Code, rec.Body.String())
		}
		if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
			t.Fatalf("GET %s CSP = %q", path, csp)
		}
	}
}

func TestClientServesAssetsWithCaching(t *testing.T) {
	h := NewClient(clientFS())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app-1.js", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset = %d cache %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing asset = %d, want 404", rec.Code)
	}
}

func TestClientMissingBuildShowsSetupPage(t *testing.T) {
	h := NewClient(http.FS(fstest.MapFS{".gitkeep": {Data: []byte("")}}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "make app-build") {
		t.Fatalf("setup page = %d %q", rec.Code, rec.Body.String())
	}
}

func TestOwnsPath(t *testing.T) {
	owned := []string{"/", "/login", "/org/members", "/apiary", "/devices"}
	reserved := []string{"/api", "/api/v1/x", "/dev", "/dev/requests", "/docs", "/storage/a.png", "/mcp", "/oauth/token", "/.well-known/x", "/metrics", "/up"}
	for _, path := range owned {
		if !OwnsPath(path) {
			t.Errorf("OwnsPath(%q) = false, want true", path)
		}
	}
	for _, path := range reserved {
		if OwnsPath(path) {
			t.Errorf("OwnsPath(%q) = true, want false", path)
		}
	}
}
