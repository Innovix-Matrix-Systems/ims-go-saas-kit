package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
)

// TxManager exposes sqlc queries bound to a pool or to a transaction. Services
// own transaction boundaries; repositories receive the queries they need.
type TxManager struct {
	pool    *pgxpool.Pool
	queries *dbgen.Queries
}

// NewTxManager creates a transaction manager for a connection.
func NewTxManager(conn *Conn) *TxManager {
	return &TxManager{pool: conn.Pool, queries: dbgen.New(conn.Pool)}
}

// Pool returns the underlying pool.
func (m *TxManager) Pool() *pgxpool.Pool {
	return m.pool
}

// Queries returns queries bound to the pool (non-transactional access).
func (m *TxManager) Queries() *dbgen.Queries {
	return m.queries
}

// WithTx runs fn inside a transaction. The queries passed to fn share the
// transaction handle; the transaction is rolled back on error or panic and
// committed otherwise.
func (m *TxManager) WithTx(ctx context.Context, fn func(q *dbgen.Queries) error) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	// Roll back with a context detached from cancellation so a timed-out or
	// cancelled request still releases the transaction and its locks.
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()

	if err := fn(m.queries.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// WithTenantLock runs fn in a transaction that first takes a per-tenant
// advisory lock on resource (e.g. "users.max"). Use it for "count, check the
// plan limit, insert" so concurrent creates cannot exceed a quota.
func (m *TxManager) WithTenantLock(ctx context.Context, resource string, tenantID int64, fn func(q *dbgen.Queries) error) error {
	return m.WithTx(ctx, func(q *dbgen.Queries) error {
		if err := q.LockTenantResource(ctx, dbgen.LockTenantResourceParams{Resource: resource, TenantID: tenantID}); err != nil {
			return MapError("tenant_lock", err)
		}
		return fn(q)
	})
}

// SchedulerLockID is the advisory lock that keeps one scheduler per database.
const SchedulerLockID = int64(4804692)

// TryHoldLock takes a session-level advisory lock on a dedicated connection
// and keeps it until release is called. ok is false when another process
// already holds it. Use it to make "one instance per deployment" rules
// self-enforcing (e.g. the scheduler).
func (m *TxManager) TryHoldLock(ctx context.Context, key int64) (release func(), ok bool, err error) {
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("acquire lock connection: %w", err)
	}
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&ok); err != nil {
		conn.Release()
		return nil, false, fmt.Errorf("try advisory lock: %w", err)
	}
	if !ok {
		conn.Release()
		return nil, false, nil
	}
	return func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", key)
		conn.Release()
	}, true, nil
}
