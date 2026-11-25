package settings

import (
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/i18n"
)

func TestCheckChoice(t *testing.T) {
	key := Key{Name: "x.colour", Choices: func() []string { return []string{"red", "blue"} }}
	for _, ok := range []any{"red", "blue", ""} {
		if err := key.CheckChoice(ok); err != nil {
			t.Errorf("CheckChoice(%q) = %v, want nil", ok, err)
		}
	}
	if err := key.CheckChoice("green"); err == nil {
		t.Error("CheckChoice(green) accepted a value outside the choices")
	}
	if err := (Key{Name: "free"}).CheckChoice("anything"); err != nil {
		t.Errorf("a key without choices refused a value: %v", err)
	}
}

// The organization's language accepts the languages the application offers,
// read when the value is written (the list follows APP_SUPPORTED_LOCALES).
func TestDefaultLocaleChoicesFollowTheOfferedLanguages(t *testing.T) {
	key, ok := Lookup("general.default_locale")
	if !ok || key.Choices == nil {
		t.Fatal("general.default_locale has no choices")
	}
	translator, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := translator.Support([]string{"en", "bn"}, "en"); err != nil {
		t.Fatal(err)
	}
	i18n.SetDefault(translator)
	t.Cleanup(func() { i18n.SetDefault(nil) })

	if err := key.CheckChoice("bn"); err != nil {
		t.Errorf("bn refused: %v", err)
	}
	if err := key.CheckChoice("de"); err == nil {
		t.Error("de accepted although it is not offered")
	}
}
