// Package version reports the application version.
//
// The VERSION file at the repository root is the single source of truth.
// Release builds stamp it exactly (make build passes -ldflags "-X
// .../internal/version.Version=0.1.0"); every other build — go run, air,
// tests — reports it with a "-dev" suffix so a local binary is never
// mistaken for a release.
package version

import project "github.com/Innovix-Matrix-Systems/ims-go-api-starter"

// Version is the build version, e.g. "0.1.0" (release) or "0.1.0-dev".
var Version = "dev"

func init() {
	if Version == "dev" {
		Version = project.Version + "-dev"
	}
}
