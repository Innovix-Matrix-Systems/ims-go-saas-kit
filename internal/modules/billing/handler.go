package billing

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/billing/service"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// Handler serves billing HTTP endpoints.
type Handler struct {
	packages      *service.PackageService
	subscriptions *service.SubscriptionService
	payments      *service.PaymentService
	approvals     *service.ApprovalService
	checker       *authz.Checker
	httpx.Kit
}

// NewHandler creates the billing handler.
func NewHandler(
	packages *service.PackageService,
	subscriptions *service.SubscriptionService,
	payments *service.PaymentService,
	approvals *service.ApprovalService,
	checker *authz.Checker,
	kit httpx.Kit,
) *Handler {
	return &Handler{
		packages:      packages,
		subscriptions: subscriptions,
		payments:      payments,
		approvals:     approvals,
		checker:       checker,
		Kit:           kit,
	}
}

func (h *Handler) publicPackages(w http.ResponseWriter, r *http.Request) {
	items, err := h.packages.ListPublic(r.Context())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]PackageResponse, 0, len(items))
	for _, item := range items {
		data = append(data, NewPackageResponse(item))
	}
	h.Data(w, r, http.StatusOK, data)
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	scope := tenancy.From(r)
	status, err := h.subscriptions.Status(r.Context(), scope.EffectiveTenantID())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	response := StatusResponse{Active: status.Active}
	if status.Subscription != nil {
		subscription := NewSubscriptionResponse(*status.Subscription)
		response.Subscription = &subscription
	}
	if status.Package != nil {
		pkg := NewPackageResponse(*status.Package)
		response.Package = &pkg
	}
	h.Data(w, r, http.StatusOK, response)
}

func (h *Handler) listTenantPayments(w http.ResponseWriter, r *http.Request) {
	scope := tenancy.From(r)
	page, perPage, offset := httpx.PageParams(r)
	items, total, err := h.payments.ListForTenant(r.Context(), scope.EffectiveTenantID(), int32(perPage), int32(offset))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]PaymentResponse, 0, len(items))
	for _, item := range items {
		data = append(data, NewPaymentResponse(item))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) subscribe(w http.ResponseWriter, r *http.Request) {
	var req SubscribeRequest
	if !h.Bind(w, r, &req) {
		return
	}
	scope := tenancy.From(r)
	subscription, payment, err := h.subscriptions.Subscribe(r.Context(), scope.EffectiveTenantID(), req.PackageID, req.PaymentMethod, req.PaymentData, authn.UserID(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, map[string]any{
		"subscription": NewSubscriptionResponse(*subscription),
		"payment":      NewPaymentResponse(*payment),
	})
}

func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	gateway := chi.URLParam(r, "gateway")
	payload := map[string]any{}
	for key, values := range r.URL.Query() {
		if len(values) > 0 {
			payload[key] = values[0]
		}
	}

	if domain.IsManualMethod(gateway) {
		h.Error(w, r, apperr.New(apperr.InvalidInput, "Manual payment methods do not accept callbacks."))
		return
	}
	gatewayRef := queryValue(payload, "paymentID")
	if gatewayRef == "" {
		h.Error(w, r, apperr.New(apperr.ValidationFailed, "The callback is missing its payment reference."))
		return
	}

	payment, err := h.payments.FindByGatewayRef(r.Context(), gatewayRef)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	if _, err := h.subscriptions.HandleGatewayCallback(r.Context(), payment.ID, gateway, payload); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Message(w, r, http.StatusOK, "Payment callback processed.")
}

func (h *Handler) listPackages(w http.ResponseWriter, r *http.Request) {
	items, err := h.packages.List(r.Context())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]PackageResponse, 0, len(items))
	for _, item := range items {
		data = append(data, NewPackageResponse(item))
	}
	h.Data(w, r, http.StatusOK, data)
}

func (h *Handler) showPackage(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	pkg, err := h.packages.Get(r.Context(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewPackageResponse(*pkg))
}

func (h *Handler) storePackage(w http.ResponseWriter, r *http.Request) {
	var req PackageInsertRequest
	if !h.Bind(w, r, &req) {
		return
	}
	pkg, err := h.packages.Create(r.Context(), req.toInput(), req.toFeatures())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, NewPackageResponse(*pkg))
}

func (h *Handler) updatePackage(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	var req PackageInsertRequest
	if !h.Bind(w, r, &req) {
		return
	}
	pkg, err := h.packages.Update(r.Context(), id, req.toInput(), req.toFeatures())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewPackageResponse(*pkg))
}

func (h *Handler) destroyPackage(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.packages.Delete(r.Context(), id); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

func (h *Handler) listSubscriptions(w http.ResponseWriter, r *http.Request) {
	page, perPage, offset := httpx.PageParams(r)
	items, total, err := h.subscriptions.List(r.Context(), r.URL.Query().Get("status"), int32(perPage), int32(offset))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]SubscriptionResponse, 0, len(items))
	for _, item := range items {
		data = append(data, NewSubscriptionResponse(item))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) cancelSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	var req CancelRequest
	if !h.Bind(w, r, &req) {
		return
	}
	subscription, err := h.subscriptions.Cancel(r.Context(), id, req.Reason)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewSubscriptionResponse(*subscription))
}

func (h *Handler) overrides(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	var req OverridesRequest
	if !h.Bind(w, r, &req) {
		return
	}
	subscription, err := h.subscriptions.ApplyOverrides(r.Context(), id, req.Overrides)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewSubscriptionResponse(*subscription))
}

// assignPlan puts a tenant on a package (support, sales deals, comps) and
// records the manual payment when one is given.
func (h *Handler) assignPlan(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.PathID(w, r, "tenant_id")
	if !ok {
		return
	}
	var req AssignPlanRequest
	if !h.Bind(w, r, &req) {
		return
	}
	subscription, payment, err := h.subscriptions.Assign(r.Context(), service.AssignInput{
		TenantID:      tenantID,
		PackageID:     req.PackageID,
		ActorID:       authn.UserID(r),
		PaymentMethod: req.PaymentMethod,
		Amount:        req.Amount,
	})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	var paymentResponse *PaymentResponse
	if payment != nil {
		value := NewPaymentResponse(*payment)
		paymentResponse = &value
	}
	h.Data(w, r, http.StatusCreated, map[string]any{
		"subscription": NewSubscriptionResponse(*subscription),
		"payment":      paymentResponse,
	})
}

func (h *Handler) listAllPayments(w http.ResponseWriter, r *http.Request) {
	page, perPage, offset := httpx.PageParams(r)
	items, total, err := h.payments.List(r.Context(), r.URL.Query().Get("status"), int32(perPage), int32(offset))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]PaymentResponse, 0, len(items))
	for _, item := range items {
		data = append(data, NewPaymentResponse(item))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) recordPayment(w http.ResponseWriter, r *http.Request) {
	var req RecordPaymentRequest
	if !h.Bind(w, r, &req) {
		return
	}
	notes := ""
	if req.Notes != nil {
		notes = *req.Notes
	}
	if _, err := h.subscriptions.GetForTenant(r.Context(), req.TenantID, req.SubscriptionID); err != nil {
		h.Error(w, r, err)
		return
	}
	payment, err := h.payments.RecordManual(r.Context(), req.TenantID, req.SubscriptionID, req.PaymentMethod, req.Amount, notes, authn.UserID(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	if _, err := h.subscriptions.Activate(r.Context(), req.SubscriptionID); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, NewPaymentResponse(*payment))
}

func (h *Handler) listApprovals(w http.ResponseWriter, r *http.Request) {
	page, perPage, offset := httpx.PageParams(r)
	items, total, err := h.approvals.List(r.Context(), r.URL.Query().Get("status"), int32(perPage), int32(offset))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]ApprovalResponse, 0, len(items))
	for _, item := range items {
		data = append(data, NewApprovalResponse(item))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) approve(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	var req ReviewRequest
	if !h.Bind(w, r, &req) {
		return
	}
	approval, err := h.approvals.Approve(r.Context(), id, authn.UserID(r), req.Note)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewApprovalResponse(*approval))
}

func (h *Handler) reject(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	var req ReviewRequest
	if !h.Bind(w, r, &req) {
		return
	}
	approval, err := h.approvals.Reject(r.Context(), id, authn.UserID(r), req.Note)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewApprovalResponse(*approval))
}

func queryValue(payload map[string]any, key string) string {
	if value, ok := payload[key].(string); ok {
		return value
	}
	return ""
}

// can is route middleware requiring a permission (Laravel's can:...).
func (h *Handler) can(permission string) func(http.Handler) http.Handler {
	return authz.Require(h.checker, permission, h.Responder)
}
