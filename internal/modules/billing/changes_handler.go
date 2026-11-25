package billing

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/service"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

// ChangeResponse is a priced plan change (GET /billing/change-preview, and
// the "change" of POST /billing/subscribe). Amounts are minor units of
// Currency.
type ChangeResponse struct {
	// Kind: new, renew, upgrade or downgrade.
	Kind string `json:"kind"`
	// Effective: now (when paid) or period_end.
	Effective string          `json:"effective"`
	Package   PackageResponse `json:"package"`
	Currency  string          `json:"currency"`
	// Price is the plan's price; Credit the value of the current plan's
	// unused time (UnusedDays of PeriodDays); BalanceApplied the account
	// credit used; AmountDue what is paid now.
	Price          int64 `json:"price"`
	Credit         int64 `json:"credit"`
	UnusedDays     int   `json:"unused_days"`
	PeriodDays     int   `json:"period_days"`
	BalanceApplied int64 `json:"balance_applied"`
	AmountDue      int64 `json:"amount_due"`
	// Surplus is unused-time credit beyond the price: it becomes account
	// credit.
	Surplus int64 `json:"surplus"`
	// DueAtRenewal is what a downgrade costs when the organization renews.
	DueAtRenewal int64 `json:"due_at_renewal"`
	// RequiresPayment is false when nothing is paid now.
	RequiresPayment  bool                  `json:"requires_payment"`
	PeriodStart      time.Time             `json:"period_start"`
	PeriodEnd        time.Time             `json:"period_end"`
	CurrentPeriodEnd *time.Time            `json:"current_period_end"`
	CurrentPackage   *PackageResponse      `json:"current_package"`
	Lines            []InvoiceLineResponse `json:"lines"`
	// OverLimits are the new plan's limits the organization already exceeds.
	OverLimits []UsageItemResponse `json:"over_limits"`
	// OverridesDropped is how many per-organization exceptions the change
	// removes (they belong to the current plan).
	OverridesDropped int `json:"overrides_dropped"`
}

// NewChangeResponse maps a priced change.
func NewChangeResponse(change *service.Change) ChangeResponse {
	quote := change.Quote
	out := ChangeResponse{
		Kind: quote.Kind, Effective: quote.Effective, Package: NewPackageResponse(quote.Target),
		Currency: change.Currency, Price: quote.Price, Credit: quote.Credit,
		UnusedDays: quote.UnusedDays, PeriodDays: quote.PeriodDays,
		BalanceApplied: quote.BalanceApplied, AmountDue: quote.AmountDue, Surplus: quote.Surplus,
		DueAtRenewal: quote.DueAtRenewal, RequiresPayment: quote.RequiresPayment,
		PeriodStart: quote.PeriodStart, PeriodEnd: quote.PeriodEnd,
		Lines: NewInvoiceLines(quote.Lines), OverLimits: []UsageItemResponse{},
	}
	if change.Current != nil {
		out.CurrentPeriodEnd = change.Current.ExpiresAt
		if quote.Kind != domain.ChangeRenew {
			out.OverridesDropped = len(change.Current.Overrides)
		}
	}
	if change.CurrentPackage != nil {
		pkg := NewPackageResponse(*change.CurrentPackage)
		out.CurrentPackage = &pkg
	}
	return out
}

func (h *Handler) changePreview(w http.ResponseWriter, r *http.Request) {
	packageID, err := strconv.ParseInt(r.URL.Query().Get("package_id"), 10, 64)
	if err != nil || packageID <= 0 {
		h.Error(w, r, apperr.Validation(map[string][]string{"package_id": {"The package_id field is required."}}))
		return
	}
	tenantID := tenancy.From(r).EffectiveTenantID()
	change, err := h.subscriptions.Preview(r.Context(), tenantID, packageID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	out := NewChangeResponse(change)
	if h.usage != nil && change.Quote.Kind == domain.ChangeDowngrade {
		pkg, err := h.packages.Get(r.Context(), packageID)
		if err != nil {
			h.Error(w, r, err)
			return
		}
		over, err := h.usage.OverLimits(r.Context(), tenantID, pkg.Features)
		if err != nil {
			h.Error(w, r, err)
			return
		}
		for _, item := range over {
			out.OverLimits = append(out.OverLimits, UsageItemResponse{
				FeatureKey: string(item.Feature), Description: item.Feature.Description(), Limit: item.Limit, Used: item.Used,
			})
		}
	}
	h.Data(w, r, http.StatusOK, out)
}

// cancelAtPeriodEnd: the organization will not renew; the plan runs to its end.
func (h *Handler) cancelAtPeriodEnd(w http.ResponseWriter, r *http.Request) {
	subscription, err := h.subscriptions.CancelAtPeriodEnd(r.Context(), tenancy.From(r).EffectiveTenantID(), authn.UserID(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewSubscriptionResponse(*subscription))
}

// keepPlan undoes a cancellation or a chosen downgrade.
func (h *Handler) keepPlan(w http.ResponseWriter, r *http.Request) {
	subscription, err := h.subscriptions.KeepPlan(r.Context(), tenancy.From(r).EffectiveTenantID(), authn.UserID(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewSubscriptionResponse(*subscription))
}

// CreditResponse is one account credit entry.
type CreditResponse struct {
	ID             int64     `json:"id"`
	Amount         int64     `json:"amount"`
	Currency       string    `json:"currency"`
	Reason         string    `json:"reason"`
	PaymentID      *int64    `json:"payment_id"`
	SubscriptionID *int64    `json:"subscription_id"`
	CreatedAt      time.Time `json:"created_at"`
}

// CreditsResponse is the account credit balance and its entries.
type CreditsResponse struct {
	Balance  int64            `json:"balance"`
	Currency string           `json:"currency"`
	Entries  []CreditResponse `json:"entries"`
	Total    int64            `json:"total"`
}

func newCreditResponse(credit domain.Credit) CreditResponse {
	return CreditResponse{
		ID: credit.ID, Amount: credit.Amount, Currency: credit.Currency, Reason: credit.Reason,
		PaymentID: credit.PaymentID, SubscriptionID: credit.SubscriptionID, CreatedAt: credit.CreatedAt,
	}
}

func (h *Handler) writeCredits(w http.ResponseWriter, r *http.Request, tenantID int64) {
	_, perPage, offset := httpx.PageParams(r)
	summary, err := h.subscriptions.Credits(r.Context(), tenantID, int32(perPage), int32(offset))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	out := CreditsResponse{Balance: summary.Balance, Currency: summary.Currency, Total: summary.Total,
		Entries: make([]CreditResponse, 0, len(summary.Entries))}
	for _, entry := range summary.Entries {
		out.Entries = append(out.Entries, newCreditResponse(entry))
	}
	h.Data(w, r, http.StatusOK, out)
}

func (h *Handler) tenantCredits(w http.ResponseWriter, r *http.Request) {
	h.writeCredits(w, r, tenancy.From(r).EffectiveTenantID())
}

func (h *Handler) internalCredits(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.PathID(w, r, "tenant_id")
	if !ok {
		return
	}
	h.writeCredits(w, r, tenantID)
}

func (h *Handler) adjustCredit(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.PathID(w, r, "tenant_id")
	if !ok {
		return
	}
	var req CreditAdjustRequest
	if !h.Bind(w, r, &req) {
		return
	}
	credit, err := h.subscriptions.AdjustCredit(r.Context(), tenantID, req.Amount, req.Reason, authn.UserID(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, newCreditResponse(*credit))
}

func (h *Handler) refundPayment(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	var req RefundRequest
	if !h.Bind(w, r, &req) {
		return
	}
	payment, err := h.subscriptions.Refund(r.Context(), service.RefundInput{
		PaymentID: id, Amount: req.Amount, Reason: req.Reason, To: req.To,
		EndSubscription: req.EndSubscription, ActorID: authn.UserID(r),
	})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewPaymentResponse(*payment))
}
