package domain

import (
	"context"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/money"
)

// InitiateResult is the driver outcome of starting a payment.
type InitiateResult struct {
	Success     bool
	Reference   *string
	RedirectURL *string
	DeepLinkURL *string
	QRCode      *string
	Message     *string
	RawResponse map[string]any
}

// VerificationResult is the driver outcome of querying a payment.
type VerificationResult struct {
	IsCompleted   bool
	IsFailed      bool
	GatewayStatus *string
	TransactionID *string
	RawResponse   map[string]any
}

// CallbackResult is the driver outcome of a gateway callback, always
// confirmed with the gateway (never trusted from the callback body).
// IsSuccessful and IsFailed are both false when the outcome is not final yet;
// the payment then stays pending.
type CallbackResult struct {
	IsSuccessful  bool
	IsFailed      bool
	PaymentRef    *string
	GatewayRef    *string
	GatewayStatus *string
	RawResponse   map[string]any
}

// CurrencySource names the currency new payments are recorded in: the
// default currency (modules/currencies). Amounts are never converted.
type CurrencySource interface {
	DefaultCode(ctx context.Context) (string, error)
}

// CurrencyFormats says how a currency is written (modules/currencies);
// a code without a row gets money.Fallback.
type CurrencyFormats interface {
	Format(ctx context.Context, code string) money.Currency
}

// FixedCurrency is a CurrencySource that always answers one code (tests,
// tools).
type FixedCurrency string

// DefaultCode implements CurrencySource.
func (c FixedCurrency) DefaultCode(context.Context) (string, error) { return string(c), nil }

// CurrencyLimited is a driver that settles only in some currencies (bKash
// and Nagad: BDT). The orchestrator refuses a payment in any other.
type CurrencyLimited interface {
	Currencies() []string
}

// PaymentDriver is the pluggable gateway contract.
type PaymentDriver interface {
	Initiate(ctx context.Context, payment *Payment, data map[string]any) (InitiateResult, error)
	Verify(ctx context.Context, payment *Payment) (VerificationResult, error)
	HandleCallback(ctx context.Context, payload map[string]any) (CallbackResult, error)
	Refund(ctx context.Context, payment *Payment, amount int64) (bool, error)
}
