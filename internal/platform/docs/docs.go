// Package docs serves the embedded OpenAPI document and a Scalar-based API
// reference UI. Both routes are registered only when DOCS_ENABLED is set.
package docs

import (
	"net/http"

	api "github.com/Innovix-Matrix-Systems/ims-go-api-starter/api"
)

// scalarHTML loads the Scalar API reference from a CDN and points it at the
// embedded spec. Deployment can disable docs entirely for offline/air-gapped
// installs.
//
// `persistAuth` keeps the bearer token in the browser's localStorage so it
// survives a page reload (Scalar otherwise drops it). Remove the
// `data-configuration` attribute to disable persistence.
const scalarHTML = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>IMS Go API — Reference</title>
  </head>
  <body>
    <script
      id="api-reference"
      data-url="/docs/openapi.yaml"
      data-configuration='{"persistAuth":true}'
    ></script>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
  </body>
</html>
`

// UI serves the Scalar API reference page.
func UI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(scalarHTML))
}

// Spec serves the raw embedded OpenAPI document.
func Spec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(api.Spec)
}
