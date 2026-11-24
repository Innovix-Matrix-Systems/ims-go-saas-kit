package dashboard

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// Handler serves the dashboards.
type Handler struct {
	service *Service
	checker *authz.Checker
	httpx.Kit
}

// NewHandler creates the dashboard handler.
func NewHandler(service *Service, checker *authz.Checker, kit httpx.Kit) *Handler {
	return &Handler{service: service, checker: checker, Kit: kit}
}

// window reads ?days= (7, 30 or 90; default 30) and ?tz= (an IANA time zone;
// default UTC).
func (h *Handler) window(r *http.Request) (Window, error) {
	query := r.URL.Query()
	days := DefaultDays
	if raw := strings.TrimSpace(query.Get("days")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || !periods[value] {
			return Window{}, apperr.Validation(map[string][]string{"days": {"The days must be 7, 30 or 90."}})
		}
		days = value
	}
	location := time.UTC
	if raw := strings.TrimSpace(query.Get("tz")); raw != "" {
		loaded, err := time.LoadLocation(raw)
		if err != nil || raw == "Local" {
			return Window{}, apperr.Validation(map[string][]string{"tz": {"The tz must be an IANA time zone, such as Asia/Dhaka."}})
		}
		location = loaded
	}
	return h.service.NewWindow(days, location), nil
}

// may reports whether the caller holds a permission; a failed lookup hides
// the section rather than failing the page.
func (h *Handler) may(ctx context.Context, userID int64, permission string) bool {
	ok, err := h.checker.Can(ctx, userID, permission)
	return err == nil && ok
}

func (h *Handler) tenant(w http.ResponseWriter, r *http.Request) {
	tenantID := tenancy.From(r).EffectiveTenantID()
	if tenantID == 0 {
		h.Error(w, r, apperr.New(apperr.Forbidden, "Choose an organization to see its dashboard; staff use /internal/dashboard."))
		return
	}
	window, err := h.window(r)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	ctx, userID := r.Context(), authn.UserID(r)
	show := TenantSections{
		Members:  h.may(ctx, userID, authz.PermUserViewAll),
		Activity: h.may(ctx, userID, authz.PermActivityView),
		Files:    h.may(ctx, userID, authz.PermFileViewAll),
		Branches: h.may(ctx, userID, authz.PermBranchViewAll),
		Roles:    h.may(ctx, userID, authz.PermRoleViewAll),
	}
	out, err := h.service.Tenant(ctx, tenantID, window, show)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, out)
}

func (h *Handler) platform(w http.ResponseWriter, r *http.Request) {
	window, err := h.window(r)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	ctx, userID := r.Context(), authn.UserID(r)
	show := PlatformSections{
		Organizations: h.may(ctx, userID, authz.PermTenantViewAll),
		Users:         h.may(ctx, userID, authz.PermUserViewAll),
		Activity:      h.may(ctx, userID, authz.PermActivityViewAll),
		Revenue:       h.may(ctx, userID, "payment.view.all"),
		Plans:         h.may(ctx, userID, "package.view.all"),
	}
	out, err := h.service.Platform(ctx, window, show)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, out)
}
