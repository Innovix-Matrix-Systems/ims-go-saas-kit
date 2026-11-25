package oauth

import (
	"embed"
	"io/fs"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/i18n"
)

//go:embed all:locales
var localesFS embed.FS

// Translations is the module's text: the sign-in and consent pages
// (locales/oauth/<code>.json), registered in internal/app/i18n.go.
func Translations() i18n.Bundle {
	sub, err := fs.Sub(localesFS, "locales")
	if err != nil {
		panic(err) // embedded at build time
	}
	return i18n.Bundle{Owner: "oauth", Namespaces: []string{"oauth"}, FS: sub}
}
