// Package dashboard serves the home-page figures: an organization's members,
// activity, files and structure, and for platform staff the platform's
// organizations, users, revenue and plans. Each section is computed only
// when the caller may see it; the rest are left out (null).
package dashboard

import (
	"context"
	"sort"
	"time"
)

// Allowed period lengths, in days.
var periods = map[int]bool{7: true, 30: true, 90: true}

// DefaultDays is the period when the caller does not choose one.
const DefaultDays = 30

// topN bounds the ranked lists (top actions, actors, recent organizations).
const topN = 5

// Service computes dashboards.
type Service struct {
	repo Repository
	now  func() time.Time
}

// NewService creates the dashboard service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// Window is the period a dashboard covers: the last Days days up to now
// (today included), in Location, and the same length before it.
type Window struct {
	Days     int
	Location *time.Location
	From     time.Time // start of the first day of the period
	Previous time.Time // start of the period before it
	To       time.Time // now
}

// NewWindow builds the period ending now.
func (s *Service) NewWindow(days int, location *time.Location) Window {
	now := s.now().In(location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	from := today.AddDate(0, 0, -(days - 1))
	return Window{Days: days, Location: location, From: from, Previous: from.AddDate(0, 0, -days), To: now}
}

func (w Window) period() Period {
	return Period{Days: w.Days, From: w.From.Format(time.DateOnly), To: w.To.Format(time.DateOnly), TimeZone: w.Location.String()}
}

// series turns per-day counts over both periods into the current period's
// points (one per day, zeros filled in) and both totals.
func (w Window) series(days map[string]int64) Series {
	out := Series{Points: make([]Point, 0, w.Days)}
	for day := w.From; !day.After(w.To); day = day.AddDate(0, 0, 1) {
		key := day.Format(time.DateOnly)
		out.Points = append(out.Points, Point{Date: key, Value: days[key]})
		out.Total += days[key]
	}
	fromKey := w.From.Format(time.DateOnly)
	for key, value := range days {
		if key < fromKey {
			out.PreviousTotal += value
		}
	}
	return out
}

// TenantSections says which organization sections the caller may see.
type TenantSections struct {
	Members, Activity, Files, Branches, Roles bool
}

// Tenant builds an organization's dashboard.
func (s *Service) Tenant(ctx context.Context, tenantID int64, w Window, show TenantSections) (*TenantDashboard, error) {
	tz := w.Location.String()
	out := &TenantDashboard{Period: w.period()}
	if show.Members {
		counts, err := s.repo.TenantMembers(ctx, tenantID, w.From)
		if err != nil {
			return nil, err
		}
		added, err := s.repo.TenantMembersAdded(ctx, tenantID, w.Previous, tz)
		if err != nil {
			return nil, err
		}
		out.Members = &TenantMembers{
			Total: counts.Total, Active: counts.Active, PendingInvitations: counts.PendingInvitations,
			SignedInRecently: counts.SignedInRecently, Added: w.series(added),
		}
	}
	if show.Activity {
		events, err := s.repo.TenantActivity(ctx, tenantID, w.Previous, tz)
		if err != nil {
			return nil, err
		}
		actions, err := s.repo.TenantTopActions(ctx, tenantID, w.From, topN)
		if err != nil {
			return nil, err
		}
		actors, err := s.repo.TenantTopActors(ctx, tenantID, w.From, topN)
		if err != nil {
			return nil, err
		}
		out.Activity = &TenantActivity{Events: w.series(events), TopActions: actions, TopActors: actors}
	}
	if show.Files {
		files, err := s.repo.TenantFiles(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		out.Files = files
	}
	if show.Branches {
		count, err := s.repo.TenantBranches(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		out.Branches = &count
	}
	if show.Roles {
		count, err := s.repo.TenantRoles(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		out.Roles = &count
	}
	return out, nil
}

// PlatformSections says which platform sections the caller may see.
type PlatformSections struct {
	Organizations, Users, Activity, Revenue, Plans bool
}

// Platform builds the platform dashboard for staff.
func (s *Service) Platform(ctx context.Context, w Window, show PlatformSections) (*PlatformDashboard, error) {
	tz := w.Location.String()
	out := &PlatformDashboard{Period: w.period()}
	if show.Organizations {
		counts, err := s.repo.Organizations(ctx)
		if err != nil {
			return nil, err
		}
		added, err := s.repo.OrganizationsAdded(ctx, w.Previous, tz)
		if err != nil {
			return nil, err
		}
		recent, err := s.repo.RecentOrganizations(ctx, topN)
		if err != nil {
			return nil, err
		}
		out.Organizations = &PlatformOrganizations{
			Total: counts.Total, Active: counts.Active, Suspended: counts.Total - counts.Active,
			Added: w.series(added), Recent: recent,
		}
	}
	if show.Users {
		counts, err := s.repo.Users(ctx, w.From)
		if err != nil {
			return nil, err
		}
		added, err := s.repo.UsersAdded(ctx, w.Previous, tz)
		if err != nil {
			return nil, err
		}
		out.Users = &PlatformUsers{Total: counts.Total, SignedInRecently: counts.SignedInRecently, Added: w.series(added)}
	}
	if show.Activity {
		days, err := s.repo.PlatformActivity(ctx, w.Previous, tz)
		if err != nil {
			return nil, err
		}
		events := w.series(days)
		out.Activity = &events
	}
	if show.Revenue {
		revenue, err := s.revenue(ctx, w)
		if err != nil {
			return nil, err
		}
		out.Revenue = revenue
	}
	if show.Plans {
		plans, err := s.repo.Plans(ctx)
		if err != nil {
			return nil, err
		}
		out.Plans = plans
	}
	return out, nil
}

// revenue is completed payments per currency (most payments first) and what
// waits for approval.
func (s *Service) revenue(ctx context.Context, w Window) (*PlatformRevenue, error) {
	rows, err := s.repo.Revenue(ctx, w.Previous, w.Location.String())
	if err != nil {
		return nil, err
	}
	pending, err := s.repo.PendingPayments(ctx)
	if err != nil {
		return nil, err
	}
	byCurrency := map[string]DailyCounts{}
	payments := map[string]int64{}
	order := []string{}
	fromKey := w.From.Format(time.DateOnly)
	for _, row := range rows {
		if byCurrency[row.Currency] == nil {
			byCurrency[row.Currency] = DailyCounts{}
			order = append(order, row.Currency)
		}
		byCurrency[row.Currency][row.Day] += row.Amount
		if row.Day >= fromKey {
			payments[row.Currency] += row.Count
		}
	}
	out := &PlatformRevenue{Currencies: []CurrencyRevenue{}, Pending: pending}
	for _, currency := range order {
		series := w.series(byCurrency[currency])
		out.Currencies = append(out.Currencies, CurrencyRevenue{Currency: currency, Payments: payments[currency], Amount: series})
	}
	// The currency with the most completed payments leads.
	sort.SliceStable(out.Currencies, func(i, j int) bool { return out.Currencies[i].Payments > out.Currencies[j].Payments })
	return out, nil
}
