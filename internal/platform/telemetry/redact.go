// Package telemetry captures redacted HTTP, SQL, cache, queue and log activity
// for the developer dashboard. Capture is best-effort: entries travel through
// bounded channels and are dropped (counted) under backpressure so a slow
// database never stalls a request.
package telemetry

import (
	"encoding/json"
	"net/http"
	"strings"
)

const redactedPlaceholder = "[redacted]"

// defaultSensitiveKeys are redacted in headers and structured payloads no
// matter how telemetry is configured.
var defaultSensitiveKeys = []string{
	"authorization",
	"cookie",
	"set-cookie",
	"x-maintenance-bypass",
	"password",
	"password_confirmation",
	"current_password",
	"token",
	"access_token",
	"refresh_token",
	"id_token",
	"code_verifier",
	"otp",
	"secret",
	"client_secret",
	"secret_key",
	"private_key",
	"api_key",
	"apikey",
	"x-api-key",
	"remember_token",
	"credit_card",
	"card_number",
	"cvv",
	"pin",
}

// Redactor removes sensitive values before anything is persisted.
type Redactor struct {
	keys map[string]struct{}
}

// NewRedactor builds a redactor from the default key list plus extras from
// DEV_DASHBOARD_REDACT_KEYS.
func NewRedactor(extra []string) *Redactor {
	keys := make(map[string]struct{}, len(defaultSensitiveKeys)+len(extra))
	for _, key := range defaultSensitiveKeys {
		keys[key] = struct{}{}
	}
	for _, key := range extra {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if normalized != "" {
			keys[normalized] = struct{}{}
		}
	}
	return &Redactor{keys: keys}
}

// Sensitive reports whether a key name is on the redaction list.
func (r *Redactor) Sensitive(key string) bool {
	_, ok := r.keys[strings.ToLower(key)]
	return ok
}

// Value redacts sensitive map keys recursively (maps and slices).
func (r *Redactor) Value(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if r.Sensitive(key) {
				out[key] = redactedPlaceholder
				continue
			}
			out[key] = r.Value(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = r.Value(item)
		}
		return out
	default:
		return value
	}
}

// AnyJSON marshals a redacted copy of the value.
func (r *Redactor) AnyJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(r.Value(value))
	if err != nil {
		return nil
	}
	return encoded
}

// JSON parses and redacts a JSON body. Non-JSON bodies are never stored raw
// (form payloads may carry secrets), so they collapse to the placeholder.
func (r *Redactor) JSON(body []byte) json.RawMessage {
	if len(body) == 0 {
		return nil
	}
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return json.RawMessage(`"` + redactedPlaceholder + `"`)
	}
	return r.AnyJSON(parsed)
}

func (r *Redactor) mentionsSensitive(sql string) bool {
	lower := strings.ToLower(sql)
	for key := range r.keys {
		if len(key) >= 4 && strings.Contains(lower, key) {
			return true
		}
	}
	return false
}

// looksLikeSecret catches bcrypt/argon hashes regardless of the statement.
func looksLikeSecret(value string) bool {
	return strings.HasPrefix(value, "$2a$") || strings.HasPrefix(value, "$2b$") ||
		strings.HasPrefix(value, "$2y$") || strings.HasPrefix(value, "$argon2")
}

// Headers copies headers with sensitive values replaced.
func (r *Redactor) Headers(header http.Header) map[string][]string {
	out := make(map[string][]string, len(header))
	for key, values := range header {
		if r.Sensitive(key) {
			out[key] = []string{redactedPlaceholder}
			continue
		}
		copied := make([]string, len(values))
		copy(copied, values)
		out[key] = copied
	}
	return out
}

// Args redacts SQL argument values. pgx passes positional scalars, so key
// names are not available: when the statement mentions a sensitive column
// (password, token, secret, ...) every string argument is redacted, and
// anything that looks like a password hash is redacted everywhere (H15).
func (r *Redactor) Args(sql string, args []any) json.RawMessage {
	if len(args) == 0 {
		return nil
	}
	sensitiveStatement := r.mentionsSensitive(sql)
	cleaned := make([]any, len(args))
	for i, arg := range args {
		if text, ok := arg.(string); ok && (sensitiveStatement || looksLikeSecret(text)) {
			cleaned[i] = redactedPlaceholder
			continue
		}
		cleaned[i] = r.Value(arg)
	}
	encoded, err := json.Marshal(cleaned)
	if err != nil {
		return nil
	}
	return encoded
}
