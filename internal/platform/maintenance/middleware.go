package maintenance

import (
	"net/http"
	"strings"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
)

// BypassHeader carries the bypass secret of `ims down --secret` /
// `--with-secret`. A header, not a cookie or URL: API clients send headers,
// and a secret in a URL ends up in logs.
const BypassHeader = "X-Maintenance-Bypass"

// OpenPaths stay reachable while the app is down (Laravel's $except): health
// probes and scrapes, the developer dashboard (the people doing the
// maintenance, and the toggle that ends it) and the static API docs. A path
// ending in "/*" matches everything below it. Products extend it by passing
// their own list to Guard.
var OpenPaths = []string{
	"/up",
	"/api/health",
	"/metrics",
	"/dev",
	"/dev/*",
	"/docs",
	"/docs/openapi.yaml",
}

// Guard answers 503 MAINTENANCE_MODE (with Retry-After) to every request
// outside open while the app is down, unless it carries the bypass secret.
// While up it costs one cached read.
func Guard(checker *Checker, responder *httpx.Responder, open ...string) func(http.Handler) http.Handler {
	exact := map[string]bool{}
	var prefixes []string
	for _, path := range open {
		if prefix, ok := strings.CutSuffix(path, "/*"); ok {
			prefixes = append(prefixes, prefix+"/")
			continue
		}
		exact[path] = true
	}
	isOpen := func(path string) bool {
		if exact[path] {
			return true
		}
		for _, prefix := range prefixes {
			if strings.HasPrefix(path, prefix) {
				return true
			}
		}
		return false
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state := checker.Current(r.Context())
			if !state.Down || isOpen(r.URL.Path) || state.Allows(r.Header.Get(BypassHeader)) {
				next.ServeHTTP(w, r)
				return
			}
			responder.Error(w, r, Error(state))
		})
	}
}

// Error is the 503 problem for a down state.
func Error(state State) *apperr.Error {
	message := state.Message
	if message == "" {
		message = DefaultMessage
	}
	retry := state.RetryAfter
	if retry <= 0 {
		retry = DefaultRetryAfter
	}
	return apperr.New(apperr.MaintenanceMode, message).WithRetryAfter(retry)
}
