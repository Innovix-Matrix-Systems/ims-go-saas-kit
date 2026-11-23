package config

import "time"

// Duplicate-request protection (DEDUPE_*, IDEMPOTENCY_*): the guard against
// the same write running twice, and Idempotency-Key replays.

// Dedupe holds the duplicate-request protection settings.
type Dedupe struct {
	// Enabled turns the guard and Idempotency-Key handling on (default true).
	Enabled bool
	// Window refuses a repeat of a succeeded write for this long (default 3s).
	Window time.Duration
	// KeyTTL keeps an Idempotency-Key response this long (default 24h).
	KeyTTL time.Duration
}

func (c *Config) loadDedupe() error {
	enabled, err := getBool("DEDUPE_ENABLED", true)
	if err != nil {
		return err
	}
	window, err := getDuration("DEDUPE_WINDOW", 3*time.Second)
	if err != nil {
		return err
	}
	keyTTL, err := getDuration("IDEMPOTENCY_TTL", 24*time.Hour)
	if err != nil {
		return err
	}
	c.Dedupe = Dedupe{Enabled: enabled, Window: window, KeyTTL: keyTTL}
	return nil
}
