// Package sms is the SMS transport behind the delivery system's sms channel
// (internal/platform/delivery): the driver abstraction with a log driver
// (default) and a Twilio driver. Code sends SMS through delivery.Service,
// not through a driver directly.
package sms

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
)

// Message is one outbound SMS.
type Message struct {
	To   string // E.164, e.g. +8801712345678
	Body string
}

// Driver delivers SMS. Send returns the provider's message id ("" when it
// has none).
type Driver interface {
	Send(ctx context.Context, message Message) (string, error)
	Name() string
}

// New selects the configured SMS driver.
func New(cfg config.SMS, logger *slog.Logger, debug bool) (Driver, error) {
	switch cfg.Driver {
	case "log", "":
		return &LogDriver{logger: logger, debug: debug}, nil
	case "twilio":
		return NewTwilioDriver(cfg.Twilio, nil), nil
	default:
		return nil, fmt.Errorf("sms: unsupported driver %q", cfg.Driver)
	}
}

// LogDriver writes SMS to the structured log. Bodies (which may contain
// one-time codes) are only logged with debug enabled outside production.
type LogDriver struct {
	logger *slog.Logger
	debug  bool
}

// Name implements Driver.
func (d *LogDriver) Name() string { return "log" }

// Send implements Driver by logging the message.
func (d *LogDriver) Send(_ context.Context, message Message) (string, error) {
	attrs := []any{"to", message.To, "length", len([]rune(message.Body))}
	if d.debug {
		attrs = append(attrs, "body", message.Body)
	}
	d.logger.Info("sms delivered (log driver)", attrs...)
	return "", nil
}

// TwilioDriver sends SMS through Twilio's Messages API.
type TwilioDriver struct {
	cfg     config.Twilio
	client  *http.Client
	baseURL string
}

// NewTwilioDriver creates the Twilio driver; a nil client uses a 15 s
// timeout.
func NewTwilioDriver(cfg config.Twilio, client *http.Client) *TwilioDriver {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second, Transport: tracing.Transport(nil)}
	}
	return &TwilioDriver{cfg: cfg, client: client, baseURL: "https://api.twilio.com"}
}

// WithBaseURL points the driver at another host (tests).
func (d *TwilioDriver) WithBaseURL(baseURL string) *TwilioDriver {
	d.baseURL = strings.TrimRight(baseURL, "/")
	return d
}

// Name implements Driver.
func (d *TwilioDriver) Name() string { return "twilio" }

// Send implements Driver.
func (d *TwilioDriver) Send(ctx context.Context, message Message) (string, error) {
	form := url.Values{"To": {message.To}, "From": {d.cfg.From}, "Body": {message.Body}}
	endpoint := fmt.Sprintf("%s/2010-04-01/Accounts/%s/Messages.json", d.baseURL, url.PathEscape(d.cfg.AccountSID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("sms: twilio request: %w", err)
	}
	req.SetBasicAuth(d.cfg.AccountSID, d.cfg.AuthToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sms: twilio send: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	var answer struct {
		SID     string `json:"sid"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &answer)
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("sms: twilio answered %d: %s (code %d)", res.StatusCode, answer.Message, answer.Code)
	}
	return answer.SID, nil
}
