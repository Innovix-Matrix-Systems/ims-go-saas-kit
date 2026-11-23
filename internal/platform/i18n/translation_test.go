package i18n

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func readCatalog(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return entries
}

func TestCatalogsHaveKeyParity(t *testing.T) {
	en := readCatalog(t, "locales/en.json")
	bn := readCatalog(t, "locales/bn.json")

	for key := range en {
		if _, ok := bn[key]; !ok {
			t.Errorf("bn catalog is missing key %q", key)
		}
	}
	for key := range bn {
		if _, ok := en[key]; !ok {
			t.Errorf("en catalog is missing key %q", key)
		}
	}
	for _, key := range []string{
		"messages.login.success",
		"messages.billing.usage_limit_exceeded",
		"http-statuses.404",
		"validation.required",
		"validation.min.string",
		"validation.attributes.email",
		"validation.failed",
	} {
		if _, ok := en[key]; !ok {
			t.Errorf("en catalog is missing expected key %q", key)
		}
	}
	if len(en) < 350 {
		t.Errorf("en catalog has %d keys, want at least 350", len(en))
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

	enTitle := T(enCtx, "http-statuses.404", nil)
	bnTitle := T(bnCtx, "http-statuses.404", nil)
	if enTitle != "Not Found" {
		t.Errorf("en http-statuses.404 = %q", enTitle)
	}
	if bnTitle == "" || bnTitle == enTitle {
		t.Errorf("bn http-statuses.404 = %q", bnTitle)
	}

	if missing := T(enCtx, "no.such.key", nil); missing != "" {
		t.Errorf("missing key translation = %q, want empty", missing)
	}
	fallback := TOr(enCtx, "no.such.key", "fallback", nil)
	if fallback != "fallback" {
		t.Errorf("TOr fallback = %q", fallback)
	}
}

func TestFallbackLocalizerWithoutMiddleware(t *testing.T) {
	// No locale in the context: the English catalog is used.
	message := T(context.Background(), "validation.required", map[string]any{"Attribute": "name"})
	if !strings.Contains(message, "name") || !strings.Contains(message, "required") {
		t.Errorf("fallback translation = %q", message)
	}
}
