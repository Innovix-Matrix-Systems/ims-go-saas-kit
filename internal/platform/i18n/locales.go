// Package i18n provides the embedded message catalogs (en/bn), the locale
// middleware and the translation helpers used by the responder, validator and
// services. Message IDs are dot-keys
// (messages.*, validation.*, http-statuses.*); placeholders are go-i18n
// template variables ({{.Attribute}}).
package i18n

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/*.json
var catalogFS embed.FS

// Translator resolves message IDs against the embedded catalogs.
type Translator struct {
	bundle     *goi18n.Bundle
	localizers sync.Map // locale -> *goi18n.Localizer (one per locale, M26)
}

// Load builds the translator from the embedded en/bn catalogs.
func Load() (*Translator, error) {
	bundle := goi18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)
	for _, locale := range []string{"en", "bn"} {
		if _, err := bundle.LoadMessageFileFS(catalogFS, "locales/"+locale+".json"); err != nil {
			return nil, fmt.Errorf("i18n: load %s catalog: %w", locale, err)
		}
	}
	return &Translator{bundle: bundle}, nil
}

// Localizer returns a localizer for a locale; the empty locale falls back to
// English.
func (t *Translator) Localizer(locale Locale) *goi18n.Localizer {
	lang := string(locale)
	if lang == "" {
		lang = "en"
	}
	if cached, ok := t.localizers.Load(lang); ok {
		return cached.(*goi18n.Localizer)
	}
	localizer, _ := t.localizers.LoadOrStore(lang, goi18n.NewLocalizer(t.bundle, lang))
	return localizer.(*goi18n.Localizer)
}

// Middleware resolves the request locale and stores its localizer (and the
// locale itself) in the request context.
func (t *Translator) Middleware(supported []string, fallback string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			locale := Locale(ParseLocale(r.Header.Get("Accept-Language"), supported, fallback))
			ctx := WithLocale(r.Context(), locale)
			ctx = context.WithValue(ctx, localizerKey, t.Localizer(locale))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func localizerFromContext(ctx context.Context) *goi18n.Localizer {
	if ctx == nil {
		return nil
	}
	if localizer, ok := ctx.Value(localizerKey).(*goi18n.Localizer); ok {
		return localizer
	}
	return nil
}

// PluralCountParam is the TemplateData key that selects CLDR plural forms.
const PluralCountParam = "PluralCount"

// T translates a message ID for the context locale. It returns "" when the
// message is missing so callers can apply their own fallback.
func T(ctx context.Context, key string, params map[string]any) string {
	localizer := localizerFromContext(ctx)
	if localizer == nil {
		localizer = fallbackLocalizer()
	}
	config := &goi18n.LocalizeConfig{MessageID: key, TemplateData: params}
	if params != nil {
		if count, ok := params[PluralCountParam]; ok {
			config.PluralCount = count
		}
	}
	message, err := localizer.Localize(config)
	if err != nil {
		return ""
	}
	return message
}

// TOr translates a message ID and falls back to a literal when missing.
func TOr(ctx context.Context, key, fallback string, params map[string]any) string {
	if message := T(ctx, key, params); message != "" {
		return message
	}
	return fallback
}

var (
	fallbackOnce       sync.Once
	fallbackTranslator *Translator
	fallbackErr        error
)

// fallbackLocalizer translates requests (and CLI/job output) that never ran
// through the locale middleware, using the English catalog.
func fallbackLocalizer() *goi18n.Localizer {
	fallbackOnce.Do(func() {
		fallbackTranslator, fallbackErr = Load()
	})
	if fallbackErr != nil {
		panic("i18n: embedded catalogs failed to load: " + fallbackErr.Error())
	}
	return fallbackTranslator.Localizer(Locale("en"))
}
