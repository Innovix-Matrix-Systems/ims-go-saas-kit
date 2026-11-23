//go:build tools

// Package tools documents build-time tooling. Tool versions are pinned through
// `go run <module>@<version>` in the Makefile (sqlc, oapi-codegen), so they
// never leak into the application's module graph.
package tools
