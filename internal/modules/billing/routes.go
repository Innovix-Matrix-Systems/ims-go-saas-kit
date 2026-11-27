package billing

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
)

// RegisterPublicRoutes mounts unauthenticated package and callback routes.
func (h *Handler) RegisterPublicRoutes(r chi.Router) {
	r.Get("/public/packages", h.publicPackages)
	r.Get("/billing/{gateway}/callback", h.callback)
}

// RegisterRoutesResolve mounts tenant-scoped billing routes: the caller's own
// organization. They use tenant permissions (billing.*): the system ones
// (subscription.*, payment.*) can never be held by a tenant role.
func (h *Handler) RegisterRoutesResolve(r chi.Router) {
	r.Get("/billing/subscription/status", h.status)
	r.With(h.can(authz.PermBillingView)).Get("/billing/payments", h.listTenantPayments)
	r.With(h.can(authz.PermBillingView)).Get("/billing/usage", h.tenantUsage)
	r.With(h.can(authz.PermBillingManage)).Post("/billing/subscribe", h.subscribe)
	r.With(h.can(authz.PermBillingManage)).Get("/billing/change-preview", h.changePreview)
	r.With(h.can(authz.PermBillingManage)).Post("/billing/subscription/cancel", h.cancelAtPeriodEnd)
	r.With(h.can(authz.PermBillingManage)).Post("/billing/subscription/keep", h.keepPlan)
	r.With(h.can(authz.PermBillingView)).Get("/billing/credits", h.tenantCredits)
	r.With(h.can(authz.PermBillingManage)).Post("/billing/payments/{id}/cancel", h.cancelPayment)
	r.With(h.can(authz.PermBillingView)).Get("/billing/payments/{id}/invoice", h.invoice)
	r.With(h.can(authz.PermBillingView)).Get("/billing/payments/{id}/invoice.pdf", h.invoicePDF)
}

// RegisterRoutesExempt mounts system billing management routes.
func (h *Handler) RegisterRoutesExempt(r chi.Router) {
	r.With(h.can("package.view.all")).Get("/packages", h.listPackages)
	r.With(h.can("package.view.all")).Get("/packages/features", h.features)
	r.With(h.can("package.create")).Post("/packages", h.storePackage)
	r.With(h.can("package.view")).Get("/packages/{id}", h.showPackage)
	r.With(h.can("package.update")).Patch("/packages/{id}", h.updatePackage)
	r.With(h.can("package.delete")).Delete("/packages/{id}", h.destroyPackage)

	r.With(h.can("subscription.view.all")).Get("/billing/internal/subscriptions", h.listSubscriptions)
	r.With(h.can("subscription.view.all")).Get("/billing/internal/subscriptions/summary", h.subscriptionSummary)
	r.With(h.can("subscription.manage")).Post("/billing/internal/subscriptions/{id}/cancel", h.cancelSubscription)
	r.With(h.can("subscription.override")).Post("/billing/internal/subscriptions/{id}/overrides", h.overrides)
	r.With(h.can("subscription.manage")).Post("/billing/internal/tenants/{tenant_id}/subscription", h.assignPlan)
	r.With(h.can("payment.view.all")).Get("/billing/internal/payments", h.listAllPayments)
	r.With(h.can("payment.record")).Post("/billing/internal/payments", h.recordPayment)
	r.With(h.can("payment.refund")).Post("/billing/internal/payments/{id}/refund", h.refundPayment)
	r.With(h.can("payment.view.all")).Get("/billing/internal/tenants/{tenant_id}/credits", h.internalCredits)
	r.With(h.can("payment.refund")).Post("/billing/internal/tenants/{tenant_id}/credits", h.adjustCredit)
	r.With(h.can("payment_approval.view.all")).Get("/billing/internal/payment-approvals", h.listApprovals)
	r.With(h.can("payment_approval.review")).Post("/billing/internal/payment-approvals/{id}/approve", h.approve)
	r.With(h.can("payment_approval.review")).Post("/billing/internal/payment-approvals/{id}/reject", h.reject)
}
