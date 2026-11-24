package notifications

import (
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// TestCrossTenantIsolation proves notifications addressed to one tenant are not
// visible to a user of another tenant (notification isolation).
func TestCrossTenantIsolation(t *testing.T) {
	ctx, repo, pool, store := setupNotificationDB(t)
	service := NewService(repo, store, nil, nil, nil)

	// A second tenant with its own user.
	for _, statement := range []string{
		`INSERT INTO tenants (id, name, code) VALUES (2, 'Tenant Two', 'TEN2')`,
		`INSERT INTO users (id, tenant_id, name, email, password) VALUES (4, 2, 'Other User', 'other@example.com', 'x')`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("seed: %v\n%s", err, statement)
		}
	}

	// A notification addressed to tenant 1 only.
	notification := createNotification(t, service, ctx, TargetInput{Type: TargetTenant})

	visibleToTenant1, err := repo.VisibleToUser(ctx, notification.ID, Visibility{UserID: 1, TenantID: 1, BranchID: 1})
	if err != nil {
		t.Fatalf("VisibleToUser(tenant 1): %v", err)
	}
	if !visibleToTenant1 {
		t.Error("tenant 1 user cannot see its own tenant notification")
	}

	visibleToTenant2, err := repo.VisibleToUser(ctx, notification.ID, Visibility{UserID: 4, TenantID: 2})
	if err != nil {
		t.Fatalf("VisibleToUser(tenant 2): %v", err)
	}
	if visibleToTenant2 {
		t.Error("tenant 2 user can see a tenant 1 notification")
	}

	page, err := service.Feed(ctx, tenancy.Scope{TenantID: 2}, 4, FeedFilter{Limit: 20})
	if err != nil {
		t.Fatalf("Feed(tenant 2): %v", err)
	}
	for _, item := range page.Items {
		if item.ID == notification.ID {
			t.Errorf("tenant 2 feed leaked tenant 1 notification %d", notification.ID)
		}
	}
}
