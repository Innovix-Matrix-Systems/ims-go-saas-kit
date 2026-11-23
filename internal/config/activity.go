package config

import "errors"

// Activity log settings (ACTIVITY_*).

// Activity holds the activity-log settings.
type Activity struct {
	// RetentionDays deletes entries older than this many days (0 = keep
	// forever).
	RetentionDays int
}

func (c *Config) loadActivity() error {
	days, err := getInt("ACTIVITY_RETENTION_DAYS", 365)
	if err != nil {
		return err
	}
	if days < 0 {
		return errors.New("ACTIVITY_RETENTION_DAYS must not be negative")
	}
	c.Activity = Activity{RetentionDays: days}
	return nil
}
