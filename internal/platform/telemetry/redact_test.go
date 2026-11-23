package telemetry

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestRedactorSensitiveKeysAreCaseInsensitive(t *testing.T) {
	redactor := NewRedactor(nil)
	for _, key := range []string{"Authorization", "COOKIE", "Password", "X-Api-Key", "otp"} {
		if !redactor.Sensitive(key) {
			t.Errorf("Sensitive(%q) = false, want true", key)
		}
	}
	if redactor.Sensitive("email") {
		t.Error("Sensitive(email) = true, want false")
	}
}

func TestRedactorJSONRedactsNestedKeys(t *testing.T) {
	redactor := NewRedactor([]string{"patient_nid"})
	body := []byte(`{
		"email": "user@example.com",
		"password": "hunter2",
		"profile": {"authorization": "Bearer abc", "token": "xyz", "patient_nid": "1234567890"},
		"items": [{"card_number": "4111111111111111", "label": "card"}]
	}`)
	redacted := string(redactor.JSON(body))
	for _, secret := range []string{"hunter2", "Bearer abc", "xyz", "1234567890", "4111111111111111"} {
		if strings.Contains(redacted, secret) {
			t.Errorf("secret %q leaked in %s", secret, redacted)
		}
	}
	if !strings.Contains(redacted, "user@example.com") {
		t.Errorf("non-sensitive value was removed: %s", redacted)
	}
	if !strings.Contains(redacted, `"label":"card"`) {
		t.Errorf("non-sensitive nested value was removed: %s", redacted)
	}
}

func TestRedactorJSONNeverStoresNonJSONBodies(t *testing.T) {
	redactor := NewRedactor(nil)
	redacted := string(redactor.JSON([]byte("username=admin&password=hunter2")))
	if strings.Contains(redacted, "hunter2") {
		t.Fatalf("form body leaked: %s", redacted)
	}
	if redacted != `"[redacted]"` {
		t.Errorf("non-JSON body = %s, want placeholder", redacted)
	}
}

func TestRedactorHeaders(t *testing.T) {
	redactor := NewRedactor(nil)
	headers := http.Header{}
	headers.Set("Authorization", "Bearer secret-token")
	headers.Set("Cookie", "session=abc")
	headers.Set("Accept", "application/json")
	out := redactor.Headers(headers)
	if out["Authorization"][0] != redactedPlaceholder {
		t.Errorf("Authorization = %v", out["Authorization"])
	}
	if out["Cookie"][0] != redactedPlaceholder {
		t.Errorf("Cookie = %v", out["Cookie"])
	}
	if out["Accept"][0] != "application/json" {
		t.Errorf("Accept = %v", out["Accept"])
	}
}

func TestRedactorArgs(t *testing.T) {
	redactor := NewRedactor(nil)
	args := []any{"safe", map[string]any{"password": "hunter2", "id": 7}}
	encoded := string(redactor.Args("SELECT 1", args))
	if strings.Contains(encoded, "hunter2") {
		t.Fatalf("arg secret leaked: %s", encoded)
	}
	var decoded []any
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
		t.Fatalf("args not valid JSON: %v", err)
	}
	if decoded[0] != "safe" {
		t.Errorf("scalar arg = %v", decoded[0])
	}
}
