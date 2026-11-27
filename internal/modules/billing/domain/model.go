package domain

import (
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/money"
)

// Package is the domain representation of a billing plan.
type Package struct {
	ID           int64
	Name         string
	Description  *string
	Price        int64
	BillingCycle string
	// YearlyPrice offers a monthly plan yearly too, at this price (nil:
	// monthly only). See ForCycle.
	YearlyPrice *int64
	TrialDays   int32
	IsActive    bool
	SortOrder   int32
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Features    []PackageFeature
}

// PackageFeature is one feature flag/limit of a package.
type PackageFeature struct {
	ID         int64
	PackageID  int64
	FeatureKey string
	IsEnabled  bool
	LimitValue int64
}

// LapsedTenant is a tenant suspended because its subscription expired.
type LapsedTenant struct {
	ID   int64
	Name string
	Code string
}

// Subscription is the domain representation of a tenant subscription.
type Subscription struct {
	ID                 int64
	TenantID           int64
	PackageID          int64
	Status             string
	StartsAt           *time.Time
	TrialEndsAt        *time.Time
	ExpiresAt          *time.Time
	CancelledAt        *time.Time
	CancellationReason *string
	Overrides          map[string]any
	// NextPackageID is the plan the organization renews onto (a downgrade
	// chosen before the end of the period).
	NextPackageID *int64
	// CancelAtPeriodEnd: the organization will not renew; the plan runs
	// until it expires.
	CancelAtPeriodEnd bool
	// BillingCycle is the cycle the organization chose (nil: the plan's own
	// cycle); NextBillingCycle goes with NextPackageID. See Cycle.
	BillingCycle     *string
	NextBillingCycle *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Payment is the domain representation of a payment attempt.
type Payment struct {
	ID              int64
	TenantID        int64
	SubscriptionID  *int64
	PaymentMethod   string
	Status          string
	Amount          int64
	Currency        string
	GatewayRef      *string
	GatewayStatus   *string
	GatewayResponse map[string]any
	InvoiceNumber   string
	PaidAt          *time.Time
	RecordedBy      *int64
	CreatedBy       *int64
	UpdatedBy       *int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
	// Kind is why it was paid (ChangeNew, ChangeRenew, ChangeUpgrade, ...).
	Kind string
	// Lines are the invoice lines frozen at checkout (empty for old payments).
	Lines []InvoiceLine
	// Gross is the value of the period bought: the plan price before credits.
	Gross int64
	// BalanceApplied is the account credit this payment uses.
	BalanceApplied int64
	// ReplacesSubscriptionID is the plan an upgrade replaces (its unused time
	// was credited).
	ReplacesSubscriptionID *int64
	// RefundedAmount is the money given back so far.
	RefundedAmount int64
}

// Credit is one entry of an organization's account credit (positive: added,
// negative: used).
type Credit struct {
	ID             int64
	TenantID       int64
	Amount         int64
	Currency       string
	Reason         string
	PaymentID      *int64
	SubscriptionID *int64
	CreatedBy      *int64
	CreatedAt      time.Time
}

// Approval is the domain representation of a manual payment review.
type Approval struct {
	ID             int64
	TenantID       int64
	SubscriptionID int64
	PaymentID      int64
	PaymentMethod  string
	PaymentData    map[string]any
	Status         string
	ReviewerID     *int64
	ReviewerNote   *string
	ReviewedAt     *time.Time
	CreatedBy      *int64
	UpdatedBy      *int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func mapPackage(row dbgen.Package) Package {
	return Package{
		ID:           row.ID,
		Name:         row.Name,
		Description:  row.Description,
		Price:        row.Price,
		BillingCycle: row.BillingCycle,
		YearlyPrice:  row.YearlyPrice,
		TrialDays:    row.TrialDays,
		IsActive:     row.IsActive,
		SortOrder:    row.SortOrder,
		CreatedAt:    row.CreatedAt.Time,
		UpdatedAt:    row.UpdatedAt.Time,
	}
}

func mapPackageFeature(row dbgen.PackageFeature) PackageFeature {
	return PackageFeature{
		ID:         row.ID,
		PackageID:  row.PackageID,
		FeatureKey: row.FeatureKey,
		IsEnabled:  row.IsEnabled,
		LimitValue: row.LimitValue,
	}
}

func mapSubscription(row dbgen.Subscription) Subscription {
	overrides := map[string]any{}
	if len(row.Overrides) > 0 {
		_ = json.Unmarshal(row.Overrides, &overrides)
	}
	return Subscription{
		ID:                 row.ID,
		TenantID:           row.TenantID,
		PackageID:          row.PackageID,
		Status:             row.Status,
		StartsAt:           timePtr(row.StartsAt),
		TrialEndsAt:        timePtr(row.TrialEndsAt),
		ExpiresAt:          timePtr(row.ExpiresAt),
		CancelledAt:        timePtr(row.CancelledAt),
		CancellationReason: row.CancellationReason,
		Overrides:          overrides,
		NextPackageID:      row.NextPackageID,
		CancelAtPeriodEnd:  row.CancelAtPeriodEnd,
		BillingCycle:       row.BillingCycle,
		NextBillingCycle:   row.NextBillingCycle,
		CreatedAt:          row.CreatedAt.Time,
		UpdatedAt:          row.UpdatedAt.Time,
	}
}

func mapPayment(row dbgen.Payment) Payment {
	response := map[string]any{}
	if len(row.GatewayResponse) > 0 {
		_ = json.Unmarshal(row.GatewayResponse, &response)
	}
	return Payment{
		ID:                     row.ID,
		TenantID:               row.TenantID,
		SubscriptionID:         row.SubscriptionID,
		PaymentMethod:          row.PaymentMethod,
		Status:                 row.Status,
		Amount:                 row.Amount,
		Currency:               row.Currency,
		GatewayRef:             row.GatewayRef,
		GatewayStatus:          row.GatewayStatus,
		GatewayResponse:        response,
		InvoiceNumber:          row.InvoiceNumber,
		PaidAt:                 timePtr(row.PaidAt),
		RecordedBy:             row.RecordedBy,
		CreatedBy:              row.CreatedBy,
		UpdatedBy:              row.UpdatedBy,
		CreatedAt:              row.CreatedAt.Time,
		UpdatedAt:              row.UpdatedAt.Time,
		Kind:                   row.Kind,
		Lines:                  DecodeLines(row.Lines),
		Gross:                  row.Gross,
		BalanceApplied:         row.BalanceApplied,
		ReplacesSubscriptionID: row.ReplacesSubscriptionID,
		RefundedAmount:         row.RefundedAmount,
	}
}

func mapCredit(row dbgen.BillingCredit) Credit {
	return Credit{
		ID: row.ID, TenantID: row.TenantID, Amount: row.Amount, Currency: row.Currency, Reason: row.Reason,
		PaymentID: row.PaymentID, SubscriptionID: row.SubscriptionID, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt.Time,
	}
}

func mapApproval(row dbgen.PaymentApprovalRequest) Approval {
	data := map[string]any{}
	if len(row.PaymentData) > 0 {
		_ = json.Unmarshal(row.PaymentData, &data)
	}
	return Approval{
		ID:             row.ID,
		TenantID:       row.TenantID,
		SubscriptionID: row.SubscriptionID,
		PaymentID:      row.PaymentID,
		PaymentMethod:  row.PaymentMethod,
		PaymentData:    data,
		Status:         row.Status,
		ReviewerID:     row.ReviewerID,
		ReviewerNote:   row.ReviewerNote,
		ReviewedAt:     timePtr(row.ReviewedAt),
		CreatedBy:      row.CreatedBy,
		UpdatedBy:      row.UpdatedBy,
		CreatedAt:      row.CreatedAt.Time,
		UpdatedAt:      row.UpdatedAt.Time,
	}
}

func timePtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}

func timestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func timestamptzPtr(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return timestamptz(*value)
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// Invoice is everything an invoice document shows for one payment.
type Invoice struct {
	Number        string
	Status        string
	PaymentMethod string
	Amount        int64
	Currency      string
	// Money is Amount written the way its currency says (set by the
	// invoice service).
	Money        money.Amount
	Reference    string
	IssuedAt     time.Time
	PaidAt       *time.Time
	Issuer       string
	TenantName   string
	TenantEmail  string
	TenantPhone  string
	PackageName  string
	BillingCycle string
	PeriodStart  *time.Time
	PeriodEnd    *time.Time
	// Lines are the invoice lines (one plan line for old payments).
	Lines []InvoiceLine
	// RefundedAmount is the money given back so far.
	RefundedAmount int64
	// Rows are the lines as printed (set by the invoice service).
	Rows []InvoiceRow
	// Refunded is RefundedAmount written in its currency (set by the
	// invoice service; zero when nothing was refunded).
	Refunded money.Amount
}

// InvoiceRow is one printed invoice line.
type InvoiceRow struct {
	Description string
	PeriodStart *time.Time
	PeriodEnd   *time.Time
	Money       money.Amount
}

// TenantUsage is what a tenant holds of its plan's limits.
type TenantUsage struct {
	Users        int64
	Branches     int64
	StorageBytes int64
}

// MapPayment maps a payments row (for services working on a transaction).
func MapPayment(row dbgen.Payment) Payment { return mapPayment(row) }

// StaffSubscription is a subscription as the staff panel lists it: with its
// organization, its plan and its standing
// (docs/guides/billing.md#the-staff-subscriptions-panel).
type StaffSubscription struct {
	Subscription
	TenantName string
	TenantCode string
	// Package is the plan without its features: id, name, price, cycle and
	// yearly price, enough to price it at the subscription's cycle.
	Package  Package
	InGrace  bool
	ReadOnly bool
}

// StaffSubscriptionFilter narrows the staff list. Empty strings and nil
// pointers do not filter.
type StaffSubscriptionFilter struct {
	View          string
	Status        string
	Search        string
	PackageID     *int64
	BillingCycle  string
	ExpiresBefore *time.Time
	// OrderBy is id, expires_at or created_at; OrderDir asc or desc
	// (httpx.OrderParams whitelists them).
	OrderBy  string
	OrderDir string
	// GraceSince is now minus BILLING_GRACE_DAYS: a paid plan that lapsed
	// after it is in grace.
	GraceSince time.Time
	Limit      int32
	Offset     int32
}

// StaffSubscriptionCounts is the number of subscriptions in each view of the
// staff list.
type StaffSubscriptionCounts struct {
	Active    int64
	Trial     int64
	Grace     int64
	ReadOnly  int64
	Pending   int64
	Scheduled int64
	All       int64
}

// Current is one row per organization: the plan in force, in grace or
// read-only.
func (c StaffSubscriptionCounts) Current() int64 { return c.Active + c.Trial + c.Grace + c.ReadOnly }

// Views of the staff subscriptions list.
const (
	ViewCurrent   = "current"
	ViewActive    = "active"
	ViewTrial     = "trial"
	ViewGrace     = "grace"
	ViewReadOnly  = "read_only"
	ViewPending   = "pending"
	ViewScheduled = "scheduled"
	ViewAll       = "all"
)

// StaffViews lists the views in the order the panel shows them.
var StaffViews = []string{ViewCurrent, ViewActive, ViewTrial, ViewGrace, ViewReadOnly, ViewPending, ViewScheduled, ViewAll}

func mapStaffSubscription(row dbgen.ListStaffSubscriptionsRow) StaffSubscription {
	return StaffSubscription{
		Subscription: mapSubscription(dbgen.Subscription{
			ID: row.ID, TenantID: row.TenantID, PackageID: row.PackageID, Status: row.Status,
			StartsAt: row.StartsAt, TrialEndsAt: row.TrialEndsAt, ExpiresAt: row.ExpiresAt,
			CancelledAt: row.CancelledAt, CancellationReason: row.CancellationReason, Overrides: row.Overrides,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, NextPackageID: row.NextPackageID,
			CancelAtPeriodEnd: row.CancelAtPeriodEnd, BillingCycle: row.BillingCycle, NextBillingCycle: row.NextBillingCycle,
		}),
		TenantName: row.TenantName,
		TenantCode: row.TenantCode,
		Package: Package{
			ID: row.PackageID, Name: row.PackageName, Price: row.PackagePrice,
			BillingCycle: row.PackageBillingCycle, YearlyPrice: row.PackageYearlyPrice,
		},
		InGrace:  row.InGrace,
		ReadOnly: row.ReadOnly,
	}
}
