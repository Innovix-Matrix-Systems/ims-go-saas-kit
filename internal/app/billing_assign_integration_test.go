package app_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	billingdomain "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

func TestAssignPlanEndpoint(t *testing.T) {
	app := testkit.New(t)
	super := app.SuperAdmin(t)
	tenantID := app.Tenant(t, "Acme Clinic")
	pkg, err := app.Deps.Platform.BillingPackages.Create(context.Background(), billingdomain.PackageInput{
		Name: "Enterprise", Price: 9900, BillingCycle: billingdomain.CycleYearly, IsActive: false, // hidden plan
	}, []billingdomain.FeatureInput{
		{FeatureKey: string(billingdomain.FeatureUsersMax), IsEnabled: true, LimitValue: 50},
	})
	if err != nil {
		t.Fatalf("create package: %v", err)
	}
	path := fmt.Sprintf("/api/v1/billing/internal/tenants/%d/subscription", tenantID)

	// Tenant admins cannot assign plans.
	app.As(app.User(t)).Post(path, map[string]any{"package_id": pkg.ID}).Status(t, http.StatusForbidden)

	type result struct {
		Subscription struct {
			ID       int64  `json:"id"`
			TenantID int64  `json:"tenant_id"`
			Status   string `json:"status"`
		} `json:"subscription"`
		Payment *struct {
			Status string `json:"status"`
			Amount int64  `json:"amount"`
			Method string `json:"payment_method"`
		} `json:"payment"`
	}

	var free result
	app.As(super).Post(path, map[string]any{"package_id": pkg.ID}).Status(t, http.StatusCreated).Data(t, &free)
	if free.Subscription.Status != "active" || free.Subscription.TenantID != tenantID || free.Payment != nil {
		t.Fatalf("free assignment = %+v", free)
	}

	var paid result
	app.As(super).Post(path, map[string]any{"package_id": pkg.ID, "payment_method": "cash"}).
		Status(t, http.StatusCreated).Data(t, &paid)
	if paid.Payment == nil || paid.Payment.Status != "completed" || paid.Payment.Amount != 9900 || paid.Payment.Method != "cash" {
		t.Fatalf("cash assignment payment = %+v", paid.Payment)
	}

	app.As(super).Post(path, map[string]any{"package_id": 0}).Status(t, http.StatusUnprocessableEntity)
	app.As(super).Post(path, map[string]any{"package_id": pkg.ID, "payment_method": "bkash"}).Status(t, http.StatusUnprocessableEntity)

	// Overrides: set one, then null resets it to the plan.
	overrides := fmt.Sprintf("/api/v1/billing/internal/subscriptions/%d/overrides", paid.Subscription.ID)
	var sub struct {
		Overrides map[string]any `json:"overrides"`
	}
	app.As(super).Post(overrides, `{"overrides":{"users.max":75}}`).Status(t, http.StatusOK).Data(t, &sub)
	if sub.Overrides["users.max"] != float64(75) {
		t.Fatalf("overrides after set = %v", sub.Overrides)
	}
	var reset struct {
		Overrides map[string]any `json:"overrides"`
	}
	app.As(super).Post(overrides, `{"overrides":{"users.max":null}}`).Status(t, http.StatusOK).Data(t, &reset)
	if _, ok := reset.Overrides["users.max"]; ok {
		t.Fatalf("overrides after null = %v, want users.max removed", reset.Overrides)
	}
}
