package branches

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database/dbtest"
)

// TestTenantIsolationMatrix is the branch-level tenancy
// isolation matrix: every read and write is scoped by explicit tenant id, and
// cross-tenant access must fail closed.
func TestTenantIsolationMatrix(t *testing.T) {
	queries, pool := dbtest.Queries(t)
	dbtest.Clean(t, pool, "branches", "tenants")
	repo := NewRepository(queries)
	ctx := context.Background()

	tenantA := createTenant(t, ctx, pool, "Tenant A", "iso-a")
	tenantB := createTenant(t, ctx, pool, "Tenant B", "iso-b")

	branchA, err := repo.Create(ctx, CreateInput{TenantID: tenantA, Name: "Branch A"})
	if err != nil {
		t.Fatalf("create branch A: %v", err)
	}
	branchB, err := repo.Create(ctx, CreateInput{TenantID: tenantB, Name: "Branch B"})
	if err != nil {
		t.Fatalf("create branch B: %v", err)
	}

	t.Run("list is scoped", func(t *testing.T) {
		items, err := repo.List(ctx, ListFilter{TenantID: tenantA, Limit: 50})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(items) != 1 || items[0].ID != branchA.ID {
			t.Fatalf("List(tenant A) = %+v, want only branch A", items)
		}
		total, err := repo.Count(ctx, ListFilter{TenantID: tenantA})
		if err != nil {
			t.Fatalf("Count: %v", err)
		}
		if total != 1 {
			t.Errorf("Count(tenant A) = %d, want 1", total)
		}
	})

	t.Run("cross-tenant read denied", func(t *testing.T) {
		_, err := repo.Get(ctx, tenantA, branchB.ID, false)
		if !apperr.Is(err, apperr.ResourceNotFound) {
			t.Fatalf("Get(tenant A, branch B) error = %v, want RESOURCE_NOT_FOUND", err)
		}
		belongs, err := repo.BelongsToTenant(ctx, tenantA, branchB.ID)
		if err != nil {
			t.Fatalf("BelongsToTenant: %v", err)
		}
		if belongs {
			t.Error("branch B reported as belonging to tenant A")
		}
	})

	t.Run("cross-tenant update denied", func(t *testing.T) {
		_, err := repo.Update(ctx, UpdateInput{ID: branchB.ID, TenantID: tenantA, Name: "hijacked"})
		if !apperr.Is(err, apperr.ResourceNotFound) {
			t.Fatalf("Update(tenant A, branch B) error = %v, want RESOURCE_NOT_FOUND", err)
		}
		// Branch B is untouched when read in its own tenant.
		got, err := repo.Get(ctx, tenantB, branchB.ID, false)
		if err != nil {
			t.Fatalf("Get(tenant B, branch B): %v", err)
		}
		if got.Name != "Branch B" {
			t.Errorf("branch B name = %q, want unchanged", got.Name)
		}
	})

	t.Run("cross-tenant delete denied", func(t *testing.T) {
		err := repo.Delete(ctx, tenantA, branchB.ID, nil)
		if !apperr.Is(err, apperr.ResourceNotFound) {
			t.Fatalf("Delete(tenant A, branch B) error = %v, want RESOURCE_NOT_FOUND", err)
		}
		if _, err := repo.Get(ctx, tenantB, branchB.ID, false); err != nil {
			t.Fatalf("branch B should still exist in tenant B: %v", err)
		}
	})
}

func createTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name, code string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(ctx,
		`INSERT INTO tenants (name, code, is_active) VALUES ($1, $2, true) RETURNING id`,
		name, code,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert tenant %q: %v", name, err)
	}
	return id
}
