package service_test

import (
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
	service "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/service"
)

// The staff subscriptions panel (docs/guides/billing.md#the-staff-subscriptions-panel):
// one organization in each state, then every view, filter, sort and count.
func TestStaffSubscriptionPanel(t *testing.T) {
	h := newChangesHarness(t)
	grace := 14 * 24 * time.Hour
	subscriptions := h.subscriptions.WithGrace(grace)
	team := h.team()

	active := h.tenant("stf-active")
	h.paidPlan(active, h.basic, 1000, 0, 30)

	yearly := h.tenant("stf-yearly")
	if _, _, err := h.subscriptions.Assign(h.ctx, service.AssignInput{
		TenantID: yearly, PackageID: team.ID, ActorID: 1, PaymentMethod: domain.MethodCash, Cycle: domain.CycleYearly,
	}); err != nil {
		t.Fatalf("Assign(yearly) error = %v", err)
	}

	trial := h.tenant("stf-trial")
	now := time.Now()
	trialEnds := now.AddDate(0, 0, 14)
	if _, err := h.subsRepo.Create(h.ctx, domain.SubscriptionInput{
		TenantID: trial, PackageID: h.pro.ID, Status: domain.StatusTrial, StartsAt: &now, ExpiresAt: &trialEnds,
	}); err != nil {
		t.Fatalf("create trial: %v", err)
	}

	inGrace := h.tenant("stf-grace")
	h.paidPlan(inGrace, h.basic, 1000, 33, 30) // lapsed three days ago

	readOnly := h.tenant("stf-readonly")
	lapsed := h.paidPlan(readOnly, h.basic, 1000, 30, 30)
	h.setPeriod(lapsed.ID, -50*24, -20*24) // lapsed twenty days ago

	pending := h.tenant("stf-pending")
	if _, err := h.subscriptions.CheckoutAt(h.ctx, pending, h.pro.ID, "", domain.MethodBankTransfer, nil, 1); err != nil {
		t.Fatalf("pending checkout: %v", err)
	}

	list := func(in service.StaffListInput) []service.StaffSubscriptionRow {
		t.Helper()
		if in.Limit == 0 {
			in.Limit = 50
		}
		rows, total, err := subscriptions.ListForStaff(h.ctx, in)
		if err != nil {
			t.Fatalf("ListForStaff(%+v) error = %v", in, err)
		}
		if total != int64(len(rows)) {
			t.Fatalf("ListForStaff(%+v) total = %d, rows = %d", in, total, len(rows))
		}
		return rows
	}
	tenants := func(rows []service.StaffSubscriptionRow) map[int64]service.StaffSubscriptionRow {
		out := map[int64]service.StaffSubscriptionRow{}
		for _, row := range rows {
			out[row.TenantID] = row
		}
		return out
	}
	expect := func(view string, want ...int64) map[int64]service.StaffSubscriptionRow {
		t.Helper()
		got := tenants(list(service.StaffListInput{View: view}))
		if len(got) != len(want) {
			t.Fatalf("view %s = %d organizations %v, want %v", view, len(got), keys(got), want)
		}
		for _, id := range want {
			if _, ok := got[id]; !ok {
				t.Fatalf("view %s misses organization %d (has %v)", view, id, keys(got))
			}
		}
		return got
	}

	current := expect(domain.ViewCurrent, active, yearly, trial, inGrace, readOnly)
	expect(domain.ViewActive, active, yearly)
	expect(domain.ViewTrial, trial)
	expect(domain.ViewPending, pending)

	// Names, and the price at the subscription's cycle.
	team12 := current[yearly]
	if team12.TenantCode != "stf-yearly" || team12.TenantName != "Org stf-yearly" || team12.Package.Name != "Team" ||
		team12.Offered.Price != 30000 || team12.Offered.BillingCycle != domain.CycleYearly {
		t.Fatalf("yearly row = %s/%s %s at %d %s; want Team at 30000 yearly",
			team12.TenantCode, team12.TenantName, team12.Package.Name, team12.Offered.Price, team12.Offered.BillingCycle)
	}

	// Grace and read-only, as service.standingOf decides them.
	graceRow := expect(domain.ViewGrace, inGrace)[inGrace]
	if !graceRow.InGrace || graceRow.ReadOnly || graceRow.GraceEndsAt == nil || !graceRow.GraceEndsAt.Equal(graceRow.ExpiresAt.Add(grace)) {
		t.Fatalf("grace row = in grace %v, read-only %v, ends %v", graceRow.InGrace, graceRow.ReadOnly, graceRow.GraceEndsAt)
	}
	readRow := expect(domain.ViewReadOnly, readOnly)[readOnly]
	if !readRow.ReadOnly || readRow.InGrace {
		t.Fatalf("read-only row = read-only %v, in grace %v", readRow.ReadOnly, readRow.InGrace)
	}
	for _, id := range []int64{inGrace, readOnly} {
		standing, err := h.guard.WithGrace(grace).Standing(h.ctx, id)
		if err != nil || (id == inGrace) != standing.InGrace || (id == readOnly) != standing.ReadOnly {
			t.Fatalf("guard standing of %d = %+v, %v; the panel disagrees", id, standing, err)
		}
	}

	// Filters and sort.
	if got := tenants(list(service.StaffListInput{View: domain.ViewCurrent, Search: "stf-year"})); len(got) != 1 || got[yearly].TenantID != yearly {
		t.Fatalf("search stf-year = %v", keys(got))
	}
	if got := tenants(list(service.StaffListInput{View: domain.ViewCurrent, PackageID: &h.basic.ID})); len(got) != 3 {
		t.Fatalf("Basic plans = %v, want active, grace and read-only", keys(got))
	}
	if got := tenants(list(service.StaffListInput{View: domain.ViewCurrent, BillingCycle: domain.CycleYearly})); len(got) != 1 || got[yearly].TenantID != yearly {
		t.Fatalf("yearly = %v", keys(got))
	}
	week := 7
	if got := tenants(list(service.StaffListInput{View: domain.ViewCurrent, ExpiresWithinDays: &week})); len(got) != 2 {
		t.Fatalf("ending within a week = %v, want the lapsed grace and read-only plans", keys(got))
	}
	if sorted := list(service.StaffListInput{View: domain.ViewCurrent, OrderBy: "expires_at", OrderDir: "asc"}); sorted[0].TenantID != readOnly {
		t.Fatalf("sort by end: first is %d, want the read-only organization (ended first)", sorted[0].TenantID)
	}

	// Counts match the views.
	counts, err := subscriptions.CountForStaff(h.ctx)
	if err != nil {
		t.Fatalf("CountForStaff() error = %v", err)
	}
	want := domain.StaffSubscriptionCounts{Active: 2, Trial: 1, Grace: 1, ReadOnly: 1, Pending: 1, Scheduled: 0, All: int64(len(list(service.StaffListInput{})))}
	if counts != want || counts.Current() != 5 {
		t.Fatalf("counts = %+v (current %d), want %+v (current 5)", counts, counts.Current(), want)
	}

	// Bad input is a validation error, not an empty list.
	if _, _, err := subscriptions.ListForStaff(h.ctx, service.StaffListInput{View: "everything", Limit: 10}); !apperr.Is(err, apperr.ValidationFailed) {
		t.Fatalf("unknown view: err = %v, want a validation error", err)
	}
}

func keys(rows map[int64]service.StaffSubscriptionRow) []int64 {
	out := make([]int64, 0, len(rows))
	for id := range rows {
		out = append(out, id)
	}
	return out
}
