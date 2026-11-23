package config

import "fmt"

// Delivery system settings (DELIVERY_*): the log of messages sent by mail,
// SMS and push.

// Delivery holds the delivery system settings.
type Delivery struct {
	// RetentionDays is how long rows stay in the delivery log (0 = forever).
	RetentionDays int
}

func (c *Config) loadDelivery() error {
	days, err := getInt("DELIVERY_RETENTION_DAYS", 30)
	if err != nil {
		return err
	}
	if days < 0 {
		return fmt.Errorf("DELIVERY_RETENTION_DAYS must be zero or more, got %d", days)
	}
	c.Delivery = Delivery{RetentionDays: days}
	return nil
}
