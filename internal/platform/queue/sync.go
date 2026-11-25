package queue

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

// SyncEngine executes jobs inline. It powers tests, local development and
// commands that must not require a running worker fleet.
type SyncEngine struct {
	registry *Registry
}

// NewSyncEngine creates the inline engine over a job registry.
func NewSyncEngine(registry *Registry) *SyncEngine {
	return &SyncEngine{registry: registry}
}

// Kind implements Engine.
func (e *SyncEngine) Kind() string { return "sync" }

// Enqueue implements Engine.
func (e *SyncEngine) Enqueue(ctx context.Context, kind string, payload any, opts EnqueueOptions) error {
	data, def, err := encodePayload(e.registry, kind, payload)
	if err != nil {
		return err
	}
	if def.Validate != nil {
		if err := def.Validate(data); err != nil {
			return err
		}
	}
	var tenantID int64
	if scope, ok := tenancy.FromContext(ctx); ok {
		tenantID = scope.EffectiveTenantID()
	}
	if err := checkScope(def, tenantID); err != nil {
		return err
	}
	return def.Handle(ctx, data)
}

// EnqueueTx implements Engine (the transaction is irrelevant for inline runs).
func (e *SyncEngine) EnqueueTx(ctx context.Context, tx pgx.Tx, kind string, payload any, opts EnqueueOptions) error {
	return e.Enqueue(ctx, kind, payload, opts)
}
