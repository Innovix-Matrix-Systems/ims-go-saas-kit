package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/app"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/i18n"
)

func registerI18nCommands(registry *Registry, state *State) error {
	for _, cmd := range []*cobra.Command{
		newI18nCoverageCommand(state),
		newI18nMissingCommand(state),
	} {
		if err := registry.Add(cmd); err != nil {
			return err
		}
	}
	return nil
}

// newI18nCoverageCommand reports how complete each language is: the API's
// catalogs and the delivery templates. It fails while anything is missing,
// like privacy:coverage, so CI catches a half-translated language.
func newI18nCoverageCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "i18n:coverage",
		Short: "Show how complete each language is (API messages and e-mail templates)",
		Long: "For every registered language: the API catalog keys it has, lacks or has extra, keys whose placeholders or plural forms " +
			"differ from English, and e-mail/SMS templates without its variant. Exits with an error when anything is incomplete. " +
			"The client app has the same check: `npm run i18n:check` in web/app.",
		RunE: func(cmd *cobra.Command, args []string) error {
			translator, templates, err := app.Translations(state.Config())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(table, "LANGUAGE\tKEYS\tMISSING\tEXTRA\tPROBLEMS\tTEMPLATE GAPS\tREVIEWED")
			codes := translator.Codes()[1:]
			gaps, err := templates.Coverage(codes)
			if err != nil {
				return err
			}
			gapsByCode := map[string]int{}
			for _, gap := range gaps {
				gapsByCode[gap.Language] += len(gap.Missing) + len(gap.Problems)
			}
			incomplete := 0
			reports := translator.Coverage()
			for _, report := range reports {
				fmt.Fprintf(table, "%s (%s)\t%d/%d\t%d\t%d\t%d\t%d\t%s\n", report.Language.Code, report.Language.English,
					report.Translated, report.Total, len(report.Missing), len(report.Extra), len(report.Problems),
					gapsByCode[report.Language.Code], yesNo(report.Language.Reviewed))
				if !report.Complete() || gapsByCode[report.Language.Code] > 0 {
					incomplete++
				}
			}
			if err := table.Flush(); err != nil {
				return err
			}
			for _, report := range reports {
				for _, key := range report.Missing {
					fmt.Fprintf(out, "%s: missing %s\n", report.Language.Code, key)
				}
				for _, key := range report.Extra {
					fmt.Fprintf(out, "%s: extra %s (not in English)\n", report.Language.Code, key)
				}
				for _, problem := range report.Problems {
					fmt.Fprintf(out, "%s: %s: %s\n", report.Language.Code, problem.Key, problem.Detail)
				}
			}
			for _, gap := range gaps {
				for _, file := range gap.Missing {
					fmt.Fprintf(out, "%s: template %s has no variant of %s\n", gap.Language, gap.Template, file)
				}
				for _, problem := range gap.Problems {
					fmt.Fprintf(out, "%s: template %s: %s\n", gap.Language, gap.Template, problem)
				}
			}
			if incomplete > 0 {
				return fmt.Errorf("%d language(s) incomplete; `ims i18n:missing --locale <code>` lists what to translate", incomplete)
			}
			fmt.Fprintln(out, "Every language is complete.")
			return nil
		},
	}
}

// missingTemplate is a template file a language lacks, for --json.
type missingTemplate struct {
	Template string `json:"template"`
	File     string `json:"file"`
	Variant  string `json:"variant"`
}

// newI18nMissingCommand lists what one language lacks with the English text
// and translator notes: the input of the translate skill.
func newI18nMissingCommand(state *State) *cobra.Command {
	var locale string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "i18n:missing",
		Short: "List the API keys and templates a language lacks, with the English text",
		Long: "Print every API catalog key the language doesn't have yet, with its namespace, owning module, English text (each plural form) " +
			"and the translator note (a \"<key>._context\" entry), plus the e-mail/SMS template files without its variant. " +
			"--json prints the same for tools (the translate skill works from it).",
		Example: "  ims i18n:missing --locale de\n  ims i18n:missing --locale ar --json > missing-ar.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			translator, templates, err := app.Translations(state.Config())
			if err != nil {
				return err
			}
			lang, ok := translator.Language(locale)
			if !ok {
				return fmt.Errorf("--locale %q is not a registered language (registered: %s); add its descriptor in internal/app/i18n.go first", locale, strings.Join(translator.Codes(), ", "))
			}
			if lang.Code == i18n.Reference {
				return errors.New("English is the reference language: nothing to translate")
			}
			keys := translator.Missing(lang.Code)
			gaps, err := templates.Coverage([]string{lang.Code})
			if err != nil {
				return err
			}
			var files []missingTemplate
			for _, gap := range gaps {
				for _, file := range gap.Missing {
					files = append(files, missingTemplate{Template: gap.Template, File: file, Variant: variantName(file, lang.Code)})
				}
			}
			out := cmd.OutOrStdout()
			if asJSON {
				encoder := json.NewEncoder(out)
				encoder.SetIndent("", "  ")
				return encoder.Encode(map[string]any{"language": lang, "plural_forms": i18n.RequiredPluralForms(lang), "keys": nonNil(keys), "templates": nonNil(files)})
			}
			fmt.Fprintf(out, "%s (%s): %d key(s), %d template file(s) missing. Plural forms: %s.\n", lang.Code, lang.English, len(keys), len(files), strings.Join(i18n.RequiredPluralForms(lang), ", "))
			for _, key := range keys {
				forms := make([]string, 0, len(key.English))
				for form := range key.English {
					forms = append(forms, form)
				}
				sort.Strings(forms)
				fmt.Fprintf(out, "\n%s  [%s, %s]\n", key.Key, key.Namespace, key.Owner)
				for _, form := range forms {
					if len(forms) == 1 {
						fmt.Fprintf(out, "  %s\n", key.English[form])
					} else {
						fmt.Fprintf(out, "  %s: %s\n", form, key.English[form])
					}
				}
				if key.Context != "" {
					fmt.Fprintf(out, "  note: %s\n", key.Context)
				}
			}
			for _, file := range files {
				fmt.Fprintf(out, "\ntemplate %s: %s → %s\n", file.Template, file.File, file.Variant)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&locale, "locale", "", "language code to check (required)")
	_ = cmd.MarkFlagRequired("locale")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON for tools")
	return cmd
}

func variantName(file, code string) string {
	dot := strings.LastIndex(file, ".")
	return file[:dot] + "." + code + file[dot:]
}

func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "beta"
}
