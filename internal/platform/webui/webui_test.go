package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func newTestHandler(files fstest.MapFS) http.Handler {
	return New(http.FS(files))
}

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":        &fstest.MapFile{Data: []byte("<!doctype html><html><body>shell</body></html>")},
		"assets/app-abc.js": &fstest.MapFile{Data: []byte("console.log('dev')")},
	}
}

func TestSPAFallbackServesShell(t *testing.T) {
	handler := newTestHandler(testFS())
	for _, path := range []string{"/dev", "/dev/", "/dev/requests", "/dev/requests/7"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, recorder.Code)
		}
		if !strings.Contains(recorder.Body.String(), "shell") {
			t.Errorf("%s did not serve the shell", path)
		}
		if cache := recorder.Header().Get("Cache-Control"); cache != "no-cache" {
			t.Errorf("%s cache-control = %q, want no-cache", path, cache)
		}
	}
}

func TestAssetsServedWithImmutableCache(t *testing.T) {
	handler := newTestHandler(testFS())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dev/assets/app-abc.js", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if cache := recorder.Header().Get("Cache-Control"); !strings.Contains(cache, "immutable") {
		t.Errorf("cache-control = %q", cache)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "javascript") {
		t.Errorf("content-type = %q", contentType)
	}
	if !strings.Contains(recorder.Body.String(), "console.log") {
		t.Errorf("asset body = %q", recorder.Body.String())
	}
}

func TestMissingAssetReturnsProblem(t *testing.T) {
	handler := newTestHandler(testFS())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dev/assets/missing.js", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "application/problem+json") {
		t.Errorf("content-type = %q", contentType)
	}
}

func TestUnknownAPIPathReturnsProblemNotShell(t *testing.T) {
	handler := newTestHandler(testFS())
	for _, path := range []string{"/dev/api", "/dev/api/unknown"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, recorder.Code)
		}
		if strings.Contains(recorder.Body.String(), "shell") {
			t.Errorf("%s served the SPA shell", path)
		}
	}
}

func TestMethodNotAllowed(t *testing.T) {
	handler := newTestHandler(testFS())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/dev", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
}

func TestSetupPageWhenShellMissing(t *testing.T) {
	handler := newTestHandler(fstest.MapFS{})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dev", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "not built") {
		t.Errorf("setup page = %q", recorder.Body.String())
	}
}
