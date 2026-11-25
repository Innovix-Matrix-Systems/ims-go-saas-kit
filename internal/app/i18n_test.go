package app

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/i18n"
)

func testTranslator(t *testing.T) *i18n.Translator {
	t.Helper()
	translator, err := newTranslator(&config.Config{App: config.App{Locale: i18n.Reference, FallbackLocale: i18n.Reference}})
	if err != nil {
		t.Fatalf("newTranslator() error = %v", err)
	}
	return translator
}

// TestTranslationsCoverEveryLanguage is the coverage check (like privacy's
// TestCoverage): every registered language has every key of every bundle,
// with the same placeholders and its plural forms, and every template has a
// variant per language. `ims i18n:coverage` prints the same report.
func TestTranslationsCoverEveryLanguage(t *testing.T) {
	translator := testTranslator(t)
	for _, report := range translator.Coverage() {
		if !report.Complete() {
			t.Errorf("%s (%s): missing %v; extra %v; problems %v", report.Language.Code, report.Language.English, report.Missing, report.Extra, report.Problems)
		}
	}
	templates, err := newTemplateRegistry()
	if err != nil {
		t.Fatal(err)
	}
	codes := translator.Codes()[1:]
	reports, err := templates.Coverage(codes)
	if err != nil {
		t.Fatal(err)
	}
	for _, report := range reports {
		t.Errorf("template %s (%s): missing variants of %v; problems %v", report.Template, report.Language, report.Missing, report.Problems)
	}
}

// TestAppLanguagesMatchTheAPI keeps the client app's built-in language list
// (its offline fallback) equal to the API's.
func TestAppLanguagesMatchTheAPI(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "app", "src", "i18n", "languages.json"))
	if err != nil {
		t.Fatal(err)
	}
	var app []i18n.Language
	if err := json.Unmarshal(raw, &app); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(app, languages()) {
		t.Errorf("web/app/src/i18n/languages.json = %+v\nwant the API's list (internal/app/i18n.go) = %+v", app, languages())
	}
}

func TestEveryTemplateRendersItsSampleInEveryLanguage(t *testing.T) {
	translator := testTranslator(t)
	templates, err := newTemplateRegistry()
	if err != nil {
		t.Fatal(err)
	}
	brand := delivery.Brand{Name: "Acme", Color: "#112233"}
	for _, name := range templates.Names() {
		sample, err := templates.Sample(name)
		if err != nil {
			t.Fatalf("Sample(%q) error = %v", name, err)
		}
		for _, channel := range templates.Channels(name) {
			for _, code := range translator.Codes() {
				content, err := templates.Render(name, channel, []string{code}, delivery.View{Data: sample, Brand: brand})
				if err != nil {
					t.Errorf("Render(%s, %s, %s) error = %v", name, channel, code, err)
					continue
				}
				if strings.Contains(content.Text+content.Subject, "<no value>") {
					t.Errorf("Render(%s, %s, %s) left a value out", name, channel, code)
				}
			}
		}
	}
}

func TestDurationsSpeakTheLanguage(t *testing.T) {
	testTranslator(t)
	templates, err := newTemplateRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for code, want := range map[string]string{"en": "1 hour 30 minutes", "bn": "১ ঘণ্টা ৩০ মিনিট"} {
		content, err := templates.Render("otp-code", delivery.Mail, []string{code},
			delivery.View{Data: map[string]any{"code": "482913", "minutes": 90}, Brand: delivery.Brand{Name: "Acme"}})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(content.Text, want) {
			t.Errorf("%s otp-code text %q does not contain %q", code, content.Text, want)
		}
	}
}

// TestNoHardCodedLanguageCodes keeps the code free of language codes: a
// language is a descriptor plus catalog files, so a quoted code ("bn") in Go
// source is a bug waiting for the next language. The registry is the one
// place allowed to name them; a string that only looks like a code says so on
// its line ("not a language code").
func TestNoHardCodedLanguageCodes(t *testing.T) {
	translator := testTranslator(t)
	codes := translator.Codes()[1:] // the reference language is named by i18n.Reference
	pattern := regexp.MustCompile(`"(` + strings.Join(codes, "|") + `)"`)
	allowed := map[string]bool{filepath.FromSlash("platform/i18n/languages.go"): true}
	root := filepath.Join("..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "dbgen", "contract", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || allowed[rel] {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(source), "\n") {
			if pattern.MatchString(line) && !strings.Contains(line, "not a language code") {
				t.Errorf("%s:%d names a language code: %s (read it from the language registry)", rel, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
