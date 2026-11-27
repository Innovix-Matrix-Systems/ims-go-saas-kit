package domain

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
)

// PackageRepository is the persistence contract for packages.
type PackageRepository interface {
	List(ctx context.Context) ([]Package, error)
	ListActive(ctx context.Context) ([]Package, error)
	Get(ctx context.Context, id int64) (*Package, error)
	Create(ctx context.Context, input PackageInput) (*Package, error)
	Update(ctx context.Context, id int64, input PackageInput) (*Package, error)
	Delete(ctx context.Context, id int64) error
	FeaturesFor(ctx context.Context, packageID int64) ([]PackageFeature, error)
	FeaturesForPackages(ctx context.Context, packageIDs []int64) ([]PackageFeature, error)
	ReplaceFeatures(ctx context.Context, packageID int64, features []FeatureInput) error
}

// SubscriptionRepository is the persistence contract for subscriptions.
type SubscriptionRepository interface {
	Get(ctx context.Context, id int64) (*Subscription, error)
	GetActiveForTenant(ctx context.Context, tenantID int64) (*Subscription, bool, error)
	GetLatestForTenant(ctx context.Context, tenantID int64) (*Subscription, bool, error)
	// GetLapsedForTenant is the paid plan that expired after since (its grace
	// period still runs); callers use it only when nothing is current.
	GetLapsedForTenant(ctx context.Context, tenantID int64, since time.Time) (*Subscription, bool, error)
	// HadSubscription reports whether the tenant ever had a plan that started.
	HadSubscription(ctx context.Context, tenantID int64) (bool, error)
	// ListForStaff is the staff subscriptions panel: every organization's
	// rows with names, plan and standing (platform staff only; all tenants).
	ListForStaff(ctx context.Context, filter StaffSubscriptionFilter) ([]StaffSubscription, int64, error)
	// CountForStaff counts the staff panel's views.
	CountForStaff(ctx context.Context, graceSince time.Time) (StaffSubscriptionCounts, error)
	List(ctx context.Context, status string, limit, offset int32) ([]Subscription, int64, error)
	Create(ctx context.Context, input SubscriptionInput) (*Subscription, error)
	UpdateStatus(ctx context.Context, id int64, status string, cancelledAt *time.Time, reason *string) (*Subscription, error)
	// Activate makes a subscription active; a non-nil startsAt moves the
	// start of its period (a plan starts when it is paid).
	Activate(ctx context.Context, id int64, startsAt *time.Time, expiresAt time.Time) (*Subscription, error)
	// Schedule makes a pending subscription wait for the current period to
	// end (status scheduled) with its own period and overrides.
	Schedule(ctx context.Context, id int64, startsAt, expiresAt time.Time, overrides []byte) (*Subscription, error)
	GetScheduledForTenant(ctx context.Context, tenantID int64) (*Subscription, bool, error)
	// CancelScheduled withdraws the tenant's scheduled subscription (if any).
	CancelScheduled(ctx context.Context, tenantID int64, reason string) ([]Subscription, error)
	// SetPlanEnd says what happens when the period ends: renew onto another
	// plan (nextPackageID, at nextCycle) or not at all (cancelAtPeriodEnd).
	SetPlanEnd(ctx context.Context, id int64, nextPackageID *int64, nextCycle *string, cancelAtPeriodEnd bool) (*Subscription, error)
	// ActivateDueScheduled starts scheduled subscriptions whose turn came
	// (one tenant's, or every tenant's for nil).
	ActivateDueScheduled(ctx context.Context, tenantID *int64) (int64, error)
	SetOverrides(ctx context.Context, id int64, overrides []byte) (*Subscription, error)
	DeactivateActive(ctx context.Context, tenantID int64) error
	ExpireOverdue(ctx context.Context) (int64, error)
	Expiring(ctx context.Context, before time.Time) ([]Subscription, error)
	// SuspendLapsedTenants suspends tenants whose subscription expired before
	// expiredBefore with nothing current or pending, except keepTenantID.
	SuspendLapsedTenants(ctx context.Context, expiredBefore time.Time, keepTenantID int64) ([]LapsedTenant, error)
}

// PaymentRepository is the persistence contract for payments.
type PaymentRepository interface {
	Create(ctx context.Context, input PaymentInput) (*Payment, error)
	Get(ctx context.Context, id int64) (*Payment, error)
	GetByGatewayRef(ctx context.Context, gatewayRef string) (*Payment, error)
	// PendingCheckout returns the in-flight (pending) payment of a pending
	// subscription for tenant + package + method, if any.
	PendingCheckout(ctx context.Context, tenantID, packageID int64, method string) (*Payment, bool, error)
	UpdateGateway(ctx context.Context, id int64, gatewayRef, gatewayStatus *string, response []byte) error
	UpdateStatus(ctx context.Context, id int64, status string, recordedBy *int64, gatewayStatus *string) (*Payment, error)
	ListForTenant(ctx context.Context, tenantID int64, limit, offset int32) ([]Payment, int64, error)
	// List is every organization's payments (staff), narrowed by filter.
	List(ctx context.Context, filter PaymentFilter) ([]Payment, int64, error)
	// PendingForTenant returns any checkout of the tenant still waiting.
	PendingForTenant(ctx context.Context, tenantID int64) (*Payment, bool, error)
	// PaidValue is the value paid for a subscription's period (gross less
	// refunds of its completed payments).
	PaidValue(ctx context.Context, subscriptionID int64) (int64, error)
	// SetQuote freezes a checkout quote on a payment.
	SetQuote(ctx context.Context, id int64, quote PaymentQuote) (*Payment, error)
	// Refund adds to the refunded amount (completed payments only, never
	// beyond the amount); the payment becomes refunded when fully given back.
	Refund(ctx context.Context, id, amount int64) (*Payment, error)
}

// CreditRepository is the persistence contract for account credit.
type CreditRepository interface {
	Balance(ctx context.Context, tenantID int64, currency string) (int64, error)
	Add(ctx context.Context, input CreditInput) (*Credit, error)
	ListForTenant(ctx context.Context, tenantID int64, limit, offset int32) ([]Credit, int64, error)
}

// PaymentFilter narrows the staff payments list. Empty values do not filter.
type PaymentFilter struct {
	Status string
	// TenantID keeps one organization's payments.
	TenantID *int64
	Limit    int32
	Offset   int32
}

// PaymentQuote is what a checkout froze on its payment.
type PaymentQuote struct {
	Kind                   string
	Lines                  []InvoiceLine
	Gross                  int64
	BalanceApplied         int64
	ReplacesSubscriptionID *int64
}

// CreditInput is one account credit entry (positive adds, negative uses).
type CreditInput struct {
	TenantID       int64
	Amount         int64
	Currency       string
	Reason         string
	PaymentID      *int64
	SubscriptionID *int64
	CreatedBy      *int64
}

// ApprovalRepository is the persistence contract for manual payment approvals.
type ApprovalRepository interface {
	Create(ctx context.Context, input ApprovalInput) (*Approval, error)
	Get(ctx context.Context, id int64) (*Approval, error)
	List(ctx context.Context, status string, limit, offset int32) ([]Approval, int64, error)
	Update(ctx context.Context, id int64, status string, reviewerID int64, note *string) (*Approval, error)
}

// PackageInput carries package create/update input.
type PackageInput struct {
	Name         string
	Description  *string
	Price        int64
	BillingCycle string
	// YearlyPrice offers a monthly plan yearly too (nil: monthly only).
	YearlyPrice *int64
	TrialDays   int32
	IsActive    bool
	SortOrder   int32
}

// FeatureInput is one package feature definition.
type FeatureInput struct {
	FeatureKey string
	IsEnabled  bool
	LimitValue int64
}

// SubscriptionInput carries subscription creation input.
type SubscriptionInput struct {
	TenantID    int64
	PackageID   int64
	Status      string
	StartsAt    *time.Time
	TrialEndsAt *time.Time
	ExpiresAt   *time.Time
	// BillingCycle is the cycle chosen. New rows always name it (the plan as
	// offered, Package.ForCycle); "" stores NULL, which reads as the plan's own
	// cycle (rows from before plans had two cycles).
	BillingCycle string
}

// PaymentInput carries payment creation input.
type PaymentInput struct {
	TenantID       int64
	SubscriptionID *int64
	PaymentMethod  string
	Status         string
	Amount         int64
	Currency       string
	InvoiceNumber  string
	CreatedBy      *int64
	// Kind defaults to ChangeNew, Gross to Amount.
	Kind  string
	Lines []InvoiceLine
	Gross *int64
}

// ApprovalInput carries approval creation input.
type ApprovalInput struct {
	TenantID       int64
	SubscriptionID int64
	PaymentID      int64
	PaymentMethod  string
	PaymentData    []byte
	CreatedBy      *int64
}

// ── Packages ───────────────────────────────────────────────────────────────

type packageRepository struct {
	queries *dbgen.Queries
}

// NewPackageRepository creates the sqlc-backed package repository.
func NewPackageRepository(queries *dbgen.Queries) PackageRepository {
	return &packageRepository{queries: queries}
}

func (r *packageRepository) List(ctx context.Context) ([]Package, error) {
	rows, err := r.queries.ListPackages(ctx)
	if err != nil {
		return nil, database.MapError("billing.list_packages", err)
	}
	return mapPackages(rows), nil
}

func (r *packageRepository) ListActive(ctx context.Context) ([]Package, error) {
	rows, err := r.queries.ListActivePackages(ctx)
	if err != nil {
		return nil, database.MapError("billing.list_active_packages", err)
	}
	return mapPackages(rows), nil
}

func (r *packageRepository) Get(ctx context.Context, id int64) (*Package, error) {
	row, err := r.queries.GetPackage(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Package with ID '%d' was not found.", id)
		}
		return nil, database.MapError("billing.get_package", err)
	}
	pkg := mapPackage(row)
	return &pkg, nil
}

func (r *packageRepository) Create(ctx context.Context, input PackageInput) (*Package, error) {
	row, err := r.queries.CreatePackage(ctx, dbgen.CreatePackageParams{
		Name:         input.Name,
		Description:  input.Description,
		Price:        input.Price,
		BillingCycle: input.BillingCycle,
		YearlyPrice:  input.YearlyPrice,
		TrialDays:    input.TrialDays,
		IsActive:     input.IsActive,
		SortOrder:    input.SortOrder,
	})
	if err != nil {
		return nil, database.MapError("billing.create_package", err)
	}
	pkg := mapPackage(row)
	return &pkg, nil
}

func (r *packageRepository) Update(ctx context.Context, id int64, input PackageInput) (*Package, error) {
	row, err := r.queries.UpdatePackage(ctx, dbgen.UpdatePackageParams{
		ID:           id,
		Name:         input.Name,
		Description:  input.Description,
		Price:        input.Price,
		BillingCycle: input.BillingCycle,
		YearlyPrice:  input.YearlyPrice,
		TrialDays:    input.TrialDays,
		IsActive:     input.IsActive,
		SortOrder:    input.SortOrder,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Package with ID '%d' was not found.", id)
		}
		return nil, database.MapError("billing.update_package", err)
	}
	pkg := mapPackage(row)
	return &pkg, nil
}

func (r *packageRepository) Delete(ctx context.Context, id int64) error {
	affected, err := r.queries.DeletePackage(ctx, id)
	if err != nil {
		return database.MapError("billing.delete_package", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.ResourceNotFound, "Package with ID '%d' was not found.", id)
	}
	return nil
}

func (r *packageRepository) FeaturesFor(ctx context.Context, packageID int64) ([]PackageFeature, error) {
	rows, err := r.queries.ListPackageFeatures(ctx, packageID)
	if err != nil {
		return nil, database.MapError("billing.package_features", err)
	}
	return mapFeatures(rows), nil
}

func (r *packageRepository) FeaturesForPackages(ctx context.Context, packageIDs []int64) ([]PackageFeature, error) {
	if len(packageIDs) == 0 {
		return []PackageFeature{}, nil
	}
	rows, err := r.queries.ListFeaturesForPackages(ctx, packageIDs)
	if err != nil {
		return nil, database.MapError("billing.package_features_bulk", err)
	}
	return mapFeatures(rows), nil
}

func (r *packageRepository) ReplaceFeatures(ctx context.Context, packageID int64, features []FeatureInput) error {
	if err := r.queries.DeletePackageFeatures(ctx, packageID); err != nil {
		return database.MapError("billing.clear_package_features", err)
	}
	for _, feature := range features {
		if _, err := r.queries.UpsertPackageFeature(ctx, dbgen.UpsertPackageFeatureParams{
			PackageID:  packageID,
			FeatureKey: feature.FeatureKey,
			IsEnabled:  feature.IsEnabled,
			LimitValue: feature.LimitValue,
		}); err != nil {
			return database.MapError("billing.upsert_package_feature", err)
		}
	}
	return nil
}

// ── Subscriptions ──────────────────────────────────────────────────────────

type subscriptionRepository struct {
	queries *dbgen.Queries
}

// NewSubscriptionRepository creates the sqlc-backed subscription repository.
func NewSubscriptionRepository(queries *dbgen.Queries) SubscriptionRepository {
	return &subscriptionRepository{queries: queries}
}

func (r *subscriptionRepository) Get(ctx context.Context, id int64) (*Subscription, error) {
	row, err := r.queries.GetSubscription(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Subscription with ID '%d' was not found.", id)
		}
		return nil, database.MapError("billing.get_subscription", err)
	}
	sub := mapSubscription(row)
	return &sub, nil
}

func (r *subscriptionRepository) GetActiveForTenant(ctx context.Context, tenantID int64) (*Subscription, bool, error) {
	row, err := r.queries.GetActiveSubscriptionForTenant(ctx, tenantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, database.MapError("billing.active_subscription", err)
	}
	sub := mapSubscription(row)
	return &sub, true, nil
}

func (r *subscriptionRepository) GetLatestForTenant(ctx context.Context, tenantID int64) (*Subscription, bool, error) {
	row, err := r.queries.GetLatestSubscriptionForTenant(ctx, tenantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, database.MapError("billing.latest_subscription", err)
	}
	sub := mapSubscription(row)
	return &sub, true, nil
}

func (r *subscriptionRepository) GetLapsedForTenant(ctx context.Context, tenantID int64, since time.Time) (*Subscription, bool, error) {
	row, err := r.queries.GetLapsedSubscriptionForTenant(ctx, dbgen.GetLapsedSubscriptionForTenantParams{
		TenantID: tenantID, Since: timestamptz(since),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, database.MapError("billing.lapsed_subscription", err)
	}
	sub := mapSubscription(row)
	return &sub, true, nil
}

func (r *subscriptionRepository) HadSubscription(ctx context.Context, tenantID int64) (bool, error) {
	had, err := r.queries.TenantHadSubscription(ctx, tenantID)
	if err != nil {
		return false, database.MapError("billing.had_subscription", err)
	}
	return had, nil
}

func (r *subscriptionRepository) ListForStaff(ctx context.Context, filter StaffSubscriptionFilter) ([]StaffSubscription, int64, error) {
	rows, err := r.queries.ListStaffSubscriptions(ctx, dbgen.ListStaffSubscriptionsParams{
		View:          filter.View,
		Status:        optionalString(filter.Status),
		Search:        optionalString(filter.Search),
		PackageID:     filter.PackageID,
		BillingCycle:  optionalString(filter.BillingCycle),
		ExpiresBefore: timestamptzPtr(filter.ExpiresBefore),
		OrderBy:       filter.OrderBy,
		OrderDir:      filter.OrderDir,
		GraceSince:    timestamptz(filter.GraceSince),
		PageLimit:     filter.Limit,
		PageOffset:    filter.Offset,
	})
	if err != nil {
		return nil, 0, database.MapError("billing.list_staff_subscriptions", err)
	}
	total, err := r.queries.CountStaffSubscriptions(ctx, dbgen.CountStaffSubscriptionsParams{
		View:          filter.View,
		Status:        optionalString(filter.Status),
		Search:        optionalString(filter.Search),
		PackageID:     filter.PackageID,
		BillingCycle:  optionalString(filter.BillingCycle),
		ExpiresBefore: timestamptzPtr(filter.ExpiresBefore),
		GraceSince:    timestamptz(filter.GraceSince),
	})
	if err != nil {
		return nil, 0, database.MapError("billing.count_staff_subscriptions", err)
	}
	out := make([]StaffSubscription, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapStaffSubscription(row))
	}
	return out, total, nil
}

func (r *subscriptionRepository) CountForStaff(ctx context.Context, graceSince time.Time) (StaffSubscriptionCounts, error) {
	row, err := r.queries.StaffSubscriptionCounts(ctx, timestamptz(graceSince))
	if err != nil {
		return StaffSubscriptionCounts{}, database.MapError("billing.count_staff_subscriptions", err)
	}
	return StaffSubscriptionCounts{
		Active: row.Active, Trial: row.Trial, Grace: row.Grace, ReadOnly: row.ReadOnly,
		Pending: row.Pending, Scheduled: row.Scheduled, All: row.AllRows,
	}, nil
}

func (r *subscriptionRepository) List(ctx context.Context, status string, limit, offset int32) ([]Subscription, int64, error) {
	rows, err := r.queries.ListSubscriptions(ctx, dbgen.ListSubscriptionsParams{
		Status:     optionalString(status),
		PageLimit:  limit,
		PageOffset: offset,
	})
	if err != nil {
		return nil, 0, database.MapError("billing.list_subscriptions", err)
	}
	total, err := r.queries.CountSubscriptions(ctx, optionalString(status))
	if err != nil {
		return nil, 0, database.MapError("billing.count_subscriptions", err)
	}
	items := make([]Subscription, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapSubscription(row))
	}
	return items, total, nil
}

func (r *subscriptionRepository) Create(ctx context.Context, input SubscriptionInput) (*Subscription, error) {
	row, err := r.queries.CreateSubscription(ctx, dbgen.CreateSubscriptionParams{
		TenantID:     input.TenantID,
		PackageID:    input.PackageID,
		Status:       input.Status,
		StartsAt:     timestamptzPtr(input.StartsAt),
		TrialEndsAt:  timestamptzPtr(input.TrialEndsAt),
		ExpiresAt:    timestamptzPtr(input.ExpiresAt),
		BillingCycle: optionalString(input.BillingCycle),
	})
	if err != nil {
		return nil, database.MapError("billing.create_subscription", err)
	}
	sub := mapSubscription(row)
	return &sub, nil
}

func (r *subscriptionRepository) UpdateStatus(ctx context.Context, id int64, status string, cancelledAt *time.Time, reason *string) (*Subscription, error) {
	row, err := r.queries.UpdateSubscriptionStatus(ctx, dbgen.UpdateSubscriptionStatusParams{
		ID:                 id,
		Status:             status,
		CancelledAt:        timestamptzPtr(cancelledAt),
		CancellationReason: reason,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Subscription with ID '%d' was not found.", id)
		}
		return nil, database.MapError("billing.update_subscription_status", err)
	}
	sub := mapSubscription(row)
	return &sub, nil
}

func (r *subscriptionRepository) Activate(ctx context.Context, id int64, startsAt *time.Time, expiresAt time.Time) (*Subscription, error) {
	row, err := r.queries.ActivateSubscription(ctx, dbgen.ActivateSubscriptionParams{
		ID:        id,
		StartsAt:  timestamptzPtr(startsAt),
		ExpiresAt: timestamptz(expiresAt),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.InvalidState, "Subscription '%d' was not found or has been cancelled.", id)
		}
		return nil, database.MapError("billing.activate_subscription", err)
	}
	sub := mapSubscription(row)
	return &sub, nil
}

func (r *subscriptionRepository) Schedule(ctx context.Context, id int64, startsAt, expiresAt time.Time, overrides []byte) (*Subscription, error) {
	if len(overrides) == 0 {
		overrides = []byte("{}")
	}
	row, err := r.queries.ScheduleSubscription(ctx, dbgen.ScheduleSubscriptionParams{
		ID: id, StartsAt: timestamptz(startsAt), ExpiresAt: timestamptz(expiresAt), Overrides: overrides,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.InvalidState, "Subscription '%d' is no longer waiting for payment.", id)
		}
		return nil, database.MapError("billing.schedule_subscription", err)
	}
	sub := mapSubscription(row)
	return &sub, nil
}

func (r *subscriptionRepository) GetScheduledForTenant(ctx context.Context, tenantID int64) (*Subscription, bool, error) {
	row, err := r.queries.GetScheduledSubscriptionForTenant(ctx, tenantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, database.MapError("billing.scheduled_subscription", err)
	}
	sub := mapSubscription(row)
	return &sub, true, nil
}

func (r *subscriptionRepository) CancelScheduled(ctx context.Context, tenantID int64, reason string) ([]Subscription, error) {
	rows, err := r.queries.CancelScheduledSubscriptions(ctx, dbgen.CancelScheduledSubscriptionsParams{TenantID: tenantID, Reason: &reason})
	if err != nil {
		return nil, database.MapError("billing.cancel_scheduled", err)
	}
	items := make([]Subscription, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapSubscription(row))
	}
	return items, nil
}

func (r *subscriptionRepository) SetPlanEnd(ctx context.Context, id int64, nextPackageID *int64, nextCycle *string, cancelAtPeriodEnd bool) (*Subscription, error) {
	row, err := r.queries.SetSubscriptionPlanEnd(ctx, dbgen.SetSubscriptionPlanEndParams{
		ID: id, NextPackageID: nextPackageID, NextBillingCycle: nextCycle, CancelAtPeriodEnd: cancelAtPeriodEnd,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Subscription with ID '%d' was not found.", id)
		}
		return nil, database.MapError("billing.set_plan_end", err)
	}
	sub := mapSubscription(row)
	return &sub, nil
}

func (r *subscriptionRepository) ActivateDueScheduled(ctx context.Context, tenantID *int64) (int64, error) {
	affected, err := r.queries.ActivateDueScheduledSubscriptions(ctx, tenantID)
	if err != nil {
		return 0, database.MapError("billing.activate_due_scheduled", err)
	}
	return affected, nil
}

func (r *subscriptionRepository) SetOverrides(ctx context.Context, id int64, overrides []byte) (*Subscription, error) {
	row, err := r.queries.SetSubscriptionOverrides(ctx, dbgen.SetSubscriptionOverridesParams{
		ID:        id,
		Overrides: overrides,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Subscription with ID '%d' was not found.", id)
		}
		return nil, database.MapError("billing.set_overrides", err)
	}
	sub := mapSubscription(row)
	return &sub, nil
}

func (r *subscriptionRepository) DeactivateActive(ctx context.Context, tenantID int64) error {
	if err := r.queries.DeactivateActiveSubscriptions(ctx, tenantID); err != nil {
		return database.MapError("billing.deactivate_subscriptions", err)
	}
	return nil
}

func (r *subscriptionRepository) ExpireOverdue(ctx context.Context) (int64, error) {
	affected, err := r.queries.ExpireSubscriptions(ctx)
	if err != nil {
		return 0, database.MapError("billing.expire_subscriptions", err)
	}
	return affected, nil
}

func (r *subscriptionRepository) SuspendLapsedTenants(ctx context.Context, expiredBefore time.Time, keepTenantID int64) ([]LapsedTenant, error) {
	rows, err := r.queries.SuspendLapsedTenants(ctx, dbgen.SuspendLapsedTenantsParams{
		KeepTenantID:  keepTenantID,
		ExpiredBefore: timestamptz(expiredBefore),
	})
	if err != nil {
		return nil, database.MapError("billing.suspend_lapsed_tenants", err)
	}
	tenants := make([]LapsedTenant, 0, len(rows))
	for _, row := range rows {
		tenants = append(tenants, LapsedTenant{ID: row.ID, Name: row.Name, Code: row.Code})
	}
	return tenants, nil
}

func (r *subscriptionRepository) Expiring(ctx context.Context, before time.Time) ([]Subscription, error) {
	rows, err := r.queries.GetExpiringSubscriptions(ctx, timestamptz(before))
	if err != nil {
		return nil, database.MapError("billing.expiring_subscriptions", err)
	}
	items := make([]Subscription, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapSubscription(row))
	}
	return items, nil
}

// ── Payments ───────────────────────────────────────────────────────────────

type paymentRepository struct {
	queries *dbgen.Queries
}

// NewPaymentRepository creates the sqlc-backed payment repository.
func NewPaymentRepository(queries *dbgen.Queries) PaymentRepository {
	return &paymentRepository{queries: queries}
}

func (r *paymentRepository) Create(ctx context.Context, input PaymentInput) (*Payment, error) {
	kind := input.Kind
	if kind == "" {
		kind = ChangeNew
	}
	gross := input.Amount
	if input.Gross != nil {
		gross = *input.Gross
	}
	row, err := r.queries.CreatePayment(ctx, dbgen.CreatePaymentParams{
		TenantID:       input.TenantID,
		SubscriptionID: input.SubscriptionID,
		PaymentMethod:  input.PaymentMethod,
		Status:         input.Status,
		Amount:         input.Amount,
		Currency:       input.Currency,
		InvoiceNumber:  input.InvoiceNumber,
		CreatedBy:      input.CreatedBy,
		Kind:           kind,
		Lines:          EncodeLines(input.Lines),
		Gross:          gross,
	})
	if err != nil {
		return nil, database.MapError("billing.create_payment", err)
	}
	payment := mapPayment(row)
	return &payment, nil
}

func (r *paymentRepository) Get(ctx context.Context, id int64) (*Payment, error) {
	row, err := r.queries.GetPayment(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Payment with ID '%d' was not found.", id)
		}
		return nil, database.MapError("billing.get_payment", err)
	}
	payment := mapPayment(row)
	return &payment, nil
}

func (r *paymentRepository) GetByGatewayRef(ctx context.Context, gatewayRef string) (*Payment, error) {
	row, err := r.queries.GetPaymentByGatewayRef(ctx, optionalString(gatewayRef))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Payment with gateway reference '%s' was not found.", gatewayRef)
		}
		return nil, database.MapError("billing.get_payment_by_ref", err)
	}
	payment := mapPayment(row)
	return &payment, nil
}

func (r *paymentRepository) PendingCheckout(ctx context.Context, tenantID, packageID int64, method string) (*Payment, bool, error) {
	row, err := r.queries.GetPendingCheckout(ctx, dbgen.GetPendingCheckoutParams{
		TenantID:      tenantID,
		PackageID:     packageID,
		PaymentMethod: method,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, database.MapError("billing.pending_checkout", err)
	}
	payment := mapPayment(row)
	return &payment, true, nil
}

func (r *paymentRepository) UpdateGateway(ctx context.Context, id int64, gatewayRef, gatewayStatus *string, response []byte) error {
	if _, err := r.queries.UpdatePaymentGateway(ctx, dbgen.UpdatePaymentGatewayParams{
		ID:              id,
		GatewayRef:      gatewayRef,
		GatewayStatus:   gatewayStatus,
		GatewayResponse: response,
	}); err != nil {
		return database.MapError("billing.update_payment_gateway", err)
	}
	return nil
}

func (r *paymentRepository) UpdateStatus(ctx context.Context, id int64, status string, recordedBy *int64, gatewayStatus *string) (*Payment, error) {
	row, err := r.queries.UpdatePaymentStatus(ctx, dbgen.UpdatePaymentStatusParams{
		ID:            id,
		Status:        status,
		RecordedBy:    recordedBy,
		GatewayStatus: gatewayStatus,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.InvalidState, "Payment '%d' was not found or cannot change to %s.", id, status)
		}
		return nil, database.MapError("billing.update_payment_status", err)
	}
	payment := mapPayment(row)
	return &payment, nil
}

func (r *paymentRepository) ListForTenant(ctx context.Context, tenantID int64, limit, offset int32) ([]Payment, int64, error) {
	rows, err := r.queries.ListPaymentsForTenant(ctx, dbgen.ListPaymentsForTenantParams{
		TenantID:   tenantID,
		PageLimit:  limit,
		PageOffset: offset,
	})
	if err != nil {
		return nil, 0, database.MapError("billing.list_payments", err)
	}
	total, err := r.queries.CountPaymentsForTenant(ctx, tenantID)
	if err != nil {
		return nil, 0, database.MapError("billing.count_payments", err)
	}
	items := make([]Payment, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapPayment(row))
	}
	return items, total, nil
}

func (r *paymentRepository) List(ctx context.Context, filter PaymentFilter) ([]Payment, int64, error) {
	rows, err := r.queries.ListPayments(ctx, dbgen.ListPaymentsParams{
		Status:     optionalString(filter.Status),
		TenantID:   filter.TenantID,
		PageLimit:  filter.Limit,
		PageOffset: filter.Offset,
	})
	if err != nil {
		return nil, 0, database.MapError("billing.list_payments_all", err)
	}
	total, err := r.queries.CountPayments(ctx, dbgen.CountPaymentsParams{Status: optionalString(filter.Status), TenantID: filter.TenantID})
	if err != nil {
		return nil, 0, database.MapError("billing.count_payments_all", err)
	}
	items := make([]Payment, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapPayment(row))
	}
	return items, total, nil
}

// ── Approvals ──────────────────────────────────────────────────────────────

type approvalRepository struct {
	queries *dbgen.Queries
}

// NewApprovalRepository creates the sqlc-backed approval repository.
func NewApprovalRepository(queries *dbgen.Queries) ApprovalRepository {
	return &approvalRepository{queries: queries}
}

func (r *approvalRepository) Create(ctx context.Context, input ApprovalInput) (*Approval, error) {
	row, err := r.queries.CreatePaymentApproval(ctx, dbgen.CreatePaymentApprovalParams{
		TenantID:       input.TenantID,
		SubscriptionID: input.SubscriptionID,
		PaymentID:      input.PaymentID,
		PaymentMethod:  input.PaymentMethod,
		PaymentData:    input.PaymentData,
		Status:         ApprovalPending,
		CreatedBy:      input.CreatedBy,
	})
	if err != nil {
		return nil, database.MapError("billing.create_approval", err)
	}
	approval := mapApproval(row)
	return &approval, nil
}

func (r *approvalRepository) Get(ctx context.Context, id int64) (*Approval, error) {
	row, err := r.queries.GetPaymentApproval(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Payment approval with ID '%d' was not found.", id)
		}
		return nil, database.MapError("billing.get_approval", err)
	}
	approval := mapApproval(row)
	return &approval, nil
}

func (r *approvalRepository) List(ctx context.Context, status string, limit, offset int32) ([]Approval, int64, error) {
	rows, err := r.queries.ListPaymentApprovals(ctx, dbgen.ListPaymentApprovalsParams{
		Status:     optionalString(status),
		PageLimit:  limit,
		PageOffset: offset,
	})
	if err != nil {
		return nil, 0, database.MapError("billing.list_approvals", err)
	}
	total, err := r.queries.CountPaymentApprovals(ctx, optionalString(status))
	if err != nil {
		return nil, 0, database.MapError("billing.count_approvals", err)
	}
	items := make([]Approval, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapApproval(row))
	}
	return items, total, nil
}

func (r *approvalRepository) Update(ctx context.Context, id int64, status string, reviewerID int64, note *string) (*Approval, error) {
	row, err := r.queries.UpdatePaymentApproval(ctx, dbgen.UpdatePaymentApprovalParams{
		ID:           id,
		Status:       status,
		ReviewerID:   &reviewerID,
		ReviewerNote: note,
		UpdatedBy:    &reviewerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Payment approval with ID '%d' was not found.", id)
		}
		return nil, database.MapError("billing.update_approval", err)
	}
	approval := mapApproval(row)
	return &approval, nil
}

// ── Mappers ────────────────────────────────────────────────────────────────

func mapPackages(rows []dbgen.Package) []Package {
	items := make([]Package, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapPackage(row))
	}
	return items
}

func mapFeatures(rows []dbgen.PackageFeature) []PackageFeature {
	items := make([]PackageFeature, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapPackageFeature(row))
	}
	return items
}

// ReportRepository reads a tenant's invoices and what it uses of its plan.
type ReportRepository interface {
	// Invoice is one of the tenant's payments as an invoice (Issuer left empty).
	Invoice(ctx context.Context, tenantID, paymentID int64) (*Invoice, error)
	Usage(ctx context.Context, tenantID int64) (TenantUsage, error)
}

type reportRepository struct {
	queries *dbgen.Queries
}

// NewReportRepository creates the sqlc-backed report repository.
func NewReportRepository(queries *dbgen.Queries) ReportRepository {
	return &reportRepository{queries: queries}
}

func (r *reportRepository) Invoice(ctx context.Context, tenantID, paymentID int64) (*Invoice, error) {
	row, err := r.queries.InvoiceForPayment(ctx, dbgen.InvoiceForPaymentParams{PaymentID: paymentID, TenantID: tenantID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.Newf(apperr.ResourceNotFound, "Payment with ID '%d' was not found.", paymentID)
	}
	if err != nil {
		return nil, database.MapError("billing.invoice", err)
	}
	return &Invoice{
		Number: row.InvoiceNumber, Status: row.Status, PaymentMethod: row.PaymentMethod, Amount: row.Amount,
		Currency: row.Currency, Reference: row.GatewayRef, IssuedAt: row.CreatedAt.Time, TenantName: row.TenantName,
		TenantEmail: row.TenantEmail, TenantPhone: row.TenantPhone, PackageName: row.PackageName, BillingCycle: row.BillingCycle,
		PaidAt: timePtr(row.PaidAt), PeriodStart: timePtr(row.StartsAt), PeriodEnd: timePtr(row.ExpiresAt),
		Lines: DecodeLines(row.Lines), RefundedAmount: row.RefundedAmount,
	}, nil
}

func (r *reportRepository) Usage(ctx context.Context, tenantID int64) (TenantUsage, error) {
	row, err := r.queries.TenantUsage(ctx, tenantID)
	if err != nil {
		return TenantUsage{}, database.MapError("billing.usage", err)
	}
	return TenantUsage{Users: row.Users, Branches: row.Branches, StorageBytes: row.StorageBytes}, nil
}

func (r *paymentRepository) PendingForTenant(ctx context.Context, tenantID int64) (*Payment, bool, error) {
	row, err := r.queries.GetPendingPaymentForTenant(ctx, tenantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, database.MapError("billing.pending_payment", err)
	}
	payment := mapPayment(row)
	return &payment, true, nil
}

func (r *paymentRepository) PaidValue(ctx context.Context, subscriptionID int64) (int64, error) {
	paid, err := r.queries.SubscriptionPaidValue(ctx, &subscriptionID)
	if err != nil {
		return 0, database.MapError("billing.paid_value", err)
	}
	return max(paid, 0), nil
}

func (r *paymentRepository) SetQuote(ctx context.Context, id int64, quote PaymentQuote) (*Payment, error) {
	kind := quote.Kind
	if kind == "" {
		kind = ChangeNew
	}
	row, err := r.queries.SetPaymentQuote(ctx, dbgen.SetPaymentQuoteParams{
		ID: id, Kind: kind, Lines: EncodeLines(quote.Lines), Gross: quote.Gross,
		BalanceApplied: quote.BalanceApplied, ReplacesSubscriptionID: quote.ReplacesSubscriptionID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Payment with ID '%d' was not found.", id)
		}
		return nil, database.MapError("billing.set_payment_quote", err)
	}
	payment := mapPayment(row)
	return &payment, nil
}

func (r *paymentRepository) Refund(ctx context.Context, id, amount int64) (*Payment, error) {
	row, err := r.queries.RefundPayment(ctx, dbgen.RefundPaymentParams{ID: id, Refund: amount})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.New(apperr.InvalidState, "Only a completed payment can be refunded, and never for more than was paid.")
		}
		return nil, database.MapError("billing.refund_payment", err)
	}
	payment := mapPayment(row)
	return &payment, nil
}

// ── Account credit ─────────────────────────────────────────────────────────

type creditRepository struct {
	queries *dbgen.Queries
}

// NewCreditRepository creates the sqlc-backed account credit repository.
func NewCreditRepository(queries *dbgen.Queries) CreditRepository {
	return &creditRepository{queries: queries}
}

func (r *creditRepository) Balance(ctx context.Context, tenantID int64, currency string) (int64, error) {
	balance, err := r.queries.CreditBalance(ctx, dbgen.CreditBalanceParams{TenantID: tenantID, Currency: currency})
	if err != nil {
		return 0, database.MapError("billing.credit_balance", err)
	}
	return balance, nil
}

func (r *creditRepository) Add(ctx context.Context, input CreditInput) (*Credit, error) {
	row, err := r.queries.InsertCredit(ctx, dbgen.InsertCreditParams{
		TenantID: input.TenantID, Amount: input.Amount, Currency: input.Currency, Reason: input.Reason,
		PaymentID: input.PaymentID, SubscriptionID: input.SubscriptionID, CreatedBy: input.CreatedBy,
	})
	if err != nil {
		return nil, database.MapError("billing.add_credit", err)
	}
	credit := mapCredit(row)
	return &credit, nil
}

func (r *creditRepository) ListForTenant(ctx context.Context, tenantID int64, limit, offset int32) ([]Credit, int64, error) {
	rows, err := r.queries.ListCreditsForTenant(ctx, dbgen.ListCreditsForTenantParams{TenantID: tenantID, PageLimit: limit, PageOffset: offset})
	if err != nil {
		return nil, 0, database.MapError("billing.list_credits", err)
	}
	total, err := r.queries.CountCreditsForTenant(ctx, tenantID)
	if err != nil {
		return nil, 0, database.MapError("billing.count_credits", err)
	}
	items := make([]Credit, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapCredit(row))
	}
	return items, total, nil
}
