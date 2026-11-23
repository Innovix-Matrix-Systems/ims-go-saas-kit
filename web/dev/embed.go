// Package webdev embeds the built developer dashboard SPA (web/dev/dist).
package webdev

import (
	"embed"
	"io/fs"
)

//go:embed all:dist/client
var dist embed.FS

// Dist returns the client build rooted at dist/client.
func Dist() (fs.FS, error) {
	return fs.Sub(dist, "dist/client")
}

// PackageJSON is the dashboard's package.json (listed by the Inspector).
//
//go:embed package.json
var PackageJSON []byte
