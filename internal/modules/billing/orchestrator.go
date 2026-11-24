package billing

import (
	"context"
	"encoding/json"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/billing/service"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/tracing"
)

// Orchestrator selects drivers, creates payment rows and invokes gateways.
type Orchestrator struct {
	drivers  map[string]domain.PaymentDriver
	payments domain.PaymentRepository
	currency string
	now      func() time.Time
}

// NewOrchestrator creates an empty orchestrator.
func NewOrchestrator(payments domain.PaymentRepository, currency string) *Orchestrator {
	return &Orchestrator{
		drivers:  map[string]domain.PaymentDriver{},
		payments: payments,
		currency: currency,
		now:      time.Now,
	}
}

// WithPayments returns a copy that writes payment rows through payments
// (e.g. a transaction-bound repository); drivers are shared.
func (o *Orchestrator) WithPayments(payments domain.PaymentRepository) service.Gateway {
	clone := *o
	clone.payments = payments
	return &clone
}

// Register adds a driver for a payment method.
func (o *Orchestrator) Register(method string, driver domain.PaymentDriver) *Orchestrator {
	o.drivers[method] = driver
	return o
}

// Driver returns the driver for a method or a 503 error.
func (o *Orchestrator) Driver(method string) (domain.PaymentDriver, error) {
	driver, ok := o.drivers[method]
	if !ok {
		return nil, apperr.Newf(apperr.ServiceUnavailable, "The %q payment method is not available.", method)
	}
	return driver, nil
}

// AvailableMethods lists the registered methods.
func (o *Orchestrator) AvailableMethods() []string {
	methods := make([]string, 0, len(o.drivers))
	for method := range o.drivers {
		methods = append(methods, method)
	}
	return methods
}

// Charge creates a pending payment row and initiates it with the driver.
func (o *Orchestrator) Charge(ctx context.Context, tenantID int64, subscriptionID *int64, method string, amount int64, data map[string]any, actorID *int64) (_ *domain.Payment, err error) {
	ctx, end := tracing.Begin(ctx, "billing.charge",
		attribute.Int64("tenant.id", tenantID),
		attribute.String("payment.method", method),
	)
	defer end(&err)
	driver, err := o.Driver(method)
	if err != nil {
		return nil, err
	}

	invoice, err := domain.GenerateInvoiceNumber(o.now())
	if err != nil {
		return nil, err
	}
	payment, err := o.payments.Create(ctx, domain.PaymentInput{
		TenantID:       tenantID,
		SubscriptionID: subscriptionID,
		PaymentMethod:  method,
		Status:         domain.PaymentPending,
		Amount:         amount,
		Currency:       o.currency,
		InvoiceNumber:  invoice,
		CreatedBy:      actorID,
	})
	if err != nil {
		return nil, err
	}

	result, err := driver.Initiate(ctx, payment, data)
	if err != nil {
		_, _ = o.payments.UpdateStatus(ctx, payment.ID, domain.PaymentFailed, nil, nil)
		return nil, err
	}

	response, _ := json.Marshal(result.RawResponse)
	_ = o.payments.UpdateGateway(ctx, payment.ID, result.Reference, nil, response)

	if !result.Success {
		_, _ = o.payments.UpdateStatus(ctx, payment.ID, domain.PaymentFailed, nil, nil)
		message := "The payment gateway rejected the request."
		if result.Message != nil && *result.Message != "" {
			message = *result.Message
		}
		return nil, apperr.New(apperr.ServiceUnavailable, message)
	}

	return o.payments.Get(ctx, payment.ID)
}

// HandleCallback dispatches a gateway callback payload to its driver.
func (o *Orchestrator) HandleCallback(ctx context.Context, method string, payload map[string]any) (_ domain.CallbackResult, err error) {
	ctx, end := tracing.Begin(ctx, "billing.gateway_callback", attribute.String("payment.method", method))
	defer end(&err)
	driver, err := o.Driver(method)
	if err != nil {
		return domain.CallbackResult{}, err
	}
	return driver.HandleCallback(ctx, payload)
}
