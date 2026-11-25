package domain

import (
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
)

// Package is the domain representation of a billing plan.
type Package struct {
	ID           int64
	Name         string
	Description  *string
	Price        int64
	BillingCycle string
	TrialDays    int32
	IsActive     bool
	SortOrder    int32
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Features     []PackageFeature
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
	CreatedAt          time.Time
	UpdatedAt          time.Time
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
		ID:              row.ID,
		TenantID:        row.TenantID,
		SubscriptionID:  row.SubscriptionID,
		PaymentMethod:   row.PaymentMethod,
		Status:          row.Status,
		Amount:          row.Amount,
		Currency:        row.Currency,
		GatewayRef:      row.GatewayRef,
		GatewayStatus:   row.GatewayStatus,
		GatewayResponse: response,
		InvoiceNumber:   row.InvoiceNumber,
		PaidAt:          timePtr(row.PaidAt),
		RecordedBy:      row.RecordedBy,
		CreatedBy:       row.CreatedBy,
		UpdatedBy:       row.UpdatedBy,
		CreatedAt:       row.CreatedAt.Time,
		UpdatedAt:       row.UpdatedAt.Time,
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
	Reference     string
	IssuedAt      time.Time
	PaidAt        *time.Time
	Issuer        string
	TenantName    string
	TenantEmail   string
	TenantPhone   string
	PackageName   string
	BillingCycle  string
	PeriodStart   *time.Time
	PeriodEnd     *time.Time
}

// TenantUsage is what a tenant holds of its plan's limits.
type TenantUsage struct {
	Users        int64
	Branches     int64
	StorageBytes int64
}
