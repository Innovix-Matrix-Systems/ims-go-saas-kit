package config

import (
	"errors"
	"time"
)

// Maintenance holds the maintenance-mode settings. The message,
// retry and bypass secret are chosen per `ims down`, not here.
type Maintenance struct {
	// CheckInterval is how often each process re-reads the maintenance state
	// from the database (MAINTENANCE_CHECK_INTERVAL, default 5s).
	CheckInterval time.Duration
}

func (c *Config) loadMaintenance() error {
	interval, err := getDuration("MAINTENANCE_CHECK_INTERVAL", 5*time.Second)
	if err != nil {
		return err
	}
	if interval <= 0 {
		return errors.New("MAINTENANCE_CHECK_INTERVAL must be greater than zero")
	}
	c.Maintenance = Maintenance{CheckInterval: interval}
	return nil
}
