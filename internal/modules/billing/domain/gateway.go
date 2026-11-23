package domain

import "context"

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

// PaymentDriver is the pluggable gateway contract.
type PaymentDriver interface {
	Initiate(ctx context.Context, payment *Payment, data map[string]any) (InitiateResult, error)
	Verify(ctx context.Context, payment *Payment) (VerificationResult, error)
	HandleCallback(ctx context.Context, payload map[string]any) (CallbackResult, error)
	Refund(ctx context.Context, payment *Payment, amount int64) (bool, error)
}
