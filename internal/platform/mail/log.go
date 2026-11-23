package mail

import (
	"context"
	"log/slog"
)

// LogDriver writes mail to the structured log. Message bodies (which may
// contain OTP codes) are only logged with debug enabled outside production.
type LogDriver struct {
	logger *slog.Logger
	debug  bool
	from   string
}

// Name implements Driver.
func (d *LogDriver) Name() string { return "log" }

// Send implements Driver by logging the message.
func (d *LogDriver) Send(_ context.Context, message Message) error {
	attrs := []any{
		"from", d.from,
		"to", message.To,
		"subject", message.Subject,
	}
	if d.debug {
		attrs = append(attrs, "body", message.Text)
	}
	d.logger.Info("mail delivered (log driver)", attrs...)
	return nil
}
