package dashboard

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
)

// DailyCounts are counts per local day ("2006-01-02").
type DailyCounts map[string]int64

// MemberCounts are an organization's people right now.
type MemberCounts struct {
	Total, Active, PendingInvitations, SignedInRecently int64
}

// OrganizationCounts are every organization and the active ones.
type OrganizationCounts struct {
	Total, Active int64
}

// UserCounts are every user and those signed in recently.
type UserCounts struct {
	Total, SignedInRecently int64
}

// RevenueDay is one currency's completed payments on one local day.
type RevenueDay struct {
	Currency string
	Day      string
	Amount   int64
	Count    int64
}

// Repository reads the figures the dashboards show.
type Repository interface {
	TenantMembers(ctx context.Context, tenantID int64, activeSince time.Time) (MemberCounts, error)
	TenantMembersAdded(ctx context.Context, tenantID int64, since time.Time, tz string) (DailyCounts, error)
	TenantActivity(ctx context.Context, tenantID int64, since time.Time, tz string) (DailyCounts, error)
	TenantTopActions(ctx context.Context, tenantID int64, since time.Time, top int32) ([]RankedAction, error)
	TenantTopActors(ctx context.Context, tenantID int64, since time.Time, top int32) ([]RankedActor, error)
	TenantFiles(ctx context.Context, tenantID int64) (*FilesSummary, error)
	TenantBranches(ctx context.Context, tenantID int64) (int64, error)
	TenantRoles(ctx context.Context, tenantID int64) (int64, error)

	Organizations(ctx context.Context) (OrganizationCounts, error)
	OrganizationsAdded(ctx context.Context, since time.Time, tz string) (DailyCounts, error)
	RecentOrganizations(ctx context.Context, top int32) ([]RecentOrganization, error)
	Users(ctx context.Context, activeSince time.Time) (UserCounts, error)
	UsersAdded(ctx context.Context, since time.Time, tz string) (DailyCounts, error)
	PlatformActivity(ctx context.Context, since time.Time, tz string) (DailyCounts, error)
	Revenue(ctx context.Context, since time.Time, tz string) ([]RevenueDay, error)
	PendingPayments(ctx context.Context) ([]PendingPayments, error)
	Plans(ctx context.Context) ([]PlanShare, error)
}

type repository struct {
	queries *dbgen.Queries
}

// NewRepository creates the sqlc-backed repository.
func NewRepository(queries *dbgen.Queries) Repository {
	return &repository{queries: queries}
}

func (r *repository) TenantMembers(ctx context.Context, tenantID int64, activeSince time.Time) (MemberCounts, error) {
	row, err := r.queries.DashboardTenantMembers(ctx, dbgen.DashboardTenantMembersParams{TenantID: tenantID, ActiveSince: timestamp(activeSince)})
	if err != nil {
		return MemberCounts{}, database.MapError("dashboard.members", err)
	}
	return MemberCounts{Total: row.Total, Active: row.Active, PendingInvitations: row.PendingInvitations, SignedInRecently: row.SignedInRecently}, nil
}

func (r *repository) TenantMembersAdded(ctx context.Context, tenantID int64, since time.Time, tz string) (DailyCounts, error) {
	rows, err := r.queries.DashboardTenantMembersByDay(ctx, dbgen.DashboardTenantMembersByDayParams{TenantID: tenantID, Since: timestamp(since), Tz: tz})
	if err != nil {
		return nil, database.MapError("dashboard.members", err)
	}
	days := DailyCounts{}
	for _, row := range rows {
		days[row.Day] = row.Count
	}
	return days, nil
}

func (r *repository) TenantActivity(ctx context.Context, tenantID int64, since time.Time, tz string) (DailyCounts, error) {
	rows, err := r.queries.DashboardTenantActivityByDay(ctx, dbgen.DashboardTenantActivityByDayParams{TenantID: tenantID, Since: timestamp(since), Tz: tz})
	if err != nil {
		return nil, database.MapError("dashboard.activity", err)
	}
	days := DailyCounts{}
	for _, row := range rows {
		days[row.Day] = row.Count
	}
	return days, nil
}

func (r *repository) TenantTopActions(ctx context.Context, tenantID int64, since time.Time, top int32) ([]RankedAction, error) {
	rows, err := r.queries.DashboardTenantTopActions(ctx, dbgen.DashboardTenantTopActionsParams{TenantID: tenantID, Since: timestamp(since), Top: top})
	if err != nil {
		return nil, database.MapError("dashboard.activity", err)
	}
	out := make([]RankedAction, 0, len(rows))
	for _, row := range rows {
		out = append(out, RankedAction{Action: row.Action, Count: row.Count})
	}
	return out, nil
}

func (r *repository) TenantTopActors(ctx context.Context, tenantID int64, since time.Time, top int32) ([]RankedActor, error) {
	rows, err := r.queries.DashboardTenantTopActors(ctx, dbgen.DashboardTenantTopActorsParams{TenantID: tenantID, Since: timestamp(since), Top: top})
	if err != nil {
		return nil, database.MapError("dashboard.activity", err)
	}
	out := make([]RankedActor, 0, len(rows))
	for _, row := range rows {
		out = append(out, RankedActor{ID: row.ActorID, Name: row.Name, Count: row.Count})
	}
	return out, nil
}

func (r *repository) TenantFiles(ctx context.Context, tenantID int64) (*FilesSummary, error) {
	files, err := r.queries.DashboardTenantFiles(ctx, tenantID)
	if err != nil {
		return nil, database.MapError("dashboard.files", err)
	}
	types, err := r.queries.DashboardTenantFilesByType(ctx, tenantID)
	if err != nil {
		return nil, database.MapError("dashboard.files", err)
	}
	out := &FilesSummary{Files: files.Files, Bytes: files.Bytes, ByType: make([]FileTypeShare, 0, len(types))}
	for _, row := range types {
		out.ByType = append(out.ByType, FileTypeShare{Type: row.Type, Files: row.Files, Bytes: row.Bytes})
	}
	return out, nil
}

func (r *repository) TenantBranches(ctx context.Context, tenantID int64) (int64, error) {
	count, err := r.queries.DashboardTenantBranches(ctx, tenantID)
	if err != nil {
		return 0, database.MapError("dashboard.branches", err)
	}
	return count, nil
}

func (r *repository) TenantRoles(ctx context.Context, tenantID int64) (int64, error) {
	count, err := r.queries.DashboardTenantRoles(ctx, tenantID)
	if err != nil {
		return 0, database.MapError("dashboard.roles", err)
	}
	return count, nil
}

func (r *repository) Organizations(ctx context.Context) (OrganizationCounts, error) {
	row, err := r.queries.DashboardPlatformTenants(ctx)
	if err != nil {
		return OrganizationCounts{}, database.MapError("dashboard.organizations", err)
	}
	return OrganizationCounts{Total: row.Total, Active: row.Active}, nil
}

func (r *repository) OrganizationsAdded(ctx context.Context, since time.Time, tz string) (DailyCounts, error) {
	rows, err := r.queries.DashboardPlatformTenantsByDay(ctx, dbgen.DashboardPlatformTenantsByDayParams{Since: timestamp(since), Tz: tz})
	if err != nil {
		return nil, database.MapError("dashboard.organizations", err)
	}
	days := DailyCounts{}
	for _, row := range rows {
		days[row.Day] = row.Count
	}
	return days, nil
}

func (r *repository) RecentOrganizations(ctx context.Context, top int32) ([]RecentOrganization, error) {
	rows, err := r.queries.DashboardPlatformRecentTenants(ctx, top)
	if err != nil {
		return nil, database.MapError("dashboard.organizations", err)
	}
	out := make([]RecentOrganization, 0, len(rows))
	for _, row := range rows {
		out = append(out, RecentOrganization{
			ID: row.ID, Name: row.Name, Code: row.Code, Active: row.IsActive, Members: row.Members, CreatedAt: row.CreatedAt.Time,
		})
	}
	return out, nil
}

func (r *repository) Users(ctx context.Context, activeSince time.Time) (UserCounts, error) {
	row, err := r.queries.DashboardPlatformUsers(ctx, timestamp(activeSince))
	if err != nil {
		return UserCounts{}, database.MapError("dashboard.users", err)
	}
	return UserCounts{Total: row.Total, SignedInRecently: row.SignedInRecently}, nil
}

func (r *repository) UsersAdded(ctx context.Context, since time.Time, tz string) (DailyCounts, error) {
	rows, err := r.queries.DashboardPlatformUsersByDay(ctx, dbgen.DashboardPlatformUsersByDayParams{Since: timestamp(since), Tz: tz})
	if err != nil {
		return nil, database.MapError("dashboard.users", err)
	}
	days := DailyCounts{}
	for _, row := range rows {
		days[row.Day] = row.Count
	}
	return days, nil
}

func (r *repository) PlatformActivity(ctx context.Context, since time.Time, tz string) (DailyCounts, error) {
	rows, err := r.queries.DashboardPlatformActivityByDay(ctx, dbgen.DashboardPlatformActivityByDayParams{Since: timestamp(since), Tz: tz})
	if err != nil {
		return nil, database.MapError("dashboard.activity", err)
	}
	days := DailyCounts{}
	for _, row := range rows {
		days[row.Day] = row.Count
	}
	return days, nil
}

func (r *repository) Revenue(ctx context.Context, since time.Time, tz string) ([]RevenueDay, error) {
	rows, err := r.queries.DashboardPlatformRevenueByDay(ctx, dbgen.DashboardPlatformRevenueByDayParams{Since: timestamp(since), Tz: tz})
	if err != nil {
		return nil, database.MapError("dashboard.revenue", err)
	}
	out := make([]RevenueDay, 0, len(rows))
	for _, row := range rows {
		out = append(out, RevenueDay{Currency: row.Currency, Day: row.Day, Amount: row.Amount, Count: row.Count})
	}
	return out, nil
}

func (r *repository) PendingPayments(ctx context.Context) ([]PendingPayments, error) {
	rows, err := r.queries.DashboardPlatformPendingPayments(ctx)
	if err != nil {
		return nil, database.MapError("dashboard.revenue", err)
	}
	out := make([]PendingPayments, 0, len(rows))
	for _, row := range rows {
		out = append(out, PendingPayments{Currency: row.Currency, Count: row.Count, Amount: row.Amount})
	}
	return out, nil
}

func (r *repository) Plans(ctx context.Context) ([]PlanShare, error) {
	rows, err := r.queries.DashboardPlatformPlans(ctx)
	if err != nil {
		return nil, database.MapError("dashboard.plans", err)
	}
	out := make([]PlanShare, 0, len(rows))
	for _, row := range rows {
		out = append(out, PlanShare{PackageID: row.PackageID, Name: row.Name, Organizations: row.Organizations})
	}
	return out, nil
}

func timestamp(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
