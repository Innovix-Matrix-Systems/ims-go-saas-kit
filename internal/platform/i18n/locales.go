// Package i18n provides the language registry, the embedded message
// catalogs, the locale middleware and the translation helpers used by the
// responder, validator and services. Message IDs are dot-keys
// (validation.*, http-statuses.*); placeholders are go-i18n template
// variables ({{.Attribute}}). No code outside the registry and the catalogs
// names a language.
package i18n

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

// Translator resolves message IDs against the registered bundles of its
// languages.
type Translator struct {
	bundle    *goi18n.Bundle
	catalog   *catalog
	languages []Language
	supported []string
	// defaultCode is APP_LOCALE (Support); see DefaultCode.
	defaultCode string
	localizers  sync.Map // locale -> *goi18n.Localizer (one per locale, M26)
}

// Load builds the translator for the built-in languages and the platform
// bundle only (tests and tools; the application registers every module's
// bundle, internal/app/i18n.go).
func Load() (*Translator, error) {
	return New(Builtin(), Platform())
}

// New builds a translator for languages (the reference language first) from
// bundles, in order (see Bundle). Every language is supported until Support
// narrows the list; SetDefault makes it the process translator.
func New(languages []Language, bundles ...Bundle) (*Translator, error) {
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
	merged := newCatalog()
	for _, b := range bundles {
		if err := merged.register(b, codes); err != nil {
			return nil, err
		}
	}
	bundle := goi18n.NewBundle(language.English)
	for _, code := range codes {
		messages := make([]*goi18n.Message, 0, len(merged.messages[code]))
		for _, m := range merged.messages[code] {
			messages = append(messages, m.msg)
		}
		if err := bundle.AddMessages(language.Make(code), messages...); err != nil {
			return nil, fmt.Errorf("i18n: %s messages: %w", code, err)
		}
	}
	return &Translator{bundle: bundle, catalog: merged, languages: slices.Clone(languages), supported: codes}, nil
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
	t.defaultCode = strings.ToLower(defaultCode)
	return nil
}

// DefaultCode is the language for requests that match nothing (APP_LOCALE,
// set by Support); the reference language until then.
func (t *Translator) DefaultCode() string {
	if t.defaultCode == "" {
		return Reference
	}
	return t.defaultCode
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
// locale itself) in the request context. Whether Accept-Language matched is
// kept (FromHeader): when it did not, a later step may choose the account's
// or organization's language (Use).
func (t *Translator) Middleware(defaultCode string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Accept-Language")
			matched := ParseLocale(header, t.supported, "")
			locale := Locale(matched)
			if matched == "" {
				locale = Locale(defaultCode)
			}
			ctx := WithLocale(r.Context(), locale)
			ctx = context.WithValue(ctx, localizerKey, t.Localizer(locale))
			ctx = context.WithValue(ctx, fromHeaderKey, matched != "")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// FromHeader reports whether the request's language came from its
// Accept-Language header (the client's explicit choice).
func FromHeader(ctx context.Context) bool {
	matched, _ := ctx.Value(fromHeaderKey).(bool)
	return matched
}

// Use switches the context to a supported language (an unknown or
// unsupported code changes nothing).
func (t *Translator) Use(ctx context.Context, code string) context.Context {
	if !t.IsSupported(code) {
		return ctx
	}
	locale := Locale(strings.ToLower(code))
	ctx = WithLocale(ctx, locale)
	return context.WithValue(ctx, localizerKey, t.Localizer(locale))
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
	return localize(localizer, key, params)
}

// Tr translates a message ID for a language code, outside a request (an
// e-mail for its recipient, a notification for its reader). An unknown code
// is the reference language.
func Tr(code, key string, params map[string]any) string {
	return localize(Default().Localizer(Locale(LanguageFor(code).Code)), key, params)
}

// Count is the params for a plural message: {{.Count}} is the number
// formatted for the language, and it selects the plural form.
func Count(code string, n int64, params map[string]any) map[string]any {
	out := map[string]any{"Count": LanguageFor(code).FormatNumber(n), PluralCountParam: n}
	for k, v := range params {
		out[k] = v
	}
	return out
}

func localize(localizer *goi18n.Localizer, key string, params map[string]any) string {
	config := &goi18n.LocalizeConfig{MessageID: key, TemplateData: params}
	if params != nil {
		if count, ok := params[PluralCountParam]; ok {
			config.PluralCount = count
		}
	}
	text, err := localizer.Localize(config)
	if err == nil {
		return text
	}
	// A translation that lacks a plural form the number needs: the reference
	// text rather than nothing (the coverage check reports the gap).
	if fallback := fallbackLocalizer(); fallback != localizer {
		if text, err := fallback.Localize(config); err == nil {
			return text
		}
	}
	return ""
}

// TOr translates a message ID and falls back to a literal when missing.
func TOr(ctx context.Context, key, fallback string, params map[string]any) string {
	if message := T(ctx, key, params); message != "" {
		return message
	}
	return fallback
}

// defaultTranslator is the process translator (the application sets it at
// start). T uses it outside a request, and the package functions below read
// its languages.
var defaultTranslator atomic.Pointer[Translator]

// SetDefault makes t the process translator: what T uses outside a request,
// Tr, IsSupported (the locale validation) and LanguageFor.
//
// It is a process default like slog.Default, set once at start-up
// (internal/app.newTranslator) and read by code that has no request and no
// dependency to carry it: template functions, the validator, notification
// rendering, setting choices. Code that is wired (middleware, the delivery
// LocaleSource, the languages endpoint, the developer dashboard) takes the
// *Translator explicitly instead. Tests that need other languages build their
// own translator and call SetDefault.
func SetDefault(t *Translator) { defaultTranslator.Store(t) }

// Default returns the process translator, building the built-in one (the
// platform bundle only) when the application has not set its own (tests,
// tools).
func Default() *Translator {
	if t := defaultTranslator.Load(); t != nil {
		return t
	}
	t, err := Load()
	if err != nil {
		panic("i18n: embedded catalogs failed to load: " + err.Error())
	}
	defaultTranslator.CompareAndSwap(nil, t)
	return defaultTranslator.Load()
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
