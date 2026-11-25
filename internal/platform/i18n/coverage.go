package i18n

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
)

// Coverage is how complete one language's catalogs are, compared with the
// reference language.
type Coverage struct {
	Language   Language  `json:"language"`
	Total      int       `json:"total"`      // keys in the reference language
	Translated int       `json:"translated"` // of those, present in this one
	Missing    []string  `json:"missing"`
	Extra      []string  `json:"extra"` // keys the reference language doesn't have
	Problems   []Problem `json:"problems"`
}

// Complete reports whether nothing is missing, extra or wrong.
func (c Coverage) Complete() bool {
	return len(c.Missing) == 0 && len(c.Extra) == 0 && len(c.Problems) == 0
}

// A Problem is a translated key that doesn't match its reference text.
type Problem struct {
	Key    string `json:"key"`
	Detail string `json:"detail"`
}

// MissingKey is one key a language lacks, with what a translator needs.
type MissingKey struct {
	Key       string            `json:"key"`
	Namespace string            `json:"namespace"`
	Owner     string            `json:"owner"`
	English   map[string]string `json:"english"` // plural form (or "other") -> reference text
	Context   string            `json:"context,omitempty"`
}

// placeholder matches {{.Name}} (and {{ .Name }}).
var placeholder = regexp.MustCompile(`\{\{\s*\.(\w+)`)

// placeholders returns the sorted placeholder names used across a message's
// forms.
func placeholders(text ...string) []string {
	seen := map[string]bool{}
	for _, t := range text {
		for _, match := range placeholder.FindAllStringSubmatch(t, -1) {
			seen[match[1]] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func messageTexts(m *entry) []string {
	var out []string
	for _, form := range forms(m.msg) {
		out = append(out, formText(m.msg, form))
	}
	return out
}

// Coverage reports every non-reference language.
func (t *Translator) Coverage() []Coverage {
	out := make([]Coverage, 0, len(t.languages)-1)
	for _, lang := range t.languages[1:] {
		out = append(out, t.coverageOf(lang))
	}
	return out
}

func (t *Translator) coverageOf(lang Language) Coverage {
	ref := t.catalog.messages[Reference]
	own := t.catalog.messages[lang.Code]
	report := Coverage{Language: lang, Total: len(ref), Missing: []string{}, Extra: []string{}, Problems: []Problem{}}
	required := RequiredPluralForms(lang)
	for key, refMessage := range ref {
		m, ok := own[key]
		if !ok {
			report.Missing = append(report.Missing, key)
			continue
		}
		report.Translated++
		if want, got := placeholders(messageTexts(refMessage)...), placeholders(messageTexts(m)...); !slices.Equal(want, got) {
			report.Problems = append(report.Problems, Problem{Key: key,
				Detail: "placeholders differ: want " + listOrNone(want) + ", got " + listOrNone(got)})
		}
		if isPlural(refMessage.msg) {
			var absent []string
			for _, form := range required {
				if formText(m.msg, form) == "" {
					absent = append(absent, form)
				}
			}
			if len(absent) > 0 {
				report.Problems = append(report.Problems, Problem{Key: key,
					Detail: "plural forms missing for " + lang.Code + ": " + strings.Join(absent, ", ")})
			}
		}
	}
	for key := range own {
		if _, ok := ref[key]; !ok {
			report.Extra = append(report.Extra, key)
		}
	}
	sort.Strings(report.Missing)
	sort.Strings(report.Extra)
	sort.Slice(report.Problems, func(i, j int) bool { return report.Problems[i].Key < report.Problems[j].Key })
	return report
}

func listOrNone(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// Missing lists the keys a language lacks, with their reference text and
// translator notes: what `ims i18n:missing` prints and the translate skill
// works from.
func (t *Translator) Missing(code string) []MissingKey {
	ref := t.catalog.messages[Reference]
	own := t.catalog.messages[code]
	var out []MissingKey
	for key, m := range ref {
		if _, ok := own[key]; ok {
			continue
		}
		english := map[string]string{}
		for _, form := range forms(m.msg) {
			english[form] = formText(m.msg, form)
		}
		out = append(out, MissingKey{Key: key, Namespace: m.namespace, Owner: m.owner, English: english, Context: t.catalog.contexts[key]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

var (
	pluralCache sync.Map // language code -> []string
)

// RequiredPluralForms returns the CLDR cardinal plural forms a language uses
// ("one", "other" for English; six for Arabic), in the catalog's order.
func RequiredPluralForms(lang Language) []string {
	if cached, ok := pluralCache.Load(lang.Code); ok {
		return cached.([]string)
	}
	tag, err := language.Parse(lang.Intl)
	if err != nil {
		tag = language.Make(lang.Code)
	}
	names := map[plural.Form]string{plural.Other: "other", plural.Zero: "zero", plural.One: "one", plural.Two: "two", plural.Few: "few", plural.Many: "many"}
	seen := map[string]bool{"other": true}
	for n := 0; n <= 1_000_000; n = nextSample(n) {
		seen[names[plural.Cardinal.MatchPlural(tag, n, 0, 0, 0, 0)]] = true
	}
	var out []string
	for _, form := range pluralForms {
		if seen[form] {
			out = append(out, form)
		}
	}
	pluralCache.Store(lang.Code, out)
	return out
}

// nextSample walks every number to 1,000, then the powers of ten: enough to
// meet each CLDR integer rule.
func nextSample(n int) int {
	if n < 1000 {
		return n + 1
	}
	return n * 10
}
