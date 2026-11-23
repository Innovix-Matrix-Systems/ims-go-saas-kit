// Package project exposes files from the repository root to the binary
// (the developer dashboard's Inspector lists the Go modules from go.mod).
package project

import (
	_ "embed"
	"strings"
)

// GoMod is the module's go.mod.
//
//go:embed go.mod
var GoMod []byte

//go:embed VERSION
var versionFile string

// Version is the release version from the VERSION file (semver, no "v").
var Version = strings.TrimSpace(versionFile)
