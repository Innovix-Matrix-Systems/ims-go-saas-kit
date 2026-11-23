// Package api exposes the canonical OpenAPI document (api/openapi.yaml) as an
// embedded asset so it can be served by the running binary without shipping
// loose files.
package api

import _ "embed"

// Spec is the raw canonical OpenAPI 3 document.
//
//go:embed openapi.yaml
var Spec []byte
