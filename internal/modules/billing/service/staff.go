package service

import (
	"context"
	"slices"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
)

// The staff subscriptions panel (docs/guides/billing.md#the-staff-subscriptions-panel):
// every organization's subscription with its plan, price and standing.

// StaffListInput is the panel's query. Empty values do not filter; View ""
// lists every row (ViewAll). OrderBy and OrderDir come whitelisted from
// httpx.OrderParams (id, expires_at or created_at; asc or desc).
type StaffListInput struct {
	View         string
	Status       string
	Search       string
	PackageID    *int64
	BillingCycle string
	// ExpiresWithinDays keeps rows whose period ends within that many days
	// from now (ended ones included).
	ExpiresWithinDays *int
	OrderBy           string
	OrderDir          string
	Limit             int32
	Offset            int32
}

// StaffSubscriptionRow is one row of the panel.
type StaffSubscriptionRow struct {
	domain.StaffSubscription
	// Offered is the plan at the subscription's cycle: its price is what the
	// period costs.
	Offered domain.Package
	// GraceEndsAt is set while the row's plan is in grace.
	GraceEndsAt *time.Time
}

// maxExpiryWindowDays bounds expires_within_days (ten years).
const maxExpiryWindowDays = 3650

func validateStaffList(in StaffListInput) error {
	problems := map[string][]string{}
	if in.View != "" && !slices.Contains(domain.StaffViews, in.View) {
		problems["view"] = []string{"The selected view is invalid."}
	}
	if in.Status != "" && !slices.Contains(domain.SubscriptionStatuses, in.Status) {
		problems["status"] = []string{"The selected status is invalid."}
	}
	if in.BillingCycle != "" && !slices.Contains([]string{domain.CycleMonthly, domain.CycleYearly, domain.CycleOneTime}, in.BillingCycle) {
		problems["billing_cycle"] = []string{"The selected billing cycle is invalid."}
	}
	if in.ExpiresWithinDays != nil && (*in.ExpiresWithinDays < 0 || *in.ExpiresWithinDays > maxExpiryWindowDays) {
		problems["expires_within_days"] = []string{"The number of days must be between 0 and 3650."}
	}
	if len(problems) > 0 {
		return apperr.Validation(problems)
	}
	return nil
}

// ListForStaff lists subscriptions for the staff panel (every organization;
// the route is internal-only).
func (s *SubscriptionService) ListForStaff(ctx context.Context, in StaffListInput) ([]StaffSubscriptionRow, int64, error) {
	if err := validateStaffList(in); err != nil {
		return nil, 0, err
	}
	now := s.now()
	filter := domain.StaffSubscriptionFilter{
		View: in.View, Status: in.Status, Search: in.Search, PackageID: in.PackageID,
		BillingCycle: in.BillingCycle, OrderBy: in.OrderBy, OrderDir: in.OrderDir, GraceSince: now.Add(-s.grace),
		Limit: in.Limit, Offset: in.Offset,
	}
	if filter.View == "" {
		filter.View = domain.ViewAll
	}
	if in.ExpiresWithinDays != nil {
		before := now.AddDate(0, 0, *in.ExpiresWithinDays)
		filter.ExpiresBefore = &before
	}
	items, total, err := s.subscriptions.ListForStaff(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	rows := make([]StaffSubscriptionRow, 0, len(items))
	for _, item := range items {
		row := StaffSubscriptionRow{StaffSubscription: item, Offered: item.PlanFor(item.Package)}
		if item.InGrace && item.ExpiresAt != nil {
			ends := item.ExpiresAt.Add(s.grace)
			row.GraceEndsAt = &ends
		}
		rows = append(rows, row)
	}
	return rows, total, nil
}

// CountForStaff counts the staff panel's views (its tabs).
func (s *SubscriptionService) CountForStaff(ctx context.Context) (domain.StaffSubscriptionCounts, error) {
	return s.subscriptions.CountForStaff(ctx, s.now().Add(-s.grace))
}
