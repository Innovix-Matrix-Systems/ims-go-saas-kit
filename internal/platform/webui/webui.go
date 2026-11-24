// Package webui serves embedded single-page apps: the developer dashboard
// under /dev and the client app at the site root.
package webui

import (
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

// Options describe one embedded SPA.
type Options struct {
	// Prefix is the URL path the app is mounted under ("" for the root).
	Prefix string
	// Shell is the HTML file served for every route the app owns.
	Shell string
	// ShellHeaders are added to the shell response only (for example a
	// Content-Security-Policy that the JSON API does not need).
	ShellHeaders map[string]string
	// SetupPage is written when the build is missing (only the placeholder
	// is embedded). Nil uses the developer dashboard's page.
	SetupPage func(w http.ResponseWriter)
}

// Handler serves a client build with hashed-asset caching and SPA fallback. A
// missing build (only the placeholder is embedded) renders a small setup page.
type Handler struct {
	fsys http.FileSystem
	opts Options
}

// New creates the developer dashboard handler over a filesystem rooted at its
// client build.
func New(fsys http.FileSystem) *Handler {
	return NewWithOptions(fsys, Options{Prefix: "/dev", Shell: "index.html"})
}

// NewWithOptions creates an SPA handler for any mount point.
func NewWithOptions(fsys http.FileSystem, opts Options) *Handler {
	opts.Prefix = strings.TrimRight(opts.Prefix, "/")
	if opts.Shell == "" {
		opts.Shell = "index.html"
	}
	if opts.SetupPage == nil {
		opts.SetupPage = writeSetupPage
	}
	return &Handler{fsys: fsys, opts: opts}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "The HTTP method is not supported for this resource.")
		return
	}
	prefix := h.opts.Prefix
	if r.URL.Path == prefix+"/api" || strings.HasPrefix(r.URL.Path, prefix+"/api/") {
		writeProblem(w, http.StatusNotFound, "RESOURCE_NOT_FOUND", "The requested resource was not found.")
		return
	}

	name := strings.TrimPrefix(r.URL.Path, prefix)
	name = strings.TrimPrefix(path.Clean("/"+name), "/")
	if name == "" || name == "." || name == h.opts.Shell {
		h.serveShell(w, r)
		return
	}
	if h.serveFile(w, r, name) {
		return
	}
	if strings.HasPrefix(name, "assets/") {
		writeProblem(w, http.StatusNotFound, "RESOURCE_NOT_FOUND", "The requested asset was not found.")
		return
	}
	h.serveShell(w, r)
}

func (h *Handler) serveFile(w http.ResponseWriter, r *http.Request, name string) bool {
	file, err := h.fsys.Open("/" + name)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, name, info.ModTime(), file)
	return true
}

func (h *Handler) serveShell(w http.ResponseWriter, r *http.Request) {
	file, err := h.fsys.Open("/" + h.opts.Shell)
	if err != nil {
		h.opts.SetupPage(w)
		return
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		h.opts.SetupPage(w)
		return
	}
	for key, value := range h.opts.ShellHeaders {
		w.Header().Set(key, value)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, h.opts.Shell, info.ModTime(), file)
}

func writeSetupPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="dark"><title>Dev Dashboard — build required</title>
<style>
:root{color-scheme:dark}
*{box-sizing:border-box}
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#08080a;color:#f4f4f6;
  font-family:ui-sans-serif,-apple-system,"Segoe UI",Inter,Roboto,sans-serif;overflow:hidden}
.grid{position:fixed;inset:0;background-image:linear-gradient(rgba(255,255,255,.028) 1px,transparent 1px),
  linear-gradient(90deg,rgba(255,255,255,.028) 1px,transparent 1px);background-size:46px 46px;
  mask-image:radial-gradient(circle at center,black,transparent 70%);
  -webkit-mask-image:radial-gradient(circle at center,black,transparent 70%)}
.aura{position:fixed;width:620px;height:620px;border-radius:50%;pointer-events:none;
  background:radial-gradient(circle,rgba(255,92,26,.16),transparent 62%);
  animation:pulse 6s ease-in-out infinite}
main{position:relative;z-index:1;max-width:560px;padding:28px;border:1px solid #26262c;border-radius:14px;
  background:linear-gradient(180deg,#17171b,#111114);box-shadow:0 18px 40px rgba(0,0,0,.35);
  animation:up .45s cubic-bezier(.2,.7,.2,1)}
.brand{display:flex;align-items:center;gap:10px;margin-bottom:20px}
.mark{width:36px;height:36px;border-radius:10px;display:grid;place-items:center;
  background:linear-gradient(145deg,#ff6a2b,#e0430a);color:#0a0a0c;font-weight:800;font-size:17px;
  box-shadow:0 6px 18px rgba(255,92,26,.28)}
.brand-title{font-size:13px;font-weight:700;letter-spacing:.08em;text-transform:uppercase}
.brand-sub{font-size:10px;letter-spacing:.16em;color:#6b6b76;text-transform:uppercase}
.badge{display:inline-flex;align-items:center;gap:7px;font-size:10px;letter-spacing:.1em;
  text-transform:uppercase;color:#f5c451;border:1px solid rgba(245,196,81,.3);
  background:rgba(245,196,81,.1);padding:3px 10px;border-radius:999px;margin-bottom:14px}
.dot{width:6px;height:6px;border-radius:50%;background:#f5c451;animation:blink 2.6s ease-in-out infinite}
h1{font-size:23px;margin:0 0 10px;letter-spacing:-.02em}
p{color:#8d8d99;line-height:1.65;margin:0 0 12px;font-size:13.5px}
code{background:#1d1d22;border:1px solid #26262c;border-radius:6px;padding:2px 6px;
  font-family:ui-monospace,Consolas,monospace;font-size:12.5px;color:#ffb795}
.actions{display:flex;gap:8px;margin-top:20px}
.btn{display:inline-flex;align-items:center;gap:7px;padding:8px 14px;border-radius:7px;
  border:1px solid #34343c;background:#17171b;color:#c9c9d1;font-size:12.5px;font-weight:600;
  text-decoration:none;transition:background .15s ease,border-color .15s ease,color .15s ease}
.btn:hover{background:#1d1d22;color:#f4f4f6;border-color:#41414b}
.btn.primary{background:#ff5c1a;border-color:#ff5c1a;color:#140800}
.btn.primary:hover{background:#ff743d;border-color:#ff743d}
@keyframes pulse{0%,100%{transform:scale(1);opacity:.7}50%{transform:scale(1.09);opacity:1}}
@keyframes up{from{opacity:0;transform:translateY(12px)}to{opacity:1;transform:none}}
@keyframes blink{0%,100%{opacity:1}50%{opacity:.35}}
@media (prefers-reduced-motion: reduce){*{animation-duration:.001ms!important;animation-iteration-count:1!important}}
</style></head>
<body><div class="grid"></div><div class="aura"></div>
<main>
  <div class="brand"><div class="mark">D</div>
    <div><div class="brand-title">Dev Dashboard</div><div class="brand-sub">Superadmin control</div></div>
  </div>
  <div class="badge"><span class="dot"></span>Assets not built</div>
  <h1>The dashboard shell is missing</h1>
  <p>This Go binary is running without the embedded SPA assets.</p>
  <p>Run <code>make web-build</code> and rebuild the binary, or start <code>make dev</code> for API + UI
  hot reload.</p>
  <div class="actions">
    <a class="btn primary" href="/dev">Retry</a>
    <a class="btn" href="/docs">API docs</a>
  </div>
</main></body></html>`)
}

func writeProblem(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	timestamp := time.Now().UTC().Format(time.RFC3339)
	_, _ = io.WriteString(w, `{"type":"about:blank","title":"`+http.StatusText(status)+`","status":`+
		itoa(status)+`,"detail":"`+detail+`","error_code":"`+code+`","timestamp":"`+timestamp+`"}`)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := [12]byte{}
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
