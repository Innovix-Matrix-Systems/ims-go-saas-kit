package sms

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
)

func TestNewSelectsTheDriver(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for driver, want := range map[string]string{"log": "log", "": "log", "twilio": "twilio"} {
		got, err := New(config.SMS{Driver: driver}, logger, false)
		if err != nil || got.Name() != want {
			t.Errorf("New(%q) = %v, %v; want %s", driver, got, err, want)
		}
	}
	if _, err := New(config.SMS{Driver: "carrier-pigeon"}, logger, false); err == nil {
		t.Error("New(unknown) error = nil")
	}
}

func TestTwilioDriverSends(t *testing.T) {
	var got url.Values
	var user, pass string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2010-04-01/Accounts/AC123/Messages.json" {
			http.NotFound(w, r)
			return
		}
		user, pass, _ = r.BasicAuth()
		_ = r.ParseForm()
		got = r.PostForm
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"sid":"SM42"}`)
	}))
	defer server.Close()

	driver := NewTwilioDriver(config.Twilio{AccountSID: "AC123", AuthToken: "tok", From: "+15005550006"}, server.Client()).
		WithBaseURL(server.URL)
	sid, err := driver.Send(context.Background(), Message{To: "+8801712345678", Body: "123456 is your code"})
	if err != nil || sid != "SM42" {
		t.Fatalf("Send() = %q, %v", sid, err)
	}
	if user != "AC123" || pass != "tok" || got.Get("To") != "+8801712345678" || got.Get("From") != "+15005550006" || got.Get("Body") != "123456 is your code" {
		t.Fatalf("request auth=%s:%s form=%v", user, pass, got)
	}
}

func TestTwilioDriverReportsErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"code":21211,"message":"The 'To' number is not a valid phone number."}`)
	}))
	defer server.Close()

	_, err := NewTwilioDriver(config.Twilio{AccountSID: "AC1", AuthToken: "t", From: "+1"}, server.Client()).
		WithBaseURL(server.URL).Send(context.Background(), Message{To: "nope", Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "21211") {
		t.Fatalf("Send() error = %v, want the Twilio error code", err)
	}
}
