package app_test

import (
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

// Duplicate-request protection through the real router: a
// double submit is refused, an Idempotency-Key replays the first answer
// (docs/guides/handlers-and-routes.md#duplicate-requests).

func TestDoubleSubmitIsRefused(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)

	var branch struct {
		ID int64 `json:"id"`
	}
	app.As(admin).Post("/api/v1/branch", `{"name":"Sylhet"}`).Status(t, http.StatusCreated).Data(t, &branch)
	requireErrorCode(t, app.As(admin).Post("/api/v1/branch", `{"name":"Sylhet"}`), http.StatusConflict, "DUPLICATE_REQUEST")
	if n := countRows(t, app, `SELECT count(*) FROM branches WHERE name = 'Sylhet'`); n != 1 {
		t.Fatalf("branches named Sylhet = %d, want 1", n)
	}

	// Another user sending the same body is another request.
	app.As(app.User(t, testkit.InTenant(app.Tenant(t, "Other")))).Post("/api/v1/branch", `{"name":"Sylhet"}`).Status(t, http.StatusCreated)
	// Reads and skipped routes are never refused.
	app.As(admin).Get("/api/v1/branch").Status(t, http.StatusOK)
	app.As(admin).Get("/api/v1/branch").Status(t, http.StatusOK)
	app.As(admin).Post("/api/v1/notification/read-all", nil).Status(t, http.StatusNoContent)
	app.As(admin).Post("/api/v1/notification/read-all", nil).Status(t, http.StatusNoContent)
}

func TestIdempotencyKeyReplaysTheFirstAnswer(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)

	var first, replay struct {
		ID int64 `json:"id"`
	}
	app.As(admin).Header("Idempotency-Key", "create-khulna-1").Post("/api/v1/branch", `{"name":"Khulna"}`).
		Status(t, http.StatusCreated).Data(t, &first)
	res := app.As(admin).Header("Idempotency-Key", "create-khulna-1").Post("/api/v1/branch", `{"name":"Khulna"}`)
	res.Status(t, http.StatusCreated).Data(t, &replay)
	if replay.ID != first.ID || res.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay = %d (replayed=%q), want branch %d again", replay.ID, res.Header().Get("Idempotent-Replayed"), first.ID)
	}
	if n := countRows(t, app, `SELECT count(*) FROM branches WHERE name = 'Khulna'`); n != 1 {
		t.Fatalf("branches named Khulna = %d, want 1", n)
	}
	requireErrorCode(t, app.As(admin).Header("Idempotency-Key", "create-khulna-1").Post("/api/v1/branch", `{"name":"Rajshahi"}`),
		http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED")
}

// Staff acting as different tenants send different requests, even with the
// same body: the acting scope is part of who sent it.
func TestActingAsAnotherTenantIsNotADuplicate(t *testing.T) {
	app := testkit.New(t)
	super := app.SuperAdmin(t)
	first, second := app.Tenant(t, "North"), app.Tenant(t, "South")

	for _, tenantID := range []int64{first, second} {
		app.As(super).InTenant(tenantID, app.DefaultBranch(t, tenantID)).
			Post("/api/v1/branch", `{"name":"Rangpur"}`).Status(t, http.StatusCreated)
	}
	requireErrorCode(t, app.As(super).InTenant(second, app.DefaultBranch(t, second)).Post("/api/v1/branch", `{"name":"Rangpur"}`),
		http.StatusConflict, "DUPLICATE_REQUEST")
	if n := countRows(t, app, `SELECT count(*) FROM branches WHERE name = 'Rangpur'`); n != 2 {
		t.Fatalf("branches named Rangpur = %d, want one per tenant", n)
	}
}
