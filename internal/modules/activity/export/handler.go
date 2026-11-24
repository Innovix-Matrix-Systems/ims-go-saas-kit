package activityexport

import (
	"net/http"
	"strings"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/dataprocessing"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// Handler serves the export routes.
type Handler struct {
	exporter *Exporter
	checker  *authz.Checker
	httpx.Kit
}

// NewHandler creates the export handler.
func NewHandler(exporter *Exporter, checker *authz.Checker, kit httpx.Kit) *Handler {
	return &Handler{exporter: exporter, checker: checker, Kit: kit}
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	var req Request
	if !h.Bind(w, r, &req) {
		return
	}
	identity, _ := authn.IdentityFromContext(r.Context())
	system := false
	if identity != nil && !identity.Impersonated() {
		can, err := h.checker.Can(r.Context(), identity.UserID, authz.PermActivityViewAll)
		if err != nil {
			h.Error(w, r, err)
			return
		}
		system = can
	}
	filters := Filters{System: system, SubjectType: req.SubjectType, ActorID: req.ActorID, Action: req.Action, Search: strings.TrimSpace(req.Search)}
	if system {
		filters.Tenant = req.TenantID
	}
	if req.From != nil {
		filters.From = *req.From
	}
	if req.To != nil {
		filters.To = *req.To
	}
	job, err := h.exporter.Start(r.Context(), tenancy.From(r), authn.UserID(r), filters)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusAccepted, dataprocessing.NewResponse(*job))
}
