package app_test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	billingdomain "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/billing/domain"
	billingservice "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/billing/service"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

// TestQuotaUnderConcurrency fires more concurrent creates than the pool has
// connections. The plan limit is read before the per-tenant lock, so the lock
// holder never waits for a second connection (that deadlocked), and the lock
// still keeps the count exact: users.max and branches.max hold under a race.
func TestQuotaUnderConcurrency(t *testing.T) {
	t.Setenv("DATABASE_MAX_CONNS", "3")
	t.Setenv("DATABASE_MIN_CONNS", "1")
	app := testkit.New(t)
	ctx := context.Background()

	tenantID := app.Tenant(t, "Quota Clinic")
	admin := app.User(t, testkit.InTenant(tenantID))
	pkg, err := app.Deps.Platform.BillingPackages.Create(ctx, billingdomain.PackageInput{
		Name: "Small", Price: 100, BillingCycle: billingdomain.CycleMonthly, IsActive: true,
	}, []billingdomain.FeatureInput{
		{FeatureKey: string(billingdomain.FeatureUsersMax), IsEnabled: true, LimitValue: 4},
		{FeatureKey: string(billingdomain.FeatureBranchesMax), IsEnabled: true, LimitValue: 3},
	})
	if err != nil {
		t.Fatalf("create package: %v", err)
	}
	if _, _, err := app.Deps.Platform.BillingSubs.Assign(ctx, billingservice.AssignInput{
		TenantID: tenantID, PackageID: pkg.ID, ActorID: 1,
	}); err != nil {
		t.Fatalf("assign plan: %v", err)
	}

	count := func(table string) int64 {
		var n int64
		query := fmt.Sprintf("SELECT count(*) FROM %s WHERE tenant_id = $1 AND deleted_at IS NULL", table)
		if err := app.Pool.QueryRow(ctx, query, tenantID).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}

	const workers = 8
	for _, tc := range []struct {
		name  string
		table string
		limit int64
		body  func(i int) string
		path  string
	}{
		{"users", "users", 4, func(i int) string {
			return fmt.Sprintf(`{"name":"Staff %d","email":"staff%d@quota.test","password":"secret123"}`, i, i)
		}, "/api/v1/user"},
		{"branches", "branches", 3, func(i int) string {
			return fmt.Sprintf(`{"name":"Branch %d"}`, i)
		}, "/api/v1/branch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := count(tc.table)
			codes := make([]int, workers)
			var wg sync.WaitGroup
			for i := range workers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					codes[i] = app.As(admin).Post(tc.path, tc.body(i)).Code
				}()
			}
			done := make(chan struct{})
			go func() { wg.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(30 * time.Second):
				t.Fatal("concurrent creates did not finish: the quota lock deadlocked")
			}

			created := 0
			for _, code := range codes {
				switch code {
				case http.StatusCreated:
					created++
				case http.StatusPaymentRequired:
				default:
					t.Errorf("unexpected status %d (codes %v)", code, codes)
				}
			}
			if want := int(tc.limit - before); created != want {
				t.Errorf("created %d of %d (had %d, limit %d), want exactly %d; codes %v",
					created, workers, before, tc.limit, want, codes)
			}
			if after := count(tc.table); after != tc.limit {
				t.Errorf("%s count = %d, want the limit %d", tc.table, after, tc.limit)
			}
		})
	}
}
