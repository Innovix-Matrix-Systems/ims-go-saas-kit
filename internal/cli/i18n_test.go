package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestI18nCoverageCommand(t *testing.T) {
	t.Setenv("APP_ENV_FILE", "")
	out, err := run(t, "i18n:coverage")
	if err != nil {
		t.Fatalf("i18n:coverage: %v\n%s", err, out)
	}
	if !strings.Contains(out, "bn (Bangla)") || !strings.Contains(out, "Every language is complete.") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestI18nMissingCommand(t *testing.T) {
	t.Setenv("APP_ENV_FILE", "")
	out, err := run(t, "i18n:missing", "--locale", "bn", "--json")
	if err != nil {
		t.Fatalf("i18n:missing: %v\n%s", err, out)
	}
	var report struct {
		Language struct {
			Code string `json:"code"`
		} `json:"language"`
		PluralForms []string `json:"plural_forms"`
		Keys        []any    `json:"keys"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if report.Language.Code != "bn" || len(report.Keys) != 0 || len(report.PluralForms) == 0 {
		t.Errorf("report = %+v", report)
	}
	if _, err := run(t, "i18n:missing", "--locale", "zz"); err == nil {
		t.Error("an unknown language = nil error")
	}
}
