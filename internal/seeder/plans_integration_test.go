package seeder_test

import (
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/seeder"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

type publicPackage struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Price        int64  `json:"price"`
	BillingCycle string `json:"billing_cycle"`
	YearlyPrice  *int64 `json:"yearly_price"`
	TrialDays    int32  `json:"trial_days"`
	Features     []struct {
		FeatureKey string `json:"feature_key"`
		IsEnabled  bool   `json:"is_enabled"`
		LimitValue int64  `json:"limit_value"`
	} `json:"features"`
}

func (p publicPackage) limit(key string) (int64, bool) {
	for _, feature := range p.Features {
		if feature.FeatureKey == key {
			return feature.LimitValue, feature.IsEnabled
		}
	}
	return 0, false
}

func seedPlans(t *testing.T, app *testkit.App) {
	t.Helper()
	if err := seeder.SeedPlans(context.Background(), app.Deps.Tx.Queries(), slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("SeedPlans() error = %v", err)
	}
}

func plansByName(t *testing.T, app *testkit.App) map[string]publicPackage {
	t.Helper()
	var list []publicPackage
	app.Guest().Get("/api/v1/public/packages").Status(t, http.StatusOK).Data(t, &list)
	out := make(map[string]publicPackage, len(list))
	for _, pkg := range list {
		out[pkg.Name] = pkg
	}
	return out
}

// The sample public plans: Free, Pro and Business, yearly as a price of the plan.
func TestSeedPlansCreatesSamplePlans(t *testing.T) {
	app := testkit.New(t)
	seedPlans(t, app)
	plans := plansByName(t, app)

	// Yearly is a price of Pro and Business, not a plan of its own.
	for _, separate := range []string{"Pro yearly", "Business yearly"} {
		if _, found := plans[separate]; found {
			t.Fatalf("plan %q exists; yearly should be a price of the plan", separate)
		}
	}
	want := []struct {
		name          string
		price, yearly int64 // yearly 0: monthly only
		cycle         string
		users, branch int64
		storage       int64
		invoices      bool
		api           bool
	}{
		{"Free", 0, 0, "one_time", 3, 1, 100, false, false},
		{"Pro", 1900, 19000, "monthly", 25, 3, 5120, true, false},
		{"Business", 4900, 49000, "monthly", 100, -1, 51200, true, true},
	}
	for _, w := range want {
		pkg, ok := plans[w.name]
		if !ok {
			t.Fatalf("plan %q missing from the public list: %v", w.name, plans)
		}
		if pkg.Price != w.price || pkg.BillingCycle != w.cycle || pkg.TrialDays != 0 {
			t.Errorf("%s = price %d, cycle %s, trial %d; want %d, %s, no trial", w.name, pkg.Price, pkg.BillingCycle, pkg.TrialDays, w.price, w.cycle)
		}
		if yearly := pkg.YearlyPrice; (w.yearly == 0) != (yearly == nil) || (yearly != nil && *yearly != w.yearly) {
			t.Errorf("%s yearly_price = %v, want %d (0: none)", w.name, yearly, w.yearly)
		}
		if users, _ := pkg.limit("users.max"); users != w.users {
			t.Errorf("%s users.max = %d, want %d", w.name, users, w.users)
		}
		if branches, _ := pkg.limit("branches.max"); branches != w.branch {
			t.Errorf("%s branches.max = %d, want %d", w.name, branches, w.branch)
		}
		if storage, _ := pkg.limit("storage.mb"); storage != w.storage {
			t.Errorf("%s storage.mb = %d, want %d", w.name, storage, w.storage)
		}
		if _, on := pkg.limit("invoices.manage"); on != w.invoices {
			t.Errorf("%s invoices.manage = %v, want %v", w.name, on, w.invoices)
		}
		if _, on := pkg.limit("api.access"); on != w.api {
			t.Errorf("%s api.access = %v, want %v", w.name, on, w.api)
		}
	}
}

// Seeding again creates nothing and keeps what staff changed.
func TestSeedPlansNeverOverwrites(t *testing.T) {
	app := testkit.New(t)
	seedPlans(t, app)
	pro := plansByName(t, app)["Pro"]
	if _, err := app.Pool.Exec(context.Background(), `UPDATE packages SET price = 2900 WHERE id = $1`, pro.ID); err != nil {
		t.Fatal(err)
	}

	seedPlans(t, app)
	var count int
	if err := app.Pool.QueryRow(context.Background(), `SELECT count(*) FROM packages WHERE name = 'Pro'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if price := plansByName(t, app)["Pro"].Price; count != 1 || price != 2900 {
		t.Fatalf("after seeding again: %d Pro packages at %d, want 1 at the edited 2900", count, price)
	}
}

// Sign-up starts a new organization on Free, which never runs out.
func TestSignUpStartsOnFree(t *testing.T) {
	app := testkit.New(t)
	seedPlans(t, app)
	// The test kit's own free package would be picked first; production has none.
	if _, err := app.Pool.Exec(context.Background(), `UPDATE packages SET is_active = false WHERE name = 'Testkit Unlimited'`); err != nil {
		t.Fatal(err)
	}

	app.Guest().Post("/api/v1/signup", map[string]any{
		"name": "Nadia Islam", "email": "nadia@acme.test", "password": "a long passphrase",
		"organization_name": "Acme Digital", "phone": "+8801711000001", "accept_terms": true,
	}).Status(t, http.StatusAccepted)

	var plan, status string
	var expires time.Time
	if err := app.Pool.QueryRow(context.Background(), `SELECT p.name, s.status, s.expires_at FROM subscriptions s
		JOIN packages p ON p.id = s.package_id
		JOIN users u ON u.tenant_id = s.tenant_id
		WHERE u.email = 'nadia@acme.test'`).Scan(&plan, &status, &expires); err != nil {
		t.Fatal(err)
	}
	if plan != "Free" || status != "active" || expires.Before(time.Now().AddDate(50, 0, 0)) {
		t.Fatalf("new organization on %q (%s, expires %s), want an active Free plan that never ends", plan, status, expires)
	}
}
