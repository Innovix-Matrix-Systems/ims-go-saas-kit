package billing

import (
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
)

// FeatureRequest defines one package feature in create/update payloads.
type FeatureRequest struct {
	FeatureKey string `json:"feature_key" validate:"required"`
	IsEnabled  bool   `json:"is_enabled"`
	LimitValue int64  `json:"limit_value" validate:"gte=-1"`
}

// PackageInsertRequest is the package create/update payload.
type PackageInsertRequest struct {
	Name         string           `json:"name" validate:"required,min=2,max=150"`
	Description  *string          `json:"description" validate:"omitempty,max=500"`
	Price        int64            `json:"price" validate:"gte=0"`
	BillingCycle string           `json:"billing_cycle" validate:"required,oneof=monthly yearly one_time"`
	TrialDays    int32            `json:"trial_days" validate:"gte=0"`
	IsActive     *bool            `json:"is_active"`
	SortOrder    int32            `json:"sort_order"`
	Features     []FeatureRequest `json:"features" validate:"omitempty,dive"`
}

func (r PackageInsertRequest) toInput() domain.PackageInput {
	isActive := true
	if r.IsActive != nil {
		isActive = *r.IsActive
	}
	return domain.PackageInput{
		Name:         r.Name,
		Description:  r.Description,
		Price:        r.Price,
		BillingCycle: r.BillingCycle,
		TrialDays:    r.TrialDays,
		IsActive:     isActive,
		SortOrder:    r.SortOrder,
	}
}

func (r PackageInsertRequest) toFeatures() []domain.FeatureInput {
	if r.Features == nil {
		return nil
	}
	features := make([]domain.FeatureInput, 0, len(r.Features))
	for _, feature := range r.Features {
		features = append(features, domain.FeatureInput{
			FeatureKey: feature.FeatureKey,
			IsEnabled:  feature.IsEnabled,
			LimitValue: feature.LimitValue,
		})
	}
	return features
}

// SubscribeRequest starts a subscription.
type SubscribeRequest struct {
	PackageID     int64          `json:"package_id" validate:"required,gt=0"`
	PaymentMethod string         `json:"payment_method" validate:"omitempty,oneof=bkash nagad cash bank_transfer"`
	PaymentData   map[string]any `json:"payment_data"`
}

// CancelRequest cancels a subscription.
type CancelRequest struct {
	Reason *string `json:"reason" validate:"omitempty,max=500"`
}

// OverridesRequest sets per-tenant feature overrides.
type OverridesRequest struct {
	Overrides map[string]any `json:"overrides" validate:"required"`
}

// AssignPlanRequest moves a tenant onto a package from the admin side.
// Without payment_method the plan is assigned free of charge; amount
// defaults to the package price.
type AssignPlanRequest struct {
	PackageID     int64  `json:"package_id" validate:"required,gt=0"`
	PaymentMethod string `json:"payment_method" validate:"omitempty,oneof=cash bank_transfer"`
	Amount        *int64 `json:"amount" validate:"omitempty,gt=0"`
}

// RecordPaymentRequest records a manual payment.
type RecordPaymentRequest struct {
	TenantID       int64   `json:"tenant_id" validate:"required,gt=0"`
	SubscriptionID int64   `json:"subscription_id" validate:"required,gt=0"`
	PaymentMethod  string  `json:"payment_method" validate:"required,oneof=cash bank_transfer"`
	Amount         int64   `json:"amount" validate:"gt=0"`
	Notes          *string `json:"notes" validate:"omitempty,max=500"`
}

// ReviewRequest approves or rejects an approval request.
type ReviewRequest struct {
	Note *string `json:"note" validate:"omitempty,max=500"`
}

// FeatureResponse is the public feature representation.
type FeatureResponse struct {
	FeatureKey string `json:"feature_key"`
	IsEnabled  bool   `json:"is_enabled"`
	LimitValue int64  `json:"limit_value"`
}

// PackageResponse is the public package representation.
type PackageResponse struct {
	ID           int64             `json:"id"`
	Name         string            `json:"name"`
	Description  *string           `json:"description"`
	Price        int64             `json:"price"`
	BillingCycle string            `json:"billing_cycle"`
	TrialDays    int32             `json:"trial_days"`
	IsActive     bool              `json:"is_active"`
	SortOrder    int32             `json:"sort_order"`
	Features     []FeatureResponse `json:"features"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// NewPackageResponse maps a package to its public representation.
func NewPackageResponse(pkg domain.Package) PackageResponse {
	features := make([]FeatureResponse, 0, len(pkg.Features))
	for _, feature := range pkg.Features {
		features = append(features, FeatureResponse{
			FeatureKey: feature.FeatureKey,
			IsEnabled:  feature.IsEnabled,
			LimitValue: feature.LimitValue,
		})
	}
	return PackageResponse{
		ID:           pkg.ID,
		Name:         pkg.Name,
		Description:  pkg.Description,
		Price:        pkg.Price,
		BillingCycle: pkg.BillingCycle,
		TrialDays:    pkg.TrialDays,
		IsActive:     pkg.IsActive,
		SortOrder:    pkg.SortOrder,
		Features:     features,
		CreatedAt:    pkg.CreatedAt,
		UpdatedAt:    pkg.UpdatedAt,
	}
}

// SubscriptionResponse is the public subscription representation.
type SubscriptionResponse struct {
	ID                 int64          `json:"id"`
	TenantID           int64          `json:"tenant_id"`
	PackageID          int64          `json:"package_id"`
	Status             string         `json:"status"`
	StartsAt           *time.Time     `json:"starts_at"`
	TrialEndsAt        *time.Time     `json:"trial_ends_at"`
	ExpiresAt          *time.Time     `json:"expires_at"`
	CancelledAt        *time.Time     `json:"cancelled_at"`
	CancellationReason *string        `json:"cancellation_reason"`
	Overrides          map[string]any `json:"overrides"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

// NewSubscriptionResponse maps a subscription to its public representation.
func NewSubscriptionResponse(subscription domain.Subscription) SubscriptionResponse {
	return SubscriptionResponse{
		ID:                 subscription.ID,
		TenantID:           subscription.TenantID,
		PackageID:          subscription.PackageID,
		Status:             subscription.Status,
		StartsAt:           subscription.StartsAt,
		TrialEndsAt:        subscription.TrialEndsAt,
		ExpiresAt:          subscription.ExpiresAt,
		CancelledAt:        subscription.CancelledAt,
		CancellationReason: subscription.CancellationReason,
		Overrides:          subscription.Overrides,
		CreatedAt:          subscription.CreatedAt,
		UpdatedAt:          subscription.UpdatedAt,
	}
}

// StatusResponse is the tenant subscription status payload.
type StatusResponse struct {
	Subscription *SubscriptionResponse `json:"subscription"`
	Package      *PackageResponse      `json:"package"`
	Active       bool                  `json:"active"`
}

// PaymentResponse is the public payment representation.
type PaymentResponse struct {
	ID              int64          `json:"id"`
	TenantID        int64          `json:"tenant_id"`
	SubscriptionID  *int64         `json:"subscription_id"`
	PaymentMethod   string         `json:"payment_method"`
	Status          string         `json:"status"`
	Amount          int64          `json:"amount"`
	Currency        string         `json:"currency"`
	GatewayRef      *string        `json:"gateway_ref"`
	GatewayStatus   *string        `json:"gateway_status"`
	GatewayResponse map[string]any `json:"gateway_response,omitempty"`
	InvoiceNumber   string         `json:"invoice_number"`
	PaidAt          *time.Time     `json:"paid_at"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// NewPaymentResponse maps a payment to its public representation.
func NewPaymentResponse(payment domain.Payment) PaymentResponse {
	return PaymentResponse{
		ID:              payment.ID,
		TenantID:        payment.TenantID,
		SubscriptionID:  payment.SubscriptionID,
		PaymentMethod:   payment.PaymentMethod,
		Status:          payment.Status,
		Amount:          payment.Amount,
		Currency:        payment.Currency,
		GatewayRef:      payment.GatewayRef,
		GatewayStatus:   payment.GatewayStatus,
		GatewayResponse: payment.GatewayResponse,
		InvoiceNumber:   payment.InvoiceNumber,
		PaidAt:          payment.PaidAt,
		CreatedAt:       payment.CreatedAt,
		UpdatedAt:       payment.UpdatedAt,
	}
}

// ApprovalResponse is the public approval representation.
type ApprovalResponse struct {
	ID             int64          `json:"id"`
	TenantID       int64          `json:"tenant_id"`
	SubscriptionID int64          `json:"subscription_id"`
	PaymentID      int64          `json:"payment_id"`
	PaymentMethod  string         `json:"payment_method"`
	PaymentData    map[string]any `json:"payment_data"`
	Status         string         `json:"status"`
	ReviewerID     *int64         `json:"reviewer_id"`
	ReviewerNote   *string        `json:"reviewer_note"`
	ReviewedAt     *time.Time     `json:"reviewed_at"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	// Details is filled in lists: who pays, how much, for which plan.
	Details *ApprovalDetailResponse `json:"details,omitempty"`
}

// ApprovalDetailResponse is the payer, plan, amount and invoice of an approval.
type ApprovalDetailResponse struct {
	TenantName    string `json:"tenant_name"`
	PackageName   string `json:"package_name"`
	Amount        int64  `json:"amount"`
	Currency      string `json:"currency"`
	InvoiceNumber string `json:"invoice_number"`
}

// NewApprovalResponse maps an approval to its public representation.
func NewApprovalResponse(approval domain.Approval) ApprovalResponse {
	return ApprovalResponse{
		ID:             approval.ID,
		TenantID:       approval.TenantID,
		SubscriptionID: approval.SubscriptionID,
		PaymentID:      approval.PaymentID,
		PaymentMethod:  approval.PaymentMethod,
		PaymentData:    approval.PaymentData,
		Status:         approval.Status,
		ReviewerID:     approval.ReviewerID,
		ReviewerNote:   approval.ReviewerNote,
		ReviewedAt:     approval.ReviewedAt,
		CreatedAt:      approval.CreatedAt,
		UpdatedAt:      approval.UpdatedAt,
	}
}
