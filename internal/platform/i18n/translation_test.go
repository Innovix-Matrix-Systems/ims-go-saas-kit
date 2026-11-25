package i18n

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

func TestPlatformCatalogsAreComplete(t *testing.T) {
	translator, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	for _, report := range translator.Coverage() {
		if !report.Complete() {
			t.Errorf("%s: missing %v, extra %v, problems %v", report.Language.Code, report.Missing, report.Extra, report.Problems)
		}
	}
	for _, key := range []string{"http-statuses.404", "validation.required", "validation.min.string", "validation.attributes.email", "validation.failed", "common.duration.days"} {
		if Tr(Reference, key, Count(Reference, 2, map[string]any{"Attribute": "x", "Min": "1"})) == "" {
			t.Errorf("platform bundle is missing %q", key)
		}
	}
}

func TestTranslationsRenderPerLocale(t *testing.T) {
	translator, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	enCtx := context.WithValue(context.Background(), localizerKey, translator.Localizer("en"))
	bnCtx := context.WithValue(context.Background(), localizerKey, translator.Localizer("bn"))

	english := T(enCtx, "validation.required", map[string]any{"Attribute": "email"})
	bengali := T(bnCtx, "validation.required", map[string]any{"Attribute": "email"})
	if !strings.Contains(english, "required") {
		t.Errorf("en validation.required = %q", english)
	}
	if bengali == "" || bengali == english {
		t.Errorf("bn validation.required = %q (en was %q)", bengali, english)
	}
	if strings.Contains(bengali, "{{") {
		t.Errorf("bn translation leaked a template placeholder: %q", bengali)
	}
	if missing := T(enCtx, "no.such.key", nil); missing != "" {
		t.Errorf("missing key translation = %q, want empty", missing)
	}
	if fallback := TOr(enCtx, "no.such.key", "fallback", nil); fallback != "fallback" {
		t.Errorf("TOr fallback = %q", fallback)
	}
}

func TestPluralsFollowTheLanguage(t *testing.T) {
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		code string
		n    int64
		want string
	}{
		{"en", 1, "1 day"},
		{"en", 3, "3 days"},
		{"en", 1200, "1,200 days"},
		{"bn", 3, "৩ দিন"},
	}
	for _, tc := range cases {
		if got := Tr(tc.code, "common.duration.days", Count(tc.code, tc.n, nil)); got != tc.want {
			t.Errorf("%s %d days = %q, want %q", tc.code, tc.n, got, tc.want)
		}
	}
}

func TestFallbackLocalizerWithoutMiddleware(t *testing.T) {
	// No locale in the context: the reference catalog is used.
	message := T(context.Background(), "validation.required", map[string]any{"Attribute": "name"})
	if !strings.Contains(message, "name") || !strings.Contains(message, "required") {
		t.Errorf("fallback translation = %q", message)
	}
}

func bundleOf(owner string, namespaces []string, files map[string]string) Bundle {
	fsys := fstest.MapFS{}
	for name, body := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return Bundle{Owner: owner, Namespaces: namespaces, FS: fsys}
}

func TestBundlesOwnTheirNamespaces(t *testing.T) {
	langs := Builtin()
	shop := bundleOf("shop", []string{"shop"}, map[string]string{
		"shop/en.json": `{"shop.title": "Shop", "shop.items": {"one": "{{.Count}} item", "other": "{{.Count}} items"}}`,
		"shop/bn.json": `{"shop.title": "দোকান"}`,
	})
	if _, err := New(langs, Platform(), shop, bundleOf("other", []string{"shop"}, nil)); err == nil || !strings.Contains(err.Error(), "owned by") {
		t.Errorf("a second owner of a namespace = %v, want refused", err)
	}
	stray := bundleOf("stray", []string{"stray"}, map[string]string{"stray/en.json": `{"shop.title": "x"}`})
	if _, err := New(langs, Platform(), stray); err == nil || !strings.Contains(err.Error(), "outside the namespace") {
		t.Errorf("a key outside the bundle's namespace = %v, want refused", err)
	}
	noOther := bundleOf("x", []string{"x"}, map[string]string{"x/en.json": `{"x.n": {"one": "one"}}`})
	if _, err := New(langs, Platform(), noOther); err == nil {
		t.Error("a plural without an other form = nil error")
	}

	translator, err := New(langs, Platform(), shop)
	if err != nil {
		t.Fatal(err)
	}
	var report Coverage
	for _, r := range translator.Coverage() {
		if r.Language.Code == "bn" {
			report = r
		}
	}
	if len(report.Missing) != 1 || report.Missing[0] != "shop.items" {
		t.Errorf("bn missing = %v, want [shop.items]", report.Missing)
	}
	missing := translator.Missing("bn")
	if len(missing) != 1 || missing[0].Owner != "shop" || missing[0].English["other"] != "{{.Count}} items" {
		t.Errorf("Missing(bn) = %+v", missing)
	}
}

func TestOverridesReplaceExistingKeysOnly(t *testing.T) {
	langs := Builtin()
	overrides := Overrides(fstest.MapFS{
		"validation/en.json": {Data: []byte(`{"validation.required": "Please fill in {{.Attribute}}."}`)},
	})
	translator, err := New(langs, Platform(), overrides)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), localizerKey, translator.Localizer("en"))
	if got := T(ctx, "validation.required", map[string]any{"Attribute": "name"}); got != "Please fill in name." {
		t.Errorf("overridden text = %q", got)
	}
	if got := translator.Overridden()["en"]; len(got) != 1 || got[0] != "validation.required" {
		t.Errorf("Overridden() = %v", got)
	}
	adds := Overrides(fstest.MapFS{"validation/en.json": {Data: []byte(`{"validation.brand_new": "x"}`)}})
	if _, err := New(langs, Platform(), adds); err == nil {
		t.Error("an override that adds a key = nil error, want refused")
	}
	unknown := Overrides(fstest.MapFS{"nobody/en.json": {Data: []byte(`{"nobody.x": "x"}`)}})
	if _, err := New(langs, Platform(), unknown); err == nil {
		t.Error("an override of an unregistered namespace = nil error, want refused")
	}
}

func TestCoverageFindsPlaceholderAndPluralProblems(t *testing.T) {
	ar := Language{Code: "ar", Label: "العربية", English: "Arabic", Intl: "ar", Dir: RTL, Digits: "latn"}
	langs := []Language{Builtin()[0], ar}
	shop := bundleOf("shop", []string{"shop"}, map[string]string{
		"shop/en.json": `{"shop.hello": "Hello {{.Name}}", "shop.items": {"one": "{{.Count}} item", "other": "{{.Count}} items"}}`,
		"shop/ar.json": `{"shop.hello": "مرحبا", "shop.items": {"one": "عنصر", "other": "{{.Count}} عناصر"}}`,
	})
	translator, err := New(langs, shop)
	if err != nil {
		t.Fatal(err)
	}
	report := translator.Coverage()[0]
	if len(report.Problems) != 2 {
		t.Fatalf("problems = %+v, want a dropped placeholder and missing Arabic plural forms", report.Problems)
	}
	if !strings.Contains(report.Problems[0].Detail, "placeholders") || !strings.Contains(report.Problems[1].Detail, "zero, two, few, many") {
		t.Errorf("problems = %+v", report.Problems)
	}
}
