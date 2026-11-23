// Package webapp embeds the built client app SPA (web/app/dist).
package webapp

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the client build rooted at dist.
func Dist() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}
