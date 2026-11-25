package app

import (
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/i18n"
)

// languages is the application's language list: the kit's languages, then
// any a project adds (a descriptor here plus its catalog files). The client
// app reads the same list from GET /api/v1/languages.
func languages() []i18n.Language {
	return i18n.Builtin()
}

// newTranslator builds the translator every process uses (HTTP, CLI, jobs)
// and narrows it to APP_SUPPORTED_LOCALES (empty: every language). A code
// that is not registered stops the start.
func newTranslator(cfg *config.Config) (*i18n.Translator, error) {
	translator, err := i18n.New(languages())
	if err != nil {
		return nil, err
	}
	if err := translator.Support(cfg.App.SupportedLocales, cfg.App.Locale); err != nil {
		return nil, err
	}
	return translator, nil
}
