package webui

import (
	"io"
	"net/http"
	"strings"
)

// ClientCSP is the Content-Security-Policy for the client app's HTML. The app
// loads only its own scripts and styles (no inline code, no third parties) and
// talks only to this origin.
const ClientCSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; " +
	"font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'"

// clientReserved are path prefixes the client app never owns: an unknown path
// under them is a JSON 404 from the API, not the app's HTML.
var clientReserved = []string{
	"/api", "/dev", "/docs", "/storage", "/mcp", "/oauth", "/.well-known", "/metrics", "/up",
}

// OwnsPath reports whether the client app at the site root should answer a
// request for this path (anything outside the reserved API and tool prefixes).
func OwnsPath(urlPath string) bool {
	for _, prefix := range clientReserved {
		if urlPath == prefix || strings.HasPrefix(urlPath, prefix+"/") {
			return false
		}
	}
	return true
}

// NewClient creates the handler for the client app mounted at the site root.
func NewClient(fsys http.FileSystem) *Handler {
	return NewWithOptions(fsys, Options{
		Prefix: "",
		Shell:  "index.html",
		ShellHeaders: map[string]string{
			"Content-Security-Policy": ClientCSP,
			"Permissions-Policy":      "camera=(), microphone=(), geolocation=(), payment=()",
		},
		SetupPage: writeClientSetupPage,
	})
}

func writeClientSetupPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark"><title>App — build required</title>
<style>
*{box-sizing:border-box}
body{margin:0;min-height:100vh;display:grid;place-items:center;padding:24px;background:Canvas;color:CanvasText;
  font-family:ui-sans-serif,-apple-system,"Segoe UI",Inter,Roboto,sans-serif}
main{max-width:520px;padding:28px;border:1px solid color-mix(in srgb,CanvasText 18%,Canvas);border-radius:12px}
h1{font-size:22px;margin:0 0 10px}
p{line-height:1.6;margin:0 0 10px;opacity:.85}
code{font-family:ui-monospace,Consolas,monospace;font-size:13px;padding:1px 6px;border-radius:5px;
  background:color-mix(in srgb,CanvasText 8%,Canvas)}
</style></head>
<body><main>
  <h1>The app has not been built</h1>
  <p>This binary is running without the embedded client app.</p>
  <p>Run <code>make app-build</code> and rebuild the binary, or start <code>make dev</code> and open the app's dev
  server.</p>
</main></body></html>`)
}
