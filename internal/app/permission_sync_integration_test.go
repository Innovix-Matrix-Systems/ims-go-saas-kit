package app_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/seeder"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/testkit"
)

// TestPermissionSyncFollowsCatalogSystemFlag: a permission the catalog moves
// between system and tenant (file.view.all became a tenant permission) takes
// the catalog's flag on the next sync, so tenant roles can be granted it.
func TestPermissionSyncFollowsCatalogSystemFlag(t *testing.T) {
	kit := testkit.New(t)
	ctx := context.Background()

	// A database synced before the move still has the old flag.
	if _, err := kit.Pool.Exec(ctx, "UPDATE permissions SET is_system = true WHERE name = $1", authz.PermFileViewAll); err != nil {
		t.Fatalf("mark system: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := seeder.Run(ctx, kit.Config, kit.Deps.Tx.Queries(), kit.Deps.Tx, kit.Deps.Checker, logger); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var isSystem bool
	if err := kit.Pool.QueryRow(ctx, "SELECT is_system FROM permissions WHERE name = $1", authz.PermFileViewAll).Scan(&isSystem); err != nil {
		t.Fatalf("read flag: %v", err)
	}
	if isSystem {
		t.Fatalf("%s is_system = true after sync, want the catalog's false", authz.PermFileViewAll)
	}
}
