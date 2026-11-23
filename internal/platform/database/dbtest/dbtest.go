// Package dbtest provides helpers for repository integration tests. Every test
// package gets its own Postgres schema (deterministic per working directory and
// recreated on each run), so `go test ./...` can run packages in parallel
// without cross-package interference. Tests are skipped cleanly when
// DATABASE_TEST_DSN is not set.
package dbtest

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
)

// EnvDSN is the environment variable pointing at the base test database.
const EnvDSN = "DATABASE_TEST_DSN"

var (
	setupOnce  sync.Once
	setupPool  *pgxpool.Pool
	setupDSN   string
	setupErr   error
	schemaName string
)

func setup(t *testing.T) {
	t.Helper()

	base := os.Getenv(EnvDSN)
	if base == "" {
		t.Skipf("%s is not set; skipping integration test", EnvDSN)
	}

	setupOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		schemaName = schemaForWorkingDirectory()

		admin, err := pgx.Connect(ctx, base)
		if err != nil {
			setupErr = fmt.Errorf("connect test database: %w", err)
			return
		}
		defer func() { _ = admin.Close(ctx) }()

		for _, statement := range []string{
			"DROP SCHEMA IF EXISTS " + schemaName + " CASCADE",
			"CREATE SCHEMA " + schemaName,
		} {
			if _, err := admin.Exec(ctx, statement); err != nil {
				setupErr = fmt.Errorf("%s: %w", statement, err)
				return
			}
		}

		scoped := withSearchPath(base, schemaName)
		poolCfg, err := pgxpool.ParseConfig(scoped)
		if err != nil {
			setupErr = err
			return
		}
		pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
		if err != nil {
			setupErr = err
			return
		}
		if err := pool.Ping(ctx); err != nil {
			setupErr = err
			return
		}
		if _, err := database.MigrateUp(ctx, pool); err != nil {
			setupErr = fmt.Errorf("migrate test schema: %w", err)
			return
		}
		setupPool = pool
		setupDSN = scoped
	})
	if setupErr != nil {
		t.Fatalf("test database setup: %v", setupErr)
	}
}

// Pool returns the schema-scoped test pool.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	setup(t)
	return setupPool
}

// DSN returns the schema-scoped DSN (e.g. for tests that build the full app).
func DSN(t *testing.T) string {
	t.Helper()
	setup(t)
	return setupDSN
}

// Schema returns the schema used by the current test package.
func Schema(t *testing.T) string {
	t.Helper()
	setup(t)
	return schemaName
}

// Queries returns sqlc queries over the shared test pool.
func Queries(t *testing.T) (*dbgen.Queries, *pgxpool.Pool) {
	t.Helper()
	pool := Pool(t)
	return dbgen.New(pool), pool
}

// Clean truncates the given tables with RESTART IDENTITY CASCADE.
func Clean(t *testing.T, pool *pgxpool.Pool, tables ...string) {
	t.Helper()
	if len(tables) == 0 {
		return
	}
	quoted := make([]string, len(tables))
	for i, name := range tables {
		quoted[i] = pgx.Identifier{name}.Sanitize()
	}
	statement := "TRUNCATE " + strings.Join(quoted, ", ") + " RESTART IDENTITY CASCADE"
	if _, err := pool.Exec(context.Background(), statement); err != nil {
		t.Fatalf("truncate %v: %v", tables, err)
	}
}

// SyncSequence advances a table's bigserial sequence past rows seeded with
// explicit ids (like seeder.SyncIDSequence; needed whenever tests insert
// fixed ids and later rely on auto-generated ones).
func SyncSequence(t *testing.T, pool *pgxpool.Pool, table string) {
	t.Helper()
	statement := "SELECT setval(pg_get_serial_sequence('" + table + "', 'id'), (SELECT COALESCE(MAX(id), 1) FROM " + table + "))"
	if _, err := pool.Exec(context.Background(), statement); err != nil {
		t.Fatalf("sync sequence for %s: %v", table, err)
	}
}

func schemaForWorkingDirectory() string {
	wd, err := os.Getwd()
	if err != nil {
		wd = "unknown"
	}
	sum := md5.Sum([]byte(wd))
	return "test_" + hex.EncodeToString(sum[:6])
}

func withSearchPath(dsn, schema string) string {
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + "search_path=" + schema
}
