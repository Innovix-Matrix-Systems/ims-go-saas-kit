package tenants

import (
	"context"
	"errors"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database/dbtest"
)

func TestRepositoryCRUD(t *testing.T) {
	queries, pool := dbtest.Queries(t)
	dbtest.Clean(t, pool, "tenants")
	repo := NewRepository(queries)
	ctx := context.Background()

	created, err := repo.Create(ctx, CreateInput{
		Name:     "Acme Corp",
		Code:     "acme",
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.ID == 0 || created.CreatedAt.IsZero() {
		t.Fatalf("created = %+v", created)
	}

	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Code != "acme" {
		t.Errorf("Get().Code = %q, want acme", got.Code)
	}

	byCode, err := repo.GetByCode(ctx, "acme")
	if err != nil {
		t.Fatalf("GetByCode() error = %v", err)
	}
	if byCode.ID != created.ID {
		t.Errorf("GetByCode().ID = %d, want %d", byCode.ID, created.ID)
	}

	second, err := repo.Create(ctx, CreateInput{Name: "Beta Ltd", Code: "beta", IsActive: false})
	if err != nil {
		t.Fatalf("Create() second error = %v", err)
	}

	all, err := repo.List(ctx, ListFilter{Limit: 10, Offset: 0})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 2 {
		t.Errorf("List() length = %d, want 2", len(all))
	}

	filtered, err := repo.List(ctx, ListFilter{Search: "beta", Limit: 10, Offset: 0})
	if err != nil {
		t.Fatalf("List(search) error = %v", err)
	}
	if len(filtered) != 1 || filtered[0].Code != "beta" {
		t.Errorf("List(search) = %+v", filtered)
	}

	total, err := repo.Count(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if total != 2 {
		t.Errorf("Count() = %d, want 2", total)
	}

	updated, err := repo.Update(ctx, UpdateInput{
		ID:       second.ID,
		Name:     "Beta Limited",
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Name != "Beta Limited" || !updated.IsActive {
		t.Errorf("Update() = %+v", updated)
	}

	if err := repo.Delete(ctx, created.ID, nil); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := repo.Get(ctx, created.ID); !apperr.Is(err, apperr.ResourceNotFound) {
		t.Fatalf("Get(deleted) error = %v, want RESOURCE_NOT_FOUND", err)
	}

	remaining, err := repo.Count(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("Count() after delete error = %v", err)
	}
	if remaining != 1 {
		t.Errorf("Count() after delete = %d, want 1", remaining)
	}
}

func TestRepositoryUniqueCodeConflict(t *testing.T) {
	queries, pool := dbtest.Queries(t)
	dbtest.Clean(t, pool, "tenants")
	repo := NewRepository(queries)
	ctx := context.Background()

	if _, err := repo.Create(ctx, CreateInput{Name: "Acme", Code: "acme", IsActive: true}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_, err := repo.Create(ctx, CreateInput{Name: "Acme Again", Code: "acme", IsActive: true})
	if !apperr.Is(err, apperr.ResourceAlreadyExists) {
		t.Fatalf("duplicate Create() error = %v, want RESOURCE_ALREADY_EXISTS", err)
	}
}

func TestRepositorySoftDeleteAllowsCodeReuse(t *testing.T) {
	queries, pool := dbtest.Queries(t)
	dbtest.Clean(t, pool, "tenants")
	repo := NewRepository(queries)
	ctx := context.Background()

	first, err := repo.Create(ctx, CreateInput{Name: "Acme", Code: "acme", IsActive: true})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := repo.Delete(ctx, first.ID, nil); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Partial unique index (WHERE deleted_at IS NULL) allows reuse.
	recreated, err := repo.Create(ctx, CreateInput{Name: "Acme Two", Code: "acme", IsActive: true})
	if err != nil {
		t.Fatalf("recreate after soft delete error = %v", err)
	}
	if recreated.ID == first.ID {
		t.Errorf("recreated.ID = %d, want a new row", recreated.ID)
	}

	missingErr := repo.Delete(ctx, 999999, nil)
	if !apperr.Is(missingErr, apperr.ResourceNotFound) {
		t.Fatalf("Delete(missing) error = %v, want RESOURCE_NOT_FOUND", missingErr)
	}

	var appErr *apperr.Error
	if !errors.As(missingErr, &appErr) {
		t.Fatalf("Delete() error type = %T, want *apperr.Error", missingErr)
	}
}
