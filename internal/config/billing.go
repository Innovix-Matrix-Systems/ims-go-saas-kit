package config

import (
	"fmt"
	"time"
)

// Billing and payment gateways (BILLING_*, BKASH_*, NAGAD_*, BANK_*). The
// currency is data, not configuration: the currencies table (staff console).

// Billing holds subscription and payment gateway settings.
type Billing struct {
	// SuspendOnExpiry suspends a tenant (is_active false) once its
	// subscription has been expired for SuspendGrace with nothing current or
	// pending. Off by default: suspending customers is a business decision.
	SuspendOnExpiry bool
	SuspendGrace    time.Duration
	Bkash           Bkash
	Nagad           Nagad
	Bank            BankTransfer
}

// Bkash holds bKash gateway credentials.
type Bkash struct {
	BaseURL     string
	AppKey      string
	AppSecret   string
	Username    string
	Password    string
	CallbackURL string
}

// Nagad holds Nagad gateway credentials (integration not yet implemented).
type Nagad struct {
	BaseURL        string
	MerchantID     string
	MerchantNumber string
	PublicKey      string
	PrivateKey     string
}

// BankTransfer holds manual bank-transfer instructions.
type BankTransfer struct {
	AccountName   string
	AccountNumber string
	BankName      string
	Branch        string
}

func (c *Config) loadBilling() error {
	suspend, err := getBool("BILLING_SUSPEND_ON_EXPIRY", false)
	if err != nil {
		return err
	}
	graceDays, err := getInt("BILLING_SUSPEND_GRACE_DAYS", 3)
	if err != nil {
		return err
	}
	if graceDays < 0 {
		return fmt.Errorf("BILLING_SUSPEND_GRACE_DAYS must be zero or more, got %d", graceDays)
	}
	c.Billing = Billing{
		SuspendOnExpiry: suspend,
		SuspendGrace:    time.Duration(graceDays) * 24 * time.Hour,
		Bkash: Bkash{
			BaseURL:     getString("BKASH_BASE_URL", ""),
			AppKey:      getString("BKASH_APP_KEY", ""),
			AppSecret:   getString("BKASH_APP_SECRET", ""),
			Username:    getString("BKASH_USERNAME", ""),
			Password:    getString("BKASH_PASSWORD", ""),
			CallbackURL: getString("BKASH_CALLBACK_URL", ""),
		},
		Nagad: Nagad{
			BaseURL:        getString("NAGAD_BASE_URL", ""),
			MerchantID:     getString("NAGAD_MERCHANT_ID", ""),
			MerchantNumber: getString("NAGAD_MERCHANT_NUMBER", ""),
			PublicKey:      getString("NAGAD_PUBLIC_KEY", ""),
			PrivateKey:     getString("NAGAD_PRIVATE_KEY", ""),
		},
		Bank: BankTransfer{
			AccountName:   getString("BANK_TRANSFER_ACCOUNT_NAME", ""),
			AccountNumber: getString("BANK_TRANSFER_ACCOUNT_NUMBER", ""),
			BankName:      getString("BANK_TRANSFER_BANK_NAME", ""),
			Branch:        getString("BANK_TRANSFER_BRANCH", ""),
		},
	}
	return nil
}

// Configured reports whether the bKash credentials are complete.
func (b Bkash) Configured() bool {
	return b.BaseURL != "" && b.AppKey != "" && b.AppSecret != "" && b.Username != "" && b.Password != ""
}
