package billing

import (
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/service"
)

// FeatureRequest defines one package feature in create/update payloads.
type FeatureRequest struct {
	FeatureKey string `json:"feature_key" validate:"required"`
	IsEnabled  bool   `json:"is_enabled"`
	LimitValue int64  `json:"limit_value" validate:"gte=-1"`
}

// PackageInsertRequest is the package create/update payload.
type PackageInsertRequest struct {
	Name         string  `json:"name" validate:"required,min=2,max=150"`
	Description  *string `json:"description" validate:"omitempty,max=500"`
	Price        int64   `json:"price" validate:"gte=0"`
	BillingCycle string  `json:"billing_cycle" validate:"required,oneof=monthly yearly one_time"`
	// YearlyPrice offers a monthly plan yearly too (null: monthly only).
	YearlyPrice *int64           `json:"yearly_price" validate:"omitempty,gte=0"`
	TrialDays   int32            `json:"trial_days" validate:"gte=0"`
	IsActive    *bool            `json:"is_active"`
	SortOrder   int32            `json:"sort_order"`
	Features    []FeatureRequest `json:"features" validate:"omitempty,dive"`
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
		YearlyPrice:  r.YearlyPrice,
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
	PackageID int64 `json:"package_id" validate:"required,gt=0"`
	// BillingCycle is the cycle to buy (default: the plan's own).
	BillingCycle  string         `json:"billing_cycle" validate:"omitempty,oneof=monthly yearly one_time"`
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
	Amount        *int64 `json:"amount" validate:"omitempty,gte=0"`
	// Mode: replace (default) ends the current plan now with no credit;
	// change applies the customer's rules (credit, end-of-period changes).
	Mode string `json:"mode" validate:"omitempty,oneof=replace change"`
	// BillingCycle is the cycle to assign (default: the plan's own).
	BillingCycle string `json:"billing_cycle" validate:"omitempty,oneof=monthly yearly one_time"`
}

// RefundRequest gives money back for a payment (staff).
type RefundRequest struct {
	Amount          int64  `json:"amount" validate:"required,gt=0"`
	Reason          string `json:"reason" validate:"required,min=3,max=500"`
	To              string `json:"to" validate:"required,oneof=credit original"`
	EndSubscription bool   `json:"end_subscription"`
}

// CreditAdjustRequest adds (or takes back, negative) account credit (staff).
type CreditAdjustRequest struct {
	Amount int64  `json:"amount" validate:"required,ne=0"`
	Reason string `json:"reason" validate:"required,min=3,max=500"`
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
	// ReceivedAmount is what staff actually received (approve only): more
	// than the invoice becomes account credit; less is refused.
	ReceivedAmount *int64 `json:"received_amount" validate:"omitempty,gte=0"`
}

// FeatureResponse is the public feature representation.
type FeatureResponse struct {
	FeatureKey string `json:"feature_key"`
	IsEnabled  bool   `json:"is_enabled"`
	LimitValue int64  `json:"limit_value"`
}

// PackageResponse is the public package representation.
type PackageResponse struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	Description  *string `json:"description"`
	Price        int64   `json:"price"`
	BillingCycle string  `json:"billing_cycle"`
	// YearlyPrice: the plan can also be bought yearly at this price.
	YearlyPrice *int64            `json:"yearly_price"`
	TrialDays   int32             `json:"trial_days"`
	IsActive    bool              `json:"is_active"`
	SortOrder   int32             `json:"sort_order"`
	Features    []FeatureResponse `json:"features"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
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
		YearlyPrice:  pkg.YearlyPrice,
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
	NextPackageID      *int64         `json:"next_package_id"`
	// BillingCycle is the cycle chosen (null: the plan's own);
	// NextBillingCycle goes with NextPackageID.
	BillingCycle      *string   `json:"billing_cycle"`
	NextBillingCycle  *string   `json:"next_billing_cycle"`
	CancelAtPeriodEnd bool      `json:"cancel_at_period_end"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
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
		NextPackageID:      subscription.NextPackageID,
		BillingCycle:       subscription.BillingCycle,
		NextBillingCycle:   subscription.NextBillingCycle,
		CancelAtPeriodEnd:  subscription.CancelAtPeriodEnd,
		CreatedAt:          subscription.CreatedAt,
		UpdatedAt:          subscription.UpdatedAt,
	}
}

// StatusResponse is the tenant subscription status payload.
type StatusResponse struct {
	Subscription *SubscriptionResponse `json:"subscription"`
	Package      *PackageResponse      `json:"package"`
	Active       bool                  `json:"active"`
	// Scheduled is the period arranged to follow the current one.
	Scheduled        *SubscriptionResponse `json:"scheduled"`
	ScheduledPackage *PackageResponse      `json:"scheduled_package"`
	// NextPackage is the plan chosen for the renewal (a downgrade).
	NextPackage *PackageResponse `json:"next_package"`
	// CreditBalance is the account credit in Currency (minor units).
	CreditBalance int64  `json:"credit_balance"`
	Currency      string `json:"currency"`
	// BillingCycle and Price are what the plan costs at the subscription's
	// cycle (a monthly plan bought yearly shows the yearly price).
	BillingCycle string `json:"billing_cycle"`
	Price        int64  `json:"price"`
	// InGrace: the paid plan lapsed and still applies until GraceEndsAt.
	// ReadOnly: it lapsed beyond the grace period; writes are refused until
	// a plan is paid.
	InGrace     bool       `json:"in_grace"`
	GraceEndsAt *time.Time `json:"grace_ends_at"`
	ReadOnly    bool       `json:"read_only"`
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
	// Kind: new, renew, upgrade, downgrade or staff.
	Kind                   string                `json:"kind"`
	Lines                  []InvoiceLineResponse `json:"lines"`
	Gross                  int64                 `json:"gross"`
	BalanceApplied         int64                 `json:"balance_applied"`
	RefundedAmount         int64                 `json:"refunded_amount"`
	ReplacesSubscriptionID *int64                `json:"replaces_subscription_id"`
}

// InvoiceLineResponse is one invoice line: the plan bought (plan), the credit
// for the unused time of the plan replaced (unused_time) or account credit
// used (account_credit). Amounts are minor units; credits are negative.
type InvoiceLineResponse struct {
	Type         string     `json:"type"`
	PackageID    int64      `json:"package_id,omitempty"`
	PackageName  string     `json:"package_name,omitempty"`
	BillingCycle string     `json:"billing_cycle,omitempty"`
	PeriodStart  *time.Time `json:"period_start,omitempty"`
	PeriodEnd    *time.Time `json:"period_end,omitempty"`
	Days         int        `json:"days,omitempty"`
	Amount       int64      `json:"amount"`
}

// NewInvoiceLines maps invoice lines (never nil).
func NewInvoiceLines(lines []domain.InvoiceLine) []InvoiceLineResponse {
	out := make([]InvoiceLineResponse, 0, len(lines))
	for _, line := range lines {
		out = append(out, InvoiceLineResponse(line))
	}
	return out
}

// NewPaymentResponse maps a payment to its public representation.
func NewPaymentResponse(payment domain.Payment) PaymentResponse {
	return PaymentResponse{
		ID:                     payment.ID,
		TenantID:               payment.TenantID,
		SubscriptionID:         payment.SubscriptionID,
		PaymentMethod:          payment.PaymentMethod,
		Status:                 payment.Status,
		Amount:                 payment.Amount,
		Currency:               payment.Currency,
		GatewayRef:             payment.GatewayRef,
		GatewayStatus:          payment.GatewayStatus,
		GatewayResponse:        payment.GatewayResponse,
		InvoiceNumber:          payment.InvoiceNumber,
		PaidAt:                 payment.PaidAt,
		CreatedAt:              payment.CreatedAt,
		UpdatedAt:              payment.UpdatedAt,
		Kind:                   payment.Kind,
		Lines:                  NewInvoiceLines(payment.Lines),
		Gross:                  payment.Gross,
		BalanceApplied:         payment.BalanceApplied,
		RefundedAmount:         payment.RefundedAmount,
		ReplacesSubscriptionID: payment.ReplacesSubscriptionID,
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
	TenantName    string                `json:"tenant_name"`
	PackageName   string                `json:"package_name"`
	Amount        int64                 `json:"amount"`
	Currency      string                `json:"currency"`
	InvoiceNumber string                `json:"invoice_number"`
	Kind          string                `json:"kind"`
	Lines         []InvoiceLineResponse `json:"lines"`
	// QuoteStale: the plan credited at checkout is no longer the one in
	// force, so the amount may be wrong.
	QuoteStale bool `json:"quote_stale"`
	// OverridesDropped is how many per-tenant overrides approving removes.
	OverridesDropped int `json:"overrides_dropped"`
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

// StaffSubscriptionResponse is one row of the staff subscriptions panel: the
// subscription with its organization, its plan, what its period costs and
// its standing.
type StaffSubscriptionResponse struct {
	SubscriptionResponse
	Organization OrganizationRef `json:"organization"`
	Package      PackageRef      `json:"package"`
	// Price is what one period costs at PriceCycle (the subscription's cycle).
	Price      int64  `json:"price"`
	PriceCycle string `json:"price_cycle"`
	// InGrace: the paid plan lapsed and still applies until GraceEndsAt.
	// ReadOnly: the organization is read-only (this is its latest plan).
	InGrace     bool       `json:"in_grace"`
	GraceEndsAt *time.Time `json:"grace_ends_at"`
	ReadOnly    bool       `json:"read_only"`
}

// OrganizationRef names an organization in a list.
type OrganizationRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"`
}

// PackageRef names a plan in a list.
type PackageRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// NewStaffSubscriptionResponse maps a staff panel row.
func NewStaffSubscriptionResponse(row service.StaffSubscriptionRow) StaffSubscriptionResponse {
	return StaffSubscriptionResponse{
		SubscriptionResponse: NewSubscriptionResponse(row.Subscription),
		Organization:         OrganizationRef{ID: row.TenantID, Name: row.TenantName, Code: row.TenantCode},
		Package:              PackageRef{ID: row.Package.ID, Name: row.Package.Name},
		Price:                row.Offered.Price,
		PriceCycle:           row.Offered.BillingCycle,
		InGrace:              row.InGrace,
		GraceEndsAt:          row.GraceEndsAt,
		ReadOnly:             row.ReadOnly,
	}
}

// StaffSubscriptionCountsResponse is the number of subscriptions in each view
// of the staff panel (its tabs).
type StaffSubscriptionCountsResponse struct {
	Current   int64 `json:"current"`
	Active    int64 `json:"active"`
	Trial     int64 `json:"trial"`
	Grace     int64 `json:"grace"`
	ReadOnly  int64 `json:"read_only"`
	Pending   int64 `json:"pending"`
	Scheduled int64 `json:"scheduled"`
	All       int64 `json:"all"`
}
