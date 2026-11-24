package dev

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	billingdomain "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
)

// Tenants panel: every tenant with its subscription, plan features, usage
// against limits and override counts.

// TenantRow is one tenant in the list.
type TenantRow struct {
	ID                 int64      `json:"id"`
	Name               string     `json:"name"`
	Code               string     `json:"code"`
	Domain             *string    `json:"domain"`
	Email              *string    `json:"email"`
	Phone              *string    `json:"phone"`
	IsActive           bool       `json:"is_active"`
	SubscriptionStatus *string    `json:"subscription_status"`
	PackageName        *string    `json:"package_name"`
	ExpiresAt          *time.Time `json:"expires_at"`
	Users              int64      `json:"users"`
	Branches           int64      `json:"branches"`
	Overrides          int64      `json:"overrides"`
	CreatedAt          time.Time  `json:"created_at"`
}

// TenantFeature is one plan feature with the tenant's usage.
type TenantFeature struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	IsLimit     bool   `json:"is_limit"`
	Enabled     bool   `json:"enabled"`
	Limit       *int64 `json:"limit"` // nil = unlimited (limit features only)
	Used        *int64 `json:"used"`
	Overridden  bool   `json:"overridden"`
}

// TenantSubscription is one subscription in the tenant's history.
type TenantSubscription struct {
	ID          int64      `json:"id"`
	Status      string     `json:"status"`
	PackageName string     `json:"package_name"`
	StartsAt    *time.Time `json:"starts_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	TrialEndsAt *time.Time `json:"trial_ends_at"`
	CancelledAt *time.Time `json:"cancelled_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// TenantDetail is the drawer payload.
type TenantDetail struct {
	TenantRow
	StorageBytes  int64                `json:"storage_bytes"`
	Features      []TenantFeature      `json:"features"`
	Subscriptions []TenantSubscription `json:"subscriptions"`
}

// TenantFilter narrows the tenant list.
type TenantFilter struct {
	Search       string
	Active       string // "", active, inactive
	Subscription string // "", none, trial, active, pending, past_due, cancelled, expired
	Limit        int32
	Offset       int32
}

// TenantService reads tenants for the dashboard.
type TenantService struct {
	queries *dbgen.Queries
}

// List returns one page of tenants and the total.
func (s *TenantService) List(ctx context.Context, filter TenantFilter) ([]TenantRow, int64, error) {
	rows, err := s.queries.DevListTenants(ctx, dbgen.DevListTenantsParams{
		Search: filter.Search, Active: filter.Active, Subscription: filter.Subscription,
		PageLimit: filter.Limit, PageOffset: filter.Offset,
	})
	if err != nil {
		return nil, 0, database.MapError("dev.tenants.list", err)
	}
	total, err := s.queries.DevCountTenants(ctx, dbgen.DevCountTenantsParams{
		Search: filter.Search, Active: filter.Active, Subscription: filter.Subscription,
	})
	if err != nil {
		return nil, 0, database.MapError("dev.tenants.count", err)
	}
	out := make([]TenantRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, toTenantRow(row))
	}
	return out, total, nil
}

// Detail returns one tenant with features, usage and subscription history.
func (s *TenantService) Detail(ctx context.Context, id int64) (*TenantDetail, error) {
	rows, err := s.queries.DevListTenants(ctx, dbgen.DevListTenantsParams{TenantID: id, PageLimit: 1})
	if err != nil {
		return nil, database.MapError("dev.tenants.detail", err)
	}
	var detail *TenantDetail
	var subscriptionID int64
	for _, row := range rows {
		if row.ID == id {
			detail = &TenantDetail{TenantRow: toTenantRow(row)}
			subscriptionID = row.SubscriptionID
			break
		}
	}
	if detail == nil {
		return nil, apperr.Newf(apperr.ResourceNotFound, "Tenant with ID '%d' was not found.", id)
	}
	if detail.StorageBytes, err = s.queries.DevTenantStorageBytes(ctx, &id); err != nil {
		return nil, database.MapError("dev.tenants.storage", err)
	}

	history, err := s.queries.DevTenantSubscriptions(ctx, id)
	if err != nil {
		return nil, database.MapError("dev.tenants.subscriptions", err)
	}
	var current *dbgen.DevTenantSubscriptionsRow
	for i, sub := range history {
		detail.Subscriptions = append(detail.Subscriptions, TenantSubscription{
			ID: sub.ID, Status: sub.Status, PackageName: sub.PackageName, StartsAt: timePtr(sub.StartsAt),
			ExpiresAt: timePtr(sub.ExpiresAt), TrialEndsAt: timePtr(sub.TrialEndsAt),
			CancelledAt: timePtr(sub.CancelledAt), CreatedAt: sub.CreatedAt.Time,
		})
		if sub.ID == subscriptionID {
			current = &history[i]
		}
	}
	if detail.Subscriptions == nil {
		detail.Subscriptions = []TenantSubscription{}
	}
	detail.Features, err = s.features(ctx, current, detail)
	return detail, err
}

// features resolves the plan (package features + subscription overrides) and
// the tenant's usage for limit features.
func (s *TenantService) features(ctx context.Context, current *dbgen.DevTenantSubscriptionsRow, detail *TenantDetail) ([]TenantFeature, error) {
	plan := map[string]dbgen.DevPackageFeaturesRow{}
	overrides := map[string]any{}
	if current != nil {
		rows, err := s.queries.DevPackageFeatures(ctx, current.PackageID)
		if err != nil {
			return nil, database.MapError("dev.tenants.features", err)
		}
		for _, row := range rows {
			plan[row.FeatureKey] = row
		}
		_ = json.Unmarshal(current.Overrides, &overrides)
	}
	storageMB := detail.StorageBytes / (1 << 20)
	usage := map[billingdomain.Feature]int64{
		billingdomain.FeatureUsersMax:    detail.Users,
		billingdomain.FeatureBranchesMax: detail.Branches,
		billingdomain.FeatureStorageMB:   storageMB,
	}
	out := make([]TenantFeature, 0, len(billingdomain.Features))
	for _, feature := range billingdomain.Features {
		row, inPlan := plan[string(feature)]
		item := TenantFeature{Key: string(feature), Description: feature.Description(), IsLimit: feature.IsLimit(),
			Enabled: inPlan && row.IsEnabled}
		if feature.IsLimit() {
			if inPlan && row.IsEnabled && row.LimitValue >= 0 {
				limit := row.LimitValue
				item.Limit = &limit
			}
			used := usage[feature]
			item.Used = &used
		}
		if value, ok := overrides[string(feature)]; ok {
			item.Overridden = true
			switch typed := value.(type) {
			case bool:
				item.Enabled = typed
			case float64:
				item.Enabled = true
				if typed < 0 {
					item.Limit = nil
				} else {
					limit := int64(typed)
					item.Limit = &limit
				}
			}
		}
		out = append(out, item)
	}
	return out, nil
}

func toTenantRow(row dbgen.DevListTenantsRow) TenantRow {
	out := TenantRow{
		ID: row.ID, Name: row.Name, Code: row.Code, Domain: row.Domain, Email: row.Email, Phone: row.Phone,
		IsActive: row.IsActive, PackageName: row.PackageName, Users: row.UserCount, Branches: row.BranchCount,
		Overrides: row.OverrideCount, CreatedAt: row.CreatedAt.Time,
	}
	if row.SubscriptionStatus != "" {
		status := row.SubscriptionStatus
		out.SubscriptionStatus = &status
		out.ExpiresAt = timePtr(row.ExpiresAt)
		if status == billingdomain.StatusTrial && row.TrialEndsAt.Valid {
			out.ExpiresAt = timePtr(row.TrialEndsAt)
		}
	}
	return out
}

var tenantSubscriptionFilters = map[string]bool{
	"": true, "none": true, "trial": true, "active": true, "pending": true, "past_due": true, "cancelled": true, "expired": true,
}

func (h *Handler) listTenants(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	page, perPage, offset := httpx.PageParams(r)
	filter := TenantFilter{
		Search:       strings.TrimSpace(query.Get("search")),
		Active:       strings.TrimSpace(query.Get("active")),
		Subscription: strings.TrimSpace(query.Get("subscription")),
		Limit:        int32(perPage),
		Offset:       int32(offset),
	}
	if filter.Active != "" && filter.Active != "active" && filter.Active != "inactive" {
		h.Error(w, r, apperr.Validation(map[string][]string{"active": {"The active filter must be active or inactive."}}))
		return
	}
	if !tenantSubscriptionFilters[filter.Subscription] {
		h.Error(w, r, apperr.Validation(map[string][]string{"subscription": {"Unknown subscription status."}}))
		return
	}
	rows, total, err := h.tenants.List(r.Context(), filter)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.Paginated(w, r, http.StatusOK, rows, meta, links)
}

func (h *Handler) getTenant(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	detail, err := h.tenants.Detail(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = apperr.Newf(apperr.ResourceNotFound, "Tenant with ID '%d' was not found.", id)
	}
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, detail)
}
