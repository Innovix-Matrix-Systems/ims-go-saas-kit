package storage

import (
	"net/http"
	"strings"
)

// StaticHandler serves files from the public disk root (mounted at /storage).
// Directory listings are disabled and immutable cache headers are applied
// (file paths contain UUIDs, so contents never change under a path).
func StaticHandler(publicDir string) http.Handler {
	fileServer := http.FileServer(http.Dir(publicDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// User uploads must never run as a page on the app origin (H17): a
		// sandboxed, script-less CSP neutralises HTML/SVG opened directly.
		w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'; sandbox")
		fileServer.ServeHTTP(w, r)
	})
}
