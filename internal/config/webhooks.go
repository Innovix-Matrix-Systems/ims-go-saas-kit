package config

import (
	"fmt"
	"time"
)

// Outbound webhooks (WEBHOOK_*).

// Webhooks holds the outbound webhook settings.
type Webhooks struct {
	// AllowPrivate lets endpoints point at private, loopback and link-local
	// addresses and use plain http (default: on in local development only).
	AllowPrivate bool
	// Timeout bounds one delivery attempt (default 10s).
	Timeout time.Duration
	// DisableAfter disables an endpoint after this many abandoned deliveries
	// in a row (default 5).
	DisableAfter int
	// RetentionDays keeps delivery logs this long (default 30; 0 = forever).
	RetentionDays int
}

func (c *Config) loadWebhooks() error {
	allowPrivate, err := getBool("WEBHOOK_ALLOW_PRIVATE", c.IsLocal())
	if err != nil {
		return err
	}
	timeout, err := getDuration("WEBHOOK_TIMEOUT", 10*time.Second)
	if err != nil {
		return err
	}
	disableAfter, err := getInt("WEBHOOK_DISABLE_AFTER", 5)
	if err != nil {
		return err
	}
	retention, err := getInt("WEBHOOK_RETENTION_DAYS", 30)
	if err != nil {
		return err
	}
	if disableAfter < 1 || retention < 0 || timeout <= 0 {
		return fmt.Errorf("WEBHOOK_DISABLE_AFTER must be at least 1, WEBHOOK_RETENTION_DAYS zero or more and WEBHOOK_TIMEOUT positive")
	}
	c.Webhooks = Webhooks{AllowPrivate: allowPrivate, Timeout: timeout, DisableAfter: disableAfter, RetentionDays: retention}
	return nil
}
