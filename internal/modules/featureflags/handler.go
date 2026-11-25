package featureflags

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

// Handler serves the feature-flag REST surface.
type Handler struct {
	service *Service
	checker *authz.Checker
	httpx.Kit
}

// NewHandler creates the feature-flag handler.
func NewHandler(service *Service, checker *authz.Checker, kit httpx.Kit) *Handler {
	return &Handler{service: service, checker: checker, Kit: kit}
}

// index returns the caller's effective flags.
func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	scope := tenancy.From(r)
	values, err := h.service.Effective(r.Context(), scope, identity.UserID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, values)
}

// internalIndex returns every catalog flag's effective value for a target.
func (h *Handler) internalIndex(w http.ResponseWriter, r *http.Request) {
	target, err := targetFromQuery(r)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	items, err := h.service.EffectiveForTarget(r.Context(), target)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, toFlagItems(items))
}

// internalUpdate applies a batch of flag values to the target named in the body.
func (h *Handler) internalUpdate(w http.ResponseWriter, r *http.Request) {
	var req UpdateFlagsRequest
	if !h.Bind(w, r, &req) {
		return
	}
	items, err := h.service.SetBatch(r.Context(), authn.UserID(r), targetFromRequest(req.TargetType, req.TargetID), req.Flags, req.Rollout)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, toFlagItems(items))
}

// internalDelete clears a single override at the target named in the query.
func (h *Handler) internalDelete(w http.ResponseWriter, r *http.Request) {
	target, err := targetFromQuery(r)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	if err := h.service.Delete(r.Context(), target, chi.URLParam(r, "key")); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

func targetFromRequest(targetType string, targetID *int64) Target {
	target := Target{Type: targetType}
	if targetType == "" || targetType == "global" {
		target.Type = TargetGlobal
	}
	// Preserve a provided id even for the global target so the service rejects
	// `global` + `target_id` (the value must not be silently ignored).
	if targetID != nil {
		target.ID = *targetID
	}
	return target
}

// targetFromQuery parses the feature-flag target query parameters (global
// default).
func targetFromQuery(r *http.Request) (Target, error) {
	rawType := strings.TrimSpace(r.URL.Query().Get("target_type"))
	targetType := TargetGlobal
	switch rawType {
	case "", "global":
	case string(TargetTenant):
		targetType = TargetTenant
	case string(TargetUser):
		targetType = TargetUser
	default:
		return Target{}, apperr.Validation(map[string][]string{
			"target_type": {"The selected target_type is not supported."},
		})
	}

	var id int64
	if rawID := strings.TrimSpace(r.URL.Query().Get("target_id")); rawID != "" {
		parsed, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil || parsed < 0 {
			return Target{}, apperr.Validation(map[string][]string{
				"target_id": {"The target_id must be a positive integer."},
			})
		}
		id = parsed
	}

	if targetType == TargetGlobal {
		if id != 0 {
			return Target{}, apperr.Validation(map[string][]string{
				"target_id": {"The target_id must be omitted for the global target."},
			})
		}
		return Target{Type: TargetGlobal}, nil
	}
	if id <= 0 {
		return Target{}, apperr.Validation(map[string][]string{
			"target_id": {"The target_id is required for this target."},
		})
	}
	return Target{Type: targetType, ID: id}, nil
}

// can is route middleware requiring a permission (Laravel's can:...).
func (h *Handler) can(permission string) func(http.Handler) http.Handler {
	return authz.Require(h.checker, permission, h.Responder)
}
