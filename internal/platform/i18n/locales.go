// Package i18n provides the language registry, the embedded message
// catalogs, the locale middleware and the translation helpers used by the
// responder, validator and services. Message IDs are dot-keys
// (validation.*, http-statuses.*); placeholders are go-i18n template
// variables ({{.Attribute}}). No code outside the registry and the catalogs
// names a language.
package i18n

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/*.json
var catalogFS embed.FS

// Translator resolves message IDs against the embedded catalogs of its
// languages.
type Translator struct {
	bundle     *goi18n.Bundle
	languages  []Language
	supported  []string
	localizers sync.Map // locale -> *goi18n.Localizer (one per locale, M26)
}

// Load builds the translator for the built-in languages.
func Load() (*Translator, error) {
	return New(Builtin())
}

// New builds a translator for languages (the reference language first). It
// becomes the process default, used by T outside a request. Every language
// is supported until Support narrows the list.
func New(languages []Language) (*Translator, error) {
	if len(languages) == 0 || languages[0].Code != Reference {
		return nil, fmt.Errorf("i18n: the language list must start with the reference language %q", Reference)
	}
	seen := map[string]bool{}
	codes := make([]string, 0, len(languages))
	for _, lang := range languages {
		if problem := validLanguage(lang); problem != "" {
			return nil, fmt.Errorf("i18n: language %q: %s", lang.Code, problem)
		}
		if seen[lang.Code] {
			return nil, fmt.Errorf("i18n: language %q is listed twice", lang.Code)
		}
		seen[lang.Code] = true
		codes = append(codes, lang.Code)
	}
	bundle := goi18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)
	for _, code := range codes {
		if _, err := bundle.LoadMessageFileFS(catalogFS, "locales/"+code+".json"); err != nil {
			return nil, fmt.Errorf("i18n: load %s catalog: %w", code, err)
		}
	}
	t := &Translator{bundle: bundle, languages: slices.Clone(languages), supported: codes}
	setDefault(t)
	return t, nil
}

// Support narrows the languages offered to clients (APP_SUPPORTED_LOCALES).
// Empty keeps every registered language; a code that is not registered is an
// error, as is a list without the default language.
func (t *Translator) Support(codes []string, defaultCode string) error {
	if len(codes) == 0 {
		codes = t.Codes()
	}
	supported := make([]string, 0, len(codes))
	for _, code := range codes {
		lang, ok := t.Language(code)
		if !ok {
			return fmt.Errorf("APP_SUPPORTED_LOCALES: %q is not a registered language (registered: %s)", code, strings.Join(t.Codes(), ", "))
		}
		supported = append(supported, lang.Code)
	}
	if !slices.Contains(supported, strings.ToLower(defaultCode)) {
		return fmt.Errorf("APP_LOCALE %q must be one of the supported languages (%s)", defaultCode, strings.Join(supported, ", "))
	}
	t.supported = supported
	return nil
}

// Languages returns every registered language, the reference first.
func (t *Translator) Languages() []Language { return slices.Clone(t.languages) }

// Codes returns every registered language code.
func (t *Translator) Codes() []string {
	codes := make([]string, len(t.languages))
	for i, lang := range t.languages {
		codes[i] = lang.Code
	}
	return codes
}

// Supported returns the languages offered to clients, in registry order.
func (t *Translator) Supported() []Language {
	out := make([]Language, 0, len(t.supported))
	for _, lang := range t.languages {
		if slices.Contains(t.supported, lang.Code) {
			out = append(out, lang)
		}
	}
	return out
}

// IsSupported reports whether code is one of the offered languages.
func (t *Translator) IsSupported(code string) bool {
	return slices.Contains(t.supported, strings.ToLower(code))
}

// Language finds a registered language by code (case-insensitive).
func (t *Translator) Language(code string) (Language, bool) {
	for _, lang := range t.languages {
		if strings.EqualFold(lang.Code, code) {
			return lang, true
		}
	}
	return Language{}, false
}

// Localizer returns a localizer for a locale; the empty locale is the
// reference language, which is also the fallback for a missing message.
func (t *Translator) Localizer(locale Locale) *goi18n.Localizer {
	lang := string(locale)
	if lang == "" {
		lang = Reference
	}
	if cached, ok := t.localizers.Load(lang); ok {
		return cached.(*goi18n.Localizer)
	}
	localizer, _ := t.localizers.LoadOrStore(lang, goi18n.NewLocalizer(t.bundle, lang, Reference))
	return localizer.(*goi18n.Localizer)
}

// Middleware resolves the request locale among the supported languages
// (defaultCode when nothing matches) and stores its localizer (and the
// locale itself) in the request context.
func (t *Translator) Middleware(defaultCode string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			locale := Locale(ParseLocale(r.Header.Get("Accept-Language"), t.supported, defaultCode))
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

// defaultTranslator is the last translator built (the application builds one
// at start). T uses it outside a request, and the package functions below
// read its languages.
var defaultTranslator atomic.Pointer[Translator]

func setDefault(t *Translator) { defaultTranslator.Store(t) }

// Default returns the process translator, building the built-in one when the
// application has not built its own (tests, tools).
func Default() *Translator {
	if t := defaultTranslator.Load(); t != nil {
		return t
	}
	t, err := Load()
	if err != nil {
		panic("i18n: embedded catalogs failed to load: " + err.Error())
	}
	return t
}

// IsSupported reports whether code is a language the application offers
// (the `locale` validation tag uses it).
func IsSupported(code string) bool { return Default().IsSupported(code) }

// LanguageFor returns the registered language for code, or the reference
// language when code is empty or unknown.
func LanguageFor(code string) Language {
	t := Default()
	if lang, ok := t.Language(code); ok {
		return lang
	}
	lang, _ := t.Language(Reference)
	return lang
}

// fallbackLocalizer translates requests (and CLI/job output) that never ran
// through the locale middleware, in the reference language.
func fallbackLocalizer() *goi18n.Localizer {
	return Default().Localizer(Locale(Reference))
}
