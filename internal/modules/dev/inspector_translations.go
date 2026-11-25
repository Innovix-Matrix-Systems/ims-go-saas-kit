package dev

import (
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/i18n"
)

// ── translations ──────────────────────────────────────────────────────────

// TranslationLanguage is one language's coverage: API catalog keys and
// delivery template variants.
type TranslationLanguage struct {
	i18n.Language
	Supported    bool                        `json:"supported"`
	Total        int                         `json:"total"`
	Translated   int                         `json:"translated"`
	Missing      []string                    `json:"missing"`
	Extra        []string                    `json:"extra"`
	Problems     []i18n.Problem              `json:"problems"`
	PluralForms  []string                    `json:"plural_forms"`
	TemplateGaps []delivery.TemplateCoverage `json:"template_gaps"`
	Overrides    []string                    `json:"overrides"`
}

// TranslationsReport is what Inspector → Translations shows: every language,
// the namespaces and who owns them, and the delivery templates.
type TranslationsReport struct {
	Default    string                `json:"default"`
	Languages  []TranslationLanguage `json:"languages"`
	Namespaces []i18n.Namespace      `json:"namespaces"`
	Templates  []string              `json:"templates"`
}

// TranslationService reports translation coverage (Inspector → Translations)
// from the translator and delivery templates the application runs with.
type TranslationService struct {
	translator *i18n.Translator
	templates  *delivery.Registry
}

func newTranslationService(deps Deps) *TranslationService {
	s := &TranslationService{translator: deps.Translations}
	if deps.Deliveries != nil {
		s.templates = deps.Deliveries.Templates()
	}
	return s
}

// Report says how complete each language is. It reads the same
// registry the application translates with, so it is never out of date.
func (s *TranslationService) Report() (*TranslationsReport, error) {
	report := &TranslationsReport{Languages: []TranslationLanguage{}, Namespaces: []i18n.Namespace{}, Templates: []string{}}
	if s.translator == nil {
		return report, nil
	}
	report.Default = s.translator.DefaultCode()
	report.Namespaces = s.translator.Namespaces()
	var gaps []delivery.TemplateCoverage
	if s.templates != nil {
		report.Templates = s.templates.Names()
		var err error
		if gaps, err = s.templates.Coverage(s.translator.Codes()[1:]); err != nil {
			return nil, err
		}
	}
	coverage := map[string]i18n.Coverage{}
	for _, c := range s.translator.Coverage() {
		coverage[c.Language.Code] = c
	}
	overridden := s.translator.Overridden()
	for _, lang := range s.translator.Languages() {
		row := TranslationLanguage{
			Language: lang, Supported: s.translator.IsSupported(lang.Code),
			Missing: []string{}, Extra: []string{}, Problems: []i18n.Problem{},
			PluralForms: i18n.RequiredPluralForms(lang), TemplateGaps: []delivery.TemplateCoverage{},
			Overrides: append([]string{}, overridden[lang.Code]...),
		}
		if c, ok := coverage[lang.Code]; ok {
			row.Total, row.Translated, row.Missing, row.Extra, row.Problems = c.Total, c.Translated, c.Missing, c.Extra, c.Problems
		} else { // the reference language
			for _, ns := range report.Namespaces {
				row.Total += ns.Keys
			}
			row.Translated = row.Total
		}
		for _, gap := range gaps {
			if gap.Language == lang.Code {
				row.TemplateGaps = append(row.TemplateGaps, gap)
			}
		}
		report.Languages = append(report.Languages, row)
	}
	return report, nil
}
