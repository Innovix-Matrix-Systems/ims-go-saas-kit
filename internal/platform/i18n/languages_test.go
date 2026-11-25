package i18n

import (
	"strings"
	"testing"
)

func TestBuiltinLanguagesAreValid(t *testing.T) {
	languages := Builtin()
	if languages[0].Code != Reference {
		t.Fatalf("Builtin()[0] = %q, want the reference language %q", languages[0].Code, Reference)
	}
	for _, lang := range languages {
		if problem := validLanguage(lang); problem != "" {
			t.Errorf("language %q: %s", lang.Code, problem)
		}
	}
}

func TestNewRefusesBadLists(t *testing.T) {
	en := Builtin()[0]
	cases := map[string][]Language{
		"empty":            nil,
		"reference second": {{Code: "xx", Label: "X", English: "X", Intl: "en", Dir: LTR}, en},
		"duplicate":        {en, en},
		"bad direction":    {en, {Code: "xx", Label: "X", English: "X", Intl: "en", Dir: "up"}},
		"long code":        {en, {Code: "xyz", Label: "X", English: "X", Intl: "en", Dir: LTR}},
	}
	for name, languages := range cases {
		if _, err := New(languages); err == nil {
			t.Errorf("%s: New() = nil error, want an error", name)
		}
	}
}

func TestSupportNarrowsAndChecks(t *testing.T) {
	translator, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := translator.Support(nil, "en"); err != nil {
		t.Fatalf("Support(empty) = %v", err)
	}
	if got := len(translator.Supported()); got != len(Builtin()) {
		t.Errorf("empty list supports %d languages, want every registered one (%d)", got, len(Builtin()))
	}
	if err := translator.Support([]string{"en", "zz"}, "en"); err == nil || !strings.Contains(err.Error(), "zz") {
		t.Errorf("Support with an unknown code = %v, want an error naming it", err)
	}
	if err := translator.Support([]string{"bn"}, "en"); err == nil {
		t.Error("Support without the default language = nil error")
	}
	if err := translator.Support([]string{"en"}, "en"); err != nil {
		t.Fatal(err)
	}
	if translator.IsSupported("bn") || !translator.IsSupported("EN") {
		t.Errorf("after Support(en): IsSupported(bn) = %t, IsSupported(EN) = %t", translator.IsSupported("bn"), translator.IsSupported("EN"))
	}
}

func TestFormatNumberUsesTheLanguagesDigits(t *testing.T) {
	cases := []struct {
		lang Language
		want string
	}{
		{Language{Code: "en", Intl: "en", Digits: "latn"}, "1,234,567"},
		{Language{Code: "bn", Intl: "bn-BD", Digits: "beng"}, "১২,৩৪,৫৬৭"},
		{Language{Code: "de", Intl: "de", Digits: "latn"}, "1.234.567"},
		{Language{Code: "ar", Intl: "ar", Digits: "latn"}, "1,234,567"},
	}
	for _, tc := range cases {
		if got := tc.lang.FormatNumber(1234567); got != tc.want {
			t.Errorf("%s FormatNumber = %q, want %q", tc.lang.Code, got, tc.want)
		}
	}
}
