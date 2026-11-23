package config

// SMS driver settings (SMS_*, TWILIO_*).

// SMS holds the SMS driver settings.
type SMS struct {
	Driver string // log | twilio
	Twilio Twilio
	// DailyLimitPerTenant caps notification SMS per tenant per day
	// (SMS_DAILY_LIMIT_PER_TENANT, default 500; 0 = no cap).
	DailyLimitPerTenant int64
}

// Twilio holds the Twilio Messages API credentials.
type Twilio struct {
	AccountSID string
	AuthToken  string
	From       string // a Twilio number (E.164) or a messaging service SID
}

func (c *Config) loadSMS() error {
	limit, err := getInt64("SMS_DAILY_LIMIT_PER_TENANT", 500)
	if err != nil {
		return err
	}
	c.SMS = SMS{
		Driver: getString("SMS_DRIVER", "log"),
		Twilio: Twilio{
			AccountSID: getString("TWILIO_ACCOUNT_SID", ""),
			AuthToken:  getString("TWILIO_AUTH_TOKEN", ""),
			From:       getString("TWILIO_FROM", ""),
		},
		DailyLimitPerTenant: limit,
	}
	return nil
}
