// Package queue provides the typed job registry and pluggable execution
// engines: sync (inline, for tests/CLI) and River (durable, transactional
// enqueue on Postgres).
package queue

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
)

// Name identifies a logical queue.
type Name string

// Logical queues with distinct retry/timeout policies.
const (
	QueueCritical Name = "critical"
	QueueDefault  Name = "default"
	QueueHeavy    Name = "heavy"
)

// ChannelConfig returns the configured retry/timeout policy for a queue.
func ChannelConfig(cfg config.Queue, name Name) config.Channel {
	switch name {
	case QueueCritical:
		return cfg.Critical
	case QueueHeavy:
		return cfg.Heavy
	default:
		return cfg.Default
	}
}

// EnqueueOptions overrides per-job defaults.
type EnqueueOptions struct {
	Queue      *Name
	MaxRetries *int
	RunAt      *time.Time
}

// Definition describes one typed job kind.
type Definition struct {
	Kind        string
	Description string
	Queue       Name
	MaxRetries  int
	Timeout     time.Duration
	Validate    func(payload []byte) error
	Handle      func(ctx context.Context, payload []byte) error
	// SystemJob marks kinds that legitimately run without a tenant scope
	// (global notifications, tenantless self-service, platform-wide work
	// whose payload carries its own targets). Enqueues of every other kind
	// must carry a tenant: an unscoped envelope would execute the handler
	// across all tenants (there is no "no filter" tenant on purpose).
	SystemJob bool
}

// Engine submits jobs for execution.
type Engine interface {
	Enqueue(ctx context.Context, kind string, payload any, opts EnqueueOptions) error
	EnqueueTx(ctx context.Context, tx pgx.Tx, kind string, payload any, opts EnqueueOptions) error
	Kind() string
}
