package app

import (
	"embed"
	"io/fs"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/notifications"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/oauth"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/i18n"
)

// languages is the application's language list: the kit's languages, then
// any a project adds (a descriptor here plus its catalog files; see
// docs/guides/languages.md). The client app reads the same list from
// GET /api/v1/languages.
func languages() []i18n.Language {
	return i18n.Builtin()
}

// overridesFS holds the project's wording changes, locales/<namespace>/<code>.json
// (empty in the kit). They replace the text of existing keys without editing
// the module that owns them.
//
//go:embed all:locales
var overridesFS embed.FS

// translationBundles lists every module's text in registration order: the
// platform, the modules, the project's modules, then the overrides. A module
// that adds server-side text adds its bundle here; a key outside the
// module's namespaces is refused, and `ims i18n:coverage` fails while any
// language lacks a key.
func translationBundles() []i18n.Bundle {
	overrides, err := fs.Sub(overridesFS, "locales")
	if err != nil {
		panic(err) // embedded at build time
	}
	return []i18n.Bundle{
		i18n.Platform(),
		oauth.Translations(),
		notifications.Translations(),
		noticeTranslations(),
		i18n.Overrides(overrides), // last: the project's wording wins
	}
}

// newTranslator builds the translator every process uses (HTTP, CLI, jobs),
// narrows it to APP_SUPPORTED_LOCALES (empty: every language) and makes it the
// process default. A code that is not registered stops the start.
func newTranslator(cfg *config.Config) (*i18n.Translator, error) {
	translator, err := i18n.New(languages(), translationBundles()...)
	if err != nil {
		return nil, err
	}
	if err := translator.Support(cfg.App.SupportedLocales, cfg.App.Locale); err != nil {
		return nil, err
	}
	i18n.SetDefault(translator)
	return translator, nil
}

//go:embed all:translations
var noticesFS embed.FS

// noticeTranslations is the text of the notifications this package writes
// (payment notices, a disabled webhook), the "notices" namespace.
func noticeTranslations() i18n.Bundle {
	sub, err := fs.Sub(noticesFS, "translations")
	if err != nil {
		panic(err) // embedded at build time
	}
	return i18n.Bundle{Owner: "app", Namespaces: []string{"notices"}, FS: sub}
}

// Translations builds the translator and the delivery templates without the
// rest of the platform (no database): what `ims i18n:coverage` and
// `ims i18n:missing` read.
func Translations(cfg *config.Config) (*i18n.Translator, *delivery.Registry, error) {
	translator, err := newTranslator(cfg)
	if err != nil {
		return nil, nil, err
	}
	templates, err := newTemplateRegistry()
	if err != nil {
		return nil, nil, err
	}
	return translator, templates, nil
}
