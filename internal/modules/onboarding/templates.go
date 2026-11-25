package onboarding

import (
	"embed"
	"io/fs"
)

//go:embed all:templates
var templatesFS embed.FS

// Templates are the delivery templates this package sends (the sign-up notice for an existing account),
// registered in internal/app/delivery.go. A template folder holds its
// English files and a variant per language (mail.bn.txt); `ims i18n:coverage`
// reports a missing variant.
func Templates() fs.FS {
	sub, err := fs.Sub(templatesFS, "templates")
	if err != nil {
		panic(err) // embedded at build time
	}
	return sub
}
