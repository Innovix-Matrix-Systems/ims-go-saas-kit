package database_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database/dbtest"
)

// TestSettingsMigrationRollbackAndReapply exercises the phase-11 down migration
// (polymorphic target -> tenant_id) and the up migration again, asserting the
// schema returns to the target-aware shape.
func TestSettingsMigrationRollbackAndReapply(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()

	// MigrateDown rolls back River migrations before goose ones, so use a step
	// budget large enough to reach every goose migration.
	reverted, err := database.MigrateDown(ctx, pool, 1000)
	if err != nil {
		t.Fatalf("MigrateDown: %v", err)
	}
	if reverted == 0 {
		t.Fatal("MigrateDown reverted nothing")
	}
	if tableExists(t, ctx, pool, "settings_groups") || tableExists(t, ctx, pool, "feature_flags") {
		t.Fatal("phase-11 tables should be dropped after rollback")
	}

	applied, err := database.MigrateUp(ctx, pool)
	if err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}
	if applied == 0 {
		t.Fatal("MigrateUp applied nothing")
	}
	if !columnExists(t, ctx, pool, "settings", "target_type") {
		t.Fatal("expected settings.target_type after re-apply")
	}
	if columnExists(t, ctx, pool, "settings", "tenant_id") {
		t.Fatal("settings.tenant_id should be gone after re-apply")
	}
	if !tableExists(t, ctx, pool, "settings_groups") || !tableExists(t, ctx, pool, "feature_flags") {
		t.Fatal("expected settings_groups and feature_flags after re-apply")
	}
}

func columnExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, column string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2
		)`, table, column).Scan(&exists)
	if err != nil {
		t.Fatalf("columnExists(%s.%s): %v", table, column, err)
	}
	return exists
}

func tableExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = current_schema() AND table_name = $1
		)`, table).Scan(&exists)
	if err != nil {
		t.Fatalf("tableExists(%s): %v", table, err)
	}
	return exists
}
