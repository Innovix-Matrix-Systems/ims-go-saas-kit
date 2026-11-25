package i18n

import (
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

// Text directions.
const (
	LTR = "ltr"
	RTL = "rtl"
)

// A Language is one supported language. Adding a language is a descriptor
// here (or in the application's list, internal/app/i18n.go) plus its catalog
// files; nothing else in the code names a language.
type Language struct {
	// Code is the two-letter code used everywhere ("de"): catalog file
	// names, users.locale, Accept-Language matching.
	Code string `json:"code"`
	// Label is the language's own name ("Deutsch"), shown in menus.
	Label string `json:"label"`
	// English is its English name ("German"), for developer tools.
	English string `json:"english"`
	// Intl is the BCP 47 tag used to format dates and numbers ("de",
	// "bn-BD", "ar").
	Intl string `json:"intl"`
	// Dir is the text direction, LTR or RTL.
	Dir string `json:"dir"`
	// Digits is the numbering system for generated numbers ("latn",
	// "beng", "arab"). What a user types is never converted.
	Digits string `json:"digits"`
	// Reviewed is true once the translation passed the review step; until
	// then menus mark the language as beta.
	Reviewed bool `json:"reviewed"`
}

// Builtin returns the kit's languages, English first. English is the
// reference text and the fallback for a missing key.
func Builtin() []Language {
	return []Language{
		{Code: "en", Label: "English", English: "English", Intl: "en", Dir: LTR, Digits: "latn", Reviewed: true},
		{Code: "bn", Label: "বাংলা", English: "Bangla", Intl: "bn-BD", Dir: LTR, Digits: "beng", Reviewed: true},
		{Code: "de", Label: "Deutsch", English: "German", Intl: "de", Dir: LTR, Digits: "latn", Reviewed: true},
		{Code: "ar", Label: "العربية", English: "Arabic", Intl: "ar", Dir: RTL, Digits: "latn", Reviewed: true},
	}
}

// Reference is the code of the reference language: every other language is
// checked against its keys, and a key missing elsewhere falls back to it.
const Reference = "en"

// Tag is the formatting tag: Intl with the language's numbering system.
func (l Language) Tag() language.Tag {
	tag, err := language.Parse(l.Intl)
	if err != nil {
		tag = language.Make(l.Code)
	}
	if l.Digits != "" {
		if withDigits, err := tag.SetTypeForKey("nu", l.Digits); err == nil {
			tag = withDigits
		}
	}
	return tag
}

// FormatNumber formats an integer for the language: its grouping and its
// digits (1,234 / ১,২৩৪ / 1.234).
func (l Language) FormatNumber(n int64) string {
	return message.NewPrinter(l.Tag()).Sprint(number.Decimal(n))
}

// validLanguage reports what is wrong with a descriptor, or "".
func validLanguage(l Language) string {
	switch {
	case len(l.Code) != 2 || strings.ToLower(l.Code) != l.Code:
		return "code must be two lower-case letters"
	case l.Label == "" || l.English == "":
		return "label and english name are required"
	case l.Dir != LTR && l.Dir != RTL:
		return "dir must be ltr or rtl"
	}
	if _, err := language.Parse(l.Intl); err != nil {
		return "intl is not a valid language tag"
	}
	return ""
}
