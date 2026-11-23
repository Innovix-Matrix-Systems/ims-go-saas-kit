package app_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/testkit"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestNoSecretsInLogs is the redaction audit: passwords (success and failure)
// and issued tokens must never appear in application logs.
func TestNoSecretsInLogs(t *testing.T) {
	t.Setenv("APP_DEBUG", "false")

	captured := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(captured, &slog.HandlerOptions{Level: slog.LevelDebug}))
	router := testkit.New(t, testkit.WithLogger(logger)).Router

	// Successful login (issued token must not be logged).
	rec := apiRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"superadmin@ims.com","password":"secret123","device":"redact-device"}`, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body = %s", rec.Code, rec.Body.String())
	}
	token := extractToken(t, rec.Body.Bytes())

	// Failed login with a distinctive wrong password.
	const wrongPassword = "wrongpass-9f3a1b"
	fail := apiRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"superadmin@ims.com","password":"`+wrongPassword+`","device":"redact-device"}`, "", nil)
	if fail.Code == http.StatusOK {
		t.Fatalf("failed login unexpectedly succeeded")
	}

	logs := captured.String()
	if logs == "" {
		t.Fatal("no logs captured; test is not exercising logging")
	}
	for name, secret := range map[string]string{
		"configured password": "secret123",
		"wrong password":      wrongPassword,
		"issued token":        token,
	} {
		if secret == "" {
			continue
		}
		if bytes.Contains([]byte(logs), []byte(secret)) {
			t.Errorf("%s leaked into logs", name)
		}
	}
}

func extractToken(t *testing.T, body []byte) string {
	t.Helper()
	var parsed struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode login body: %v", err)
	}
	return parsed.Data.Token
}
