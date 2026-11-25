// Package database owns named connection pools, transactions, migrations and
// driver error mapping for the application.
package database

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
)

// OpenOption customizes pool creation (e.g. attaching a query tracer).
type OpenOption func(*openOptions)

type openOptions struct {
	tracer pgx.QueryTracer
}

// WithTracer attaches a pgx query tracer to every opened pool.
func WithTracer(tracer pgx.QueryTracer) OpenOption {
	return func(options *openOptions) { options.tracer = tracer }
}

// Conn is one named database connection backed by a pgx pool.
type Conn struct {
	Name   string
	Driver string
	Pool   *pgxpool.Pool
}

// Registry holds every configured named connection.
type Registry struct {
	conns map[string]*Conn
	order []string
}

// Open creates pools for all configured connections. Pools connect lazily; no
// network round-trip happens here, so the API can boot while a database is
// temporarily unavailable (health checks surface the real state).
func Open(ctx context.Context, cfg config.Database, opts ...OpenOption) (*Registry, error) {
	options := openOptions{}
	for _, apply := range opts {
		apply(&options)
	}
	registry := &Registry{conns: make(map[string]*Conn, len(cfg.Connections))}
	for _, connCfg := range cfg.Connections {
		if _, exists := registry.conns[connCfg.Name]; exists {
			registry.Close()
			return nil, fmt.Errorf("database: duplicate connection %q", connCfg.Name)
		}
		conn, err := openConn(ctx, connCfg, options)
		if err != nil {
			registry.Close()
			return nil, err
		}
		registry.conns[connCfg.Name] = conn
		registry.order = append(registry.order, connCfg.Name)
	}
	return registry, nil
}

func openConn(ctx context.Context, cfg config.Connection, options openOptions) (*Conn, error) {
	switch strings.ToLower(cfg.Driver) {
	case "pgsql", "postgres", "postgresql":
		poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
		if err != nil {
			return nil, fmt.Errorf("database %q: invalid DSN: %w", cfg.Name, err)
		}
		poolCfg.MaxConns = cfg.MaxConns
		poolCfg.MinConns = cfg.MinConns
		poolCfg.ConnConfig.ConnectTimeout = cfg.ConnTimeout
		// Backstops (H28): a runaway query or a forgotten open transaction
		// cannot pin a pool connection forever, deadline or not.
		if cfg.StatementTimeout > 0 {
			poolCfg.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(cfg.StatementTimeout.Milliseconds(), 10)
		}
		if cfg.IdleInTxTimeout > 0 {
			poolCfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = strconv.FormatInt(cfg.IdleInTxTimeout.Milliseconds(), 10)
		}
		if options.tracer != nil {
			poolCfg.ConnConfig.Tracer = options.tracer
		}
		pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
		if err != nil {
			return nil, fmt.Errorf("database %q: create pool: %w", cfg.Name, err)
		}
		return &Conn{Name: cfg.Name, Driver: "pgsql", Pool: pool}, nil
	case "sqlite":
		return nil, fmt.Errorf("database %q: sqlite is not supported (PostgreSQL only in v1; deferred to a future version)", cfg.Name)
	default:
		return nil, fmt.Errorf("database %q: unsupported driver %q", cfg.Name, cfg.Driver)
	}
}

// Primary returns the default connection.
func (r *Registry) Primary() *Conn {
	return r.conns["primary"]
}

// Conn returns a named connection.
func (r *Registry) Conn(name string) (*Conn, error) {
	conn, ok := r.conns[name]
	if !ok {
		return nil, fmt.Errorf("database: unknown connection %q", name)
	}
	return conn, nil
}

// Names returns connection names in configuration order.
func (r *Registry) Names() []string {
	return r.order
}

// Close closes every pool.
func (r *Registry) Close() {
	for _, conn := range r.conns {
		conn.Pool.Close()
	}
}
