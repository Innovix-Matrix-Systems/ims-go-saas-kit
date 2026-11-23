package service

import (
	"context"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/billing/domain"
)

// Gateway initiates payments and dispatches gateway callbacks. It is
// implemented by billing.Orchestrator; the interface keeps this package free of
// an import cycle with the module root.
type Gateway interface {
	Charge(ctx context.Context, tenantID int64, subscriptionID *int64, method string, amount int64, data map[string]any, actorID *int64) (*domain.Payment, error)
	HandleCallback(ctx context.Context, method string, payload map[string]any) (domain.CallbackResult, error)
}

// boundGateway is a Gateway that can write its payment rows through another
// repository (a transaction), so Subscribe never needs a second connection.
type boundGateway interface {
	WithPayments(payments domain.PaymentRepository) Gateway
}
