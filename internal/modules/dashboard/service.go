// Package dashboard serves the home-page figures: an organization's members,
// activity, files and structure, and for platform staff the platform's
// organizations, users, revenue and plans. Each section is computed only
// when the caller may see it; the rest are left out (null).
package dashboard

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
)

// Allowed period lengths, in days.
var periods = map[int]bool{7: true, 30: true, 90: true}

// DefaultDays is the period when the caller does not choose one.
const DefaultDays = 30

// topN bounds the ranked lists (top actions, actors, recent organizations).
const topN = 5

// Queries are the sqlc queries the dashboard reads.
type Queries interface {
	DashboardTenantMembers(ctx context.Context, arg dbgen.DashboardTenantMembersParams) (dbgen.DashboardTenantMembersRow, error)
	DashboardTenantMembersByDay(ctx context.Context, arg dbgen.DashboardTenantMembersByDayParams) ([]dbgen.DashboardTenantMembersByDayRow, error)
	DashboardTenantActivityByDay(ctx context.Context, arg dbgen.DashboardTenantActivityByDayParams) ([]dbgen.DashboardTenantActivityByDayRow, error)
	DashboardTenantTopActions(ctx context.Context, arg dbgen.DashboardTenantTopActionsParams) ([]dbgen.DashboardTenantTopActionsRow, error)
	DashboardTenantTopActors(ctx context.Context, arg dbgen.DashboardTenantTopActorsParams) ([]dbgen.DashboardTenantTopActorsRow, error)
	DashboardTenantFiles(ctx context.Context, tenantID int64) (dbgen.DashboardTenantFilesRow, error)
	DashboardTenantBranches(ctx context.Context, tenantID int64) (int64, error)
	DashboardTenantRoles(ctx context.Context, tenantID int64) (int64, error)
	DashboardPlatformTenants(ctx context.Context) (dbgen.DashboardPlatformTenantsRow, error)
	DashboardPlatformTenantsByDay(ctx context.Context, arg dbgen.DashboardPlatformTenantsByDayParams) ([]dbgen.DashboardPlatformTenantsByDayRow, error)
	DashboardPlatformUsers(ctx context.Context, activeSince pgtype.Timestamptz) (dbgen.DashboardPlatformUsersRow, error)
	DashboardPlatformUsersByDay(ctx context.Context, arg dbgen.DashboardPlatformUsersByDayParams) ([]dbgen.DashboardPlatformUsersByDayRow, error)
	DashboardPlatformActivityByDay(ctx context.Context, arg dbgen.DashboardPlatformActivityByDayParams) ([]dbgen.DashboardPlatformActivityByDayRow, error)
	DashboardPlatformRevenueByDay(ctx context.Context, arg dbgen.DashboardPlatformRevenueByDayParams) ([]dbgen.DashboardPlatformRevenueByDayRow, error)
	DashboardPlatformPendingPayments(ctx context.Context) ([]dbgen.DashboardPlatformPendingPaymentsRow, error)
	DashboardPlatformPlans(ctx context.Context) ([]dbgen.DashboardPlatformPlansRow, error)
	DashboardPlatformRecentTenants(ctx context.Context, top int32) ([]dbgen.DashboardPlatformRecentTenantsRow, error)
}

// Service computes dashboards.
type Service struct {
	queries Queries
	now     func() time.Time
}

// NewService creates the dashboard service.
func NewService(queries Queries) *Service {
	return &Service{queries: queries, now: time.Now}
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

func timestamp(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

// TenantSections says which organization sections the caller may see.
type TenantSections struct {
	Members, Activity, Files, Branches, Roles bool
}

// Tenant builds an organization's dashboard.
func (s *Service) Tenant(ctx context.Context, tenantID int64, w Window, show TenantSections) (*TenantDashboard, error) {
	tz, since := w.Location.String(), timestamp(w.Previous)
	out := &TenantDashboard{Period: w.period()}
	if show.Members {
		summary, err := s.queries.DashboardTenantMembers(ctx, dbgen.DashboardTenantMembersParams{TenantID: tenantID, ActiveSince: timestamp(w.From)})
		if err != nil {
			return nil, database.MapError("dashboard.members", err)
		}
		rows, err := s.queries.DashboardTenantMembersByDay(ctx, dbgen.DashboardTenantMembersByDayParams{TenantID: tenantID, Since: since, Tz: tz})
		if err != nil {
			return nil, database.MapError("dashboard.members", err)
		}
		days := map[string]int64{}
		for _, row := range rows {
			days[row.Day] = row.Count
		}
		out.Members = &TenantMembers{
			Total: summary.Total, Active: summary.Active, PendingInvitations: summary.PendingInvitations,
			SignedInRecently: summary.SignedInRecently, Added: w.series(days),
		}
	}
	if show.Activity {
		rows, err := s.queries.DashboardTenantActivityByDay(ctx, dbgen.DashboardTenantActivityByDayParams{TenantID: tenantID, Since: since, Tz: tz})
		if err != nil {
			return nil, database.MapError("dashboard.activity", err)
		}
		days := map[string]int64{}
		for _, row := range rows {
			days[row.Day] = row.Count
		}
		from := timestamp(w.From)
		actions, err := s.queries.DashboardTenantTopActions(ctx, dbgen.DashboardTenantTopActionsParams{TenantID: tenantID, Since: from, Top: topN})
		if err != nil {
			return nil, database.MapError("dashboard.activity", err)
		}
		actors, err := s.queries.DashboardTenantTopActors(ctx, dbgen.DashboardTenantTopActorsParams{TenantID: tenantID, Since: from, Top: topN})
		if err != nil {
			return nil, database.MapError("dashboard.activity", err)
		}
		activity := &TenantActivity{Events: w.series(days), TopActions: []RankedAction{}, TopActors: []RankedActor{}}
		for _, row := range actions {
			activity.TopActions = append(activity.TopActions, RankedAction{Action: row.Action, Count: row.Count})
		}
		for _, row := range actors {
			activity.TopActors = append(activity.TopActors, RankedActor{ID: row.ActorID, Name: row.Name, Count: row.Count})
		}
		out.Activity = activity
	}
	if show.Files {
		files, err := s.queries.DashboardTenantFiles(ctx, tenantID)
		if err != nil {
			return nil, database.MapError("dashboard.files", err)
		}
		out.Files = &FilesSummary{Files: files.Files, Bytes: files.Bytes}
	}
	if show.Branches {
		count, err := s.queries.DashboardTenantBranches(ctx, tenantID)
		if err != nil {
			return nil, database.MapError("dashboard.branches", err)
		}
		out.Branches = &count
	}
	if show.Roles {
		count, err := s.queries.DashboardTenantRoles(ctx, tenantID)
		if err != nil {
			return nil, database.MapError("dashboard.roles", err)
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
	tz, since := w.Location.String(), timestamp(w.Previous)
	out := &PlatformDashboard{Period: w.period()}
	if show.Organizations {
		summary, err := s.queries.DashboardPlatformTenants(ctx)
		if err != nil {
			return nil, database.MapError("dashboard.organizations", err)
		}
		rows, err := s.queries.DashboardPlatformTenantsByDay(ctx, dbgen.DashboardPlatformTenantsByDayParams{Since: since, Tz: tz})
		if err != nil {
			return nil, database.MapError("dashboard.organizations", err)
		}
		recent, err := s.queries.DashboardPlatformRecentTenants(ctx, topN)
		if err != nil {
			return nil, database.MapError("dashboard.organizations", err)
		}
		days := map[string]int64{}
		for _, row := range rows {
			days[row.Day] = row.Count
		}
		organizations := &PlatformOrganizations{
			Total: summary.Total, Active: summary.Active, Suspended: summary.Total - summary.Active,
			Added: w.series(days), Recent: []RecentOrganization{},
		}
		for _, row := range recent {
			organizations.Recent = append(organizations.Recent, RecentOrganization{
				ID: row.ID, Name: row.Name, Code: row.Code, Active: row.IsActive, Members: row.Members, CreatedAt: row.CreatedAt.Time,
			})
		}
		out.Organizations = organizations
	}
	if show.Users {
		summary, err := s.queries.DashboardPlatformUsers(ctx, timestamp(w.From))
		if err != nil {
			return nil, database.MapError("dashboard.users", err)
		}
		rows, err := s.queries.DashboardPlatformUsersByDay(ctx, dbgen.DashboardPlatformUsersByDayParams{Since: since, Tz: tz})
		if err != nil {
			return nil, database.MapError("dashboard.users", err)
		}
		days := map[string]int64{}
		for _, row := range rows {
			days[row.Day] = row.Count
		}
		out.Users = &PlatformUsers{Total: summary.Total, SignedInRecently: summary.SignedInRecently, Added: w.series(days)}
	}
	if show.Activity {
		rows, err := s.queries.DashboardPlatformActivityByDay(ctx, dbgen.DashboardPlatformActivityByDayParams{Since: since, Tz: tz})
		if err != nil {
			return nil, database.MapError("dashboard.activity", err)
		}
		days := map[string]int64{}
		for _, row := range rows {
			days[row.Day] = row.Count
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
		rows, err := s.queries.DashboardPlatformPlans(ctx)
		if err != nil {
			return nil, database.MapError("dashboard.plans", err)
		}
		plans := []PlanShare{}
		for _, row := range rows {
			plans = append(plans, PlanShare{PackageID: row.PackageID, Name: row.Name, Organizations: row.Organizations})
		}
		out.Plans = plans
	}
	return out, nil
}

// revenue is completed payments per currency (most payments first) and what
// waits for approval.
func (s *Service) revenue(ctx context.Context, w Window) (*PlatformRevenue, error) {
	rows, err := s.queries.DashboardPlatformRevenueByDay(ctx, dbgen.DashboardPlatformRevenueByDayParams{Since: timestamp(w.Previous), Tz: w.Location.String()})
	if err != nil {
		return nil, database.MapError("dashboard.revenue", err)
	}
	pending, err := s.queries.DashboardPlatformPendingPayments(ctx)
	if err != nil {
		return nil, database.MapError("dashboard.revenue", err)
	}
	byCurrency := map[string]map[string]int64{}
	payments := map[string]int64{}
	order := []string{}
	fromKey := w.From.Format(time.DateOnly)
	for _, row := range rows {
		if byCurrency[row.Currency] == nil {
			byCurrency[row.Currency] = map[string]int64{}
			order = append(order, row.Currency)
		}
		byCurrency[row.Currency][row.Day] += row.Amount
		if row.Day >= fromKey {
			payments[row.Currency] += row.Count
		}
	}
	out := &PlatformRevenue{Currencies: []CurrencyRevenue{}, Pending: []PendingPayments{}}
	for _, currency := range order {
		series := w.series(byCurrency[currency])
		out.Currencies = append(out.Currencies, CurrencyRevenue{Currency: currency, Payments: payments[currency], Amount: series})
	}
	// The currency with the most completed payments leads.
	for i := 1; i < len(out.Currencies); i++ {
		for j := i; j > 0 && out.Currencies[j].Payments > out.Currencies[j-1].Payments; j-- {
			out.Currencies[j], out.Currencies[j-1] = out.Currencies[j-1], out.Currencies[j]
		}
	}
	for _, row := range pending {
		out.Pending = append(out.Pending, PendingPayments{Currency: row.Currency, Count: row.Count, Amount: row.Amount})
	}
	return out, nil
}
