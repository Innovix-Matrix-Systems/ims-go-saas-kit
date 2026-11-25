package currencies_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

type currency struct {
	ID          int64  `json:"id"`
	Code        string `json:"code"`
	Symbol      string `json:"symbol"`
	Precision   int    `json:"precision"`
	IsEnabled   bool   `json:"is_enabled"`
	IsDefault   bool   `json:"is_default"`
	InUse       bool   `json:"in_use"`
	DecimalMark string `json:"decimal_mark"`
}

func byCode(t *testing.T, client *testkit.Client) map[string]currency {
	t.Helper()
	var items []currency
	client.Get("/api/v1/currencies").Status(t, http.StatusOK).Data(t, &items)
	out := map[string]currency{}
	for _, item := range items {
		out[item.Code] = item
	}
	return out
}

// A new install has USD (the default), BDT and EUR; anyone can read how to
// write them, and payments take the default's code.
func TestSeededCurrenciesAndPublicList(t *testing.T) {
	app := testkit.New(t)

	var public struct {
		Default    string `json:"default"`
		Currencies []struct {
			Code   string `json:"code"`
			Symbol string `json:"symbol"`
		} `json:"currencies"`
	}
	app.Guest().Get("/api/v1/public/currencies").Status(t, http.StatusOK).Data(t, &public)
	if public.Default != "USD" || len(public.Currencies) != 3 || public.Currencies[0].Code != "USD" {
		t.Fatalf("public currencies = %+v, want USD first and default, then BDT and EUR", public)
	}

	// Members can't manage currencies; staff can.
	app.As(app.User(t)).Get("/api/v1/currencies").Status(t, http.StatusForbidden)
	staff := app.As(app.SuperAdmin(t))
	seeded := byCode(t, staff)
	if seeded["BDT"].Symbol != "৳" || seeded["EUR"].DecimalMark != "," || !seeded["USD"].IsDefault {
		t.Fatalf("seeded currencies = %+v", seeded)
	}
}

func TestCurrencyRules(t *testing.T) {
	app := testkit.New(t)
	staff := app.As(app.SuperAdmin(t))

	var gbp currency
	staff.Post("/api/v1/currencies", map[string]any{
		"code": "GBP", "name": "Pound Sterling", "symbol": "£", "symbol_first": true, "precision": 2,
		"decimal_mark": ".", "thousands_separator": ",",
	}).Status(t, http.StatusCreated).Data(t, &gbp)
	if gbp.IsDefault || !gbp.IsEnabled {
		t.Fatalf("new currency = %+v, want enabled and not the default", gbp)
	}
	staff.Post("/api/v1/currencies", map[string]any{
		"code": "GBP", "name": "Again", "symbol": "£", "decimal_mark": ".",
	}).Status(t, http.StatusConflict)
	staff.Post("/api/v1/currencies", map[string]any{
		"code": "gb", "name": "Bad", "symbol": "£", "decimal_mark": ".",
	}).Status(t, http.StatusUnprocessableEntity)

	// The default moves; the old one can then be disabled, the new one can't.
	staff.Post(fmt.Sprintf("/api/v1/currencies/%d/default", gbp.ID), nil).Status(t, http.StatusOK)
	all := byCode(t, staff)
	if !all["GBP"].IsDefault || all["USD"].IsDefault {
		t.Fatalf("after moving the default: %+v", all)
	}
	update := func(c currency, enabled bool, precision int) *testkit.Response {
		return staff.Patch(fmt.Sprintf("/api/v1/currencies/%d", c.ID), map[string]any{
			"name": c.Code, "symbol": c.Symbol, "symbol_first": true, "precision": precision,
			"decimal_mark": c.DecimalMark, "thousands_separator": ",", "is_enabled": enabled,
		})
	}
	update(all["GBP"], false, 2).Status(t, http.StatusUnprocessableEntity)
	update(all["USD"], false, 2).Status(t, http.StatusOK)
	staff.Post(fmt.Sprintf("/api/v1/currencies/%d/default", all["USD"].ID), nil).Status(t, http.StatusUnprocessableEntity)
	staff.Delete(fmt.Sprintf("/api/v1/currencies/%d", all["GBP"].ID)).Status(t, http.StatusConflict)

	// A payment pins its currency: decimals locked, no delete; others go.
	if _, err := app.Pool.Exec(t.Context(), `INSERT INTO payments (tenant_id, payment_method, status, amount, currency, invoice_number)
		VALUES ($1, 'cash', 'completed', 1000, 'BDT', 'INV-CURRENCY-TEST')`, testkit.DefaultTenant); err != nil {
		t.Fatal(err)
	}
	all = byCode(t, staff)
	if !all["BDT"].InUse {
		t.Fatalf("BDT in_use = false after a payment")
	}
	update(all["BDT"], true, 0).Status(t, http.StatusUnprocessableEntity)
	update(all["BDT"], true, 2).Status(t, http.StatusOK)
	staff.Delete(fmt.Sprintf("/api/v1/currencies/%d", all["BDT"].ID)).Status(t, http.StatusConflict)
	staff.Delete(fmt.Sprintf("/api/v1/currencies/%d", all["EUR"].ID)).Status(t, http.StatusNoContent)

	var public struct {
		Default string `json:"default"`
	}
	app.Guest().Get("/api/v1/public/currencies").Status(t, http.StatusOK).Data(t, &public)
	if public.Default != "GBP" {
		t.Fatalf("public default = %q after the change, want GBP", public.Default)
	}
}

// Payments take the default currency's code, and bKash (taka only) is
// refused while the default is something else.
func TestPaymentsTakeTheDefaultCurrency(t *testing.T) {
	app := testkit.New(t)
	staff := app.As(app.SuperAdmin(t))
	admin := app.As(app.User(t))

	var pkg struct {
		ID int64 `json:"id"`
	}
	staff.Post("/api/v1/packages", map[string]any{
		"name": "Currency Pro", "price": 2500, "billing_cycle": "monthly", "is_active": true,
		"features": []map[string]any{{"feature_key": "users.max", "is_enabled": true, "limit_value": -1}},
	}).Status(t, http.StatusCreated).Data(t, &pkg)

	admin.Post("/api/v1/billing/subscribe", map[string]any{
		"package_id": pkg.ID, "payment_method": "bkash",
	}).Status(t, http.StatusUnprocessableEntity)

	var created struct {
		Payment struct {
			Currency string `json:"currency"`
		} `json:"payment"`
	}
	admin.Post("/api/v1/billing/subscribe", map[string]any{
		"package_id": pkg.ID, "payment_method": "bank_transfer", "payment_data": map[string]any{"reference_number": "CUR-1"},
	}).Status(t, http.StatusCreated).Data(t, &created)
	if created.Payment.Currency != "USD" {
		t.Fatalf("payment currency = %q, want the default USD", created.Payment.Currency)
	}
}
