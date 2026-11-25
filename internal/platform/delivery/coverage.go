package delivery

import (
	"errors"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// templateFiles are the files a template may have; each needs a variant per
// language when it has text.
var templateFiles = []string{"subject.txt", "mail.txt", "mail.html", "sms.txt", "push.json"}

// TemplateCoverage is one template's variants for one language.
type TemplateCoverage struct {
	Template string `json:"template"`
	Language string `json:"language"`
	// Missing are the files without a variant (mail.txt has no mail.de.txt).
	Missing []string `json:"missing"`
	// Problems are variants whose data fields differ from the English file.
	Problems []string `json:"problems"`
}

// action matches a template action ({{...}}); pushKey a push.json field name.
var (
	action  = regexp.MustCompile(`\{\{.*?\}\}`)
	pushKey = regexp.MustCompile(`"(title|body)"\s*:`)
	letter  = regexp.MustCompile(`\pL`)
)

// hasWords reports whether a template file has text of its own to translate.
// A file made only of data ("{{.Data.title}}") reads the same in every
// language and needs no variant.
func hasWords(file, source string) bool {
	text := action.ReplaceAllString(source, "")
	if file == "push.json" {
		text = pushKey.ReplaceAllString(text, "")
	}
	return letter.MatchString(text)
}

// dataField matches .Data.name in a template.
var dataField = regexp.MustCompile(`\.Data\.(\w+)`)

func dataFields(source string) []string {
	seen := map[string]bool{}
	for _, match := range dataField.FindAllStringSubmatch(source, -1) {
		seen[match[1]] = true
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Coverage checks every template's language variants: each file with words
// of its own needs <file>.<code><ext> for each language in codes, using the
// same data fields as the English file. Only templates with a gap are
// returned.
func (r *Registry) Coverage(codes []string) ([]TemplateCoverage, error) {
	var out []TemplateCoverage
	for _, name := range r.Names() {
		fsys, _ := r.template(name)
		for _, code := range codes {
			report := TemplateCoverage{Template: name, Language: code, Missing: []string{}, Problems: []string{}}
			for _, file := range templateFiles {
				base, err := fs.ReadFile(fsys, file)
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				if err != nil {
					return nil, err
				}
				if !hasWords(file, string(base)) {
					continue
				}
				ext := path.Ext(file)
				variant := strings.TrimSuffix(file, ext) + "." + code + ext
				translated, err := fs.ReadFile(fsys, variant)
				if errors.Is(err, fs.ErrNotExist) {
					report.Missing = append(report.Missing, file)
					continue
				}
				if err != nil {
					return nil, err
				}
				if want, got := dataFields(string(base)), dataFields(string(translated)); !slices.Equal(want, got) {
					report.Problems = append(report.Problems, variant+": data fields differ from "+file+
						" (want "+strings.Join(want, ", ")+"; got "+strings.Join(got, ", ")+")")
				}
			}
			if len(report.Missing) > 0 || len(report.Problems) > 0 {
				out = append(out, report)
			}
		}
	}
	return out, nil
}
