package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/migrations"
)

const (
	migrationsDir = "."
	// migrationLockID is an application-specific advisory lock key that
	// serializes concurrent migration runs.
	migrationLockID = int64(4804691)
)

// MigrationStatus describes one embedded go migration and whether it is applied.
type MigrationStatus struct {
	Version int64
	Name    string
	Applied bool
}

var (
	gooseSetupOnce sync.Once
	gooseSetupErr  error
)

func setupGoose() error {
	gooseSetupOnce.Do(func() {
		goose.SetLogger(goose.NopLogger())
		goose.SetBaseFS(migrations.Files)
		gooseSetupErr = goose.SetDialect("postgres")
	})
	return gooseSetupErr
}

func embeddedMigrations() (goose.Migrations, error) {
	if err := setupGoose(); err != nil {
		return nil, err
	}
	return goose.CollectMigrations(migrationsDir, 0, goose.MaxVersion)
}

func riverMigrator(pool *pgxpool.Pool) (*rivermigrate.Migrator[pgx.Tx], error) {
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return nil, fmt.Errorf("create river migrator: %w", err)
	}
	return migrator, nil
}

// MigrateUp applies all pending goose migrations and River migrations, and
// reports how many were applied. River owns its tables; running both here keeps
// a single migration entry point.
func MigrateUp(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	if err := setupGoose(); err != nil {
		return 0, err
	}
	return withMigrationLock(ctx, pool, func(db *sql.DB, pool *pgxpool.Pool) (int, error) {
		before, err := goose.GetDBVersion(db)
		if err != nil {
			return 0, fmt.Errorf("read migration version: %w", err)
		}
		if err := goose.UpContext(ctx, db, migrationsDir); err != nil {
			return 0, err
		}
		after, err := goose.GetDBVersion(db)
		if err != nil {
			return 0, fmt.Errorf("read migration version: %w", err)
		}
		migs, err := embeddedMigrations()
		if err != nil {
			return 0, err
		}
		applied := 0
		for _, m := range migs {
			if m.Version > before && m.Version <= after {
				applied++
			}
		}

		migrator, err := riverMigrator(pool)
		if err != nil {
			return applied, err
		}
		result, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
		if err != nil {
			return applied, fmt.Errorf("apply river migrations: %w", err)
		}
		applied += len(result.Versions)
		return applied, nil
	})
}

// MigrateDown rolls back up to steps migrations (River first, then goose) and
// reports how many were reverted.
func MigrateDown(ctx context.Context, pool *pgxpool.Pool, steps int) (int, error) {
	if steps < 1 {
		return 0, fmt.Errorf("rollback steps must be at least 1")
	}
	if err := setupGoose(); err != nil {
		return 0, err
	}
	return withMigrationLock(ctx, pool, func(db *sql.DB, pool *pgxpool.Pool) (int, error) {
		reverted := 0

		migrator, err := riverMigrator(pool)
		if err != nil {
			return 0, err
		}
		result, err := migrator.Migrate(ctx, rivermigrate.DirectionDown, &rivermigrate.MigrateOpts{MaxSteps: steps})
		if err != nil {
			return 0, fmt.Errorf("rollback river migrations: %w", err)
		}
		reverted += len(result.Versions)

		remaining := steps - reverted
		if remaining <= 0 {
			return reverted, nil
		}

		current, err := goose.GetDBVersion(db)
		if err != nil {
			return reverted, fmt.Errorf("read migration version: %w", err)
		}
		migs, err := embeddedMigrations()
		if err != nil {
			return reverted, err
		}
		var applied []*goose.Migration
		for _, m := range migs {
			if m.Version <= current {
				applied = append(applied, m)
			}
		}
		if len(applied) == 0 {
			return reverted, nil
		}

		count := remaining
		if count > len(applied) {
			count = len(applied)
		}
		var target int64
		if count < len(applied) {
			target = applied[len(applied)-count-1].Version
		}
		if err := goose.DownToContext(ctx, db, migrationsDir, target); err != nil {
			return reverted, err
		}
		return reverted + count, nil
	})
}

// MigrationStatuses reports applied/pending state for every embedded go
// migration.
func MigrationStatuses(ctx context.Context, pool *pgxpool.Pool) ([]MigrationStatus, error) {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	current, err := goose.GetDBVersion(db)
	if err != nil {
		return nil, fmt.Errorf("read migration version: %w", err)
	}
	migs, err := embeddedMigrations()
	if err != nil {
		return nil, err
	}

	statuses := make([]MigrationStatus, 0, len(migs))
	for _, m := range migs {
		statuses = append(statuses, MigrationStatus{
			Version: m.Version,
			Name:    filepath.Base(m.Source),
			Applied: m.Version <= current,
		})
	}
	return statuses, nil
}

// RiverSchemaVersion reports the applied River schema version and whether the
// river_migration table exists at all.
func RiverSchemaVersion(ctx context.Context, pool *pgxpool.Pool) (int64, bool, error) {
	var version int64
	err := pool.QueryRow(ctx, "SELECT COALESCE(MAX(version), 0) FROM river_migration").Scan(&version)
	if err != nil {
		var pgErr interface{ SQLState() string }
		if errors.As(err, &pgErr) && pgErr.SQLState() == "42P01" {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("read river schema version: %w", err)
	}
	return version, true, nil
}

// RiverPendingCount reports how many River migrations are outstanding.
func RiverPendingCount(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	migrator, err := riverMigrator(pool)
	if err != nil {
		return 0, err
	}
	result, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, &rivermigrate.MigrateOpts{DryRun: true})
	if err != nil {
		return 0, fmt.Errorf("check river migrations: %w", err)
	}
	return len(result.Versions), nil
}

func withMigrationLock(ctx context.Context, pool *pgxpool.Pool, fn func(db *sql.DB, pool *pgxpool.Pool) (int, error)) (int, error) {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	lockConn, err := db.Conn(ctx)
	if err != nil {
		return 0, fmt.Errorf("open migration lock connection: %w", err)
	}
	defer func() { _ = lockConn.Close() }()

	if _, err := lockConn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return 0, fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		_, _ = lockConn.ExecContext(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationLockID)
	}()

	return fn(db, pool)
}
