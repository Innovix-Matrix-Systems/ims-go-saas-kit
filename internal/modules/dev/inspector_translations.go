package dev

import (
	"net/http"

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

// WithTranslations gives the inspector the application's translator and
// delivery templates (Inspector → Translations).
func (s *InspectorService) WithTranslations(translator *i18n.Translator, templates *delivery.Registry) *InspectorService {
	s.translator, s.templates = translator, templates
	return s
}

// Translations reports how complete each language is. It reads the same
// registry the application translates with, so it is never out of date.
func (s *InspectorService) Translations() (*TranslationsReport, error) {
	report := &TranslationsReport{Default: s.cfg.App.Locale, Languages: []TranslationLanguage{}, Namespaces: []i18n.Namespace{}, Templates: []string{}}
	if s.translator == nil {
		return report, nil
	}
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

func (h *Handler) inspectorTranslations(w http.ResponseWriter, r *http.Request) {
	report, err := h.inspector.Translations()
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, report)
}
