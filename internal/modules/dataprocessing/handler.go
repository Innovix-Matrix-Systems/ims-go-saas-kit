package dataprocessing

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// Handler serves processing-job HTTP endpoints.
type Handler struct {
	service  *Service
	settings *settings.Service
	checker  *authz.Checker
	httpx.Kit
}

// NewHandler creates the processing-job handler.
func NewHandler(service *Service, settingsService *settings.Service, checker *authz.Checker, kit httpx.Kit) *Handler {
	return &Handler{service: service, settings: settingsService, checker: checker, Kit: kit}
}

// index lists everyone's jobs (data_processing_job.view.all), or one
// person's with ?user_id=.
func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	scope := tenancy.From(r)
	var userID int64
	if raw := r.URL.Query().Get("user_id"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			h.Error(w, r, apperr.Validation(map[string][]string{"user_id": {"The user_id must be a positive integer."}}))
			return
		}
		userID = parsed
	}
	h.list(w, r, scope, userID)
}

func (h *Handler) userJobs(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	scope := tenancy.From(r)
	h.list(w, r, scope, identity.UserID)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, scope tenancy.Scope, userID int64) {
	page, perPage, offset := httpx.PageParams(r)
	filter := ListFilter{
		Search:   r.URL.Query().Get("search"),
		Type:     r.URL.Query().Get("type"),
		Status:   r.URL.Query().Get("status"),
		UserID:   userID,
		ViewerID: authn.UserID(r),
		Limit:    int32(perPage),
		Offset:   int32(offset),
	}
	items, total, err := h.service.List(r.Context(), scope, filter)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	names := h.service.ActorNames(r.Context(), items)
	data := make([]Response, 0, len(items))
	for _, item := range items {
		response := h.respond(r.Context(), item)
		if item.UserID != nil {
			if name, ok := names[*item.UserID]; ok {
				response.UserName = &name
			}
		}
		data = append(data, response)
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	scope := tenancy.From(r)
	job, _ := h.ownedJob(w, r, scope, identity.UserID)
	if job == nil {
		return
	}
	h.Data(w, r, http.StatusOK, h.respond(r.Context(), *job))
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	scope := tenancy.From(r)
	job, _ := h.ownedJob(w, r, scope, identity.UserID)
	if job == nil {
		return
	}
	download, err := h.service.OpenDownload(r.Context(), scope, *job.QueueJobID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	defer download.Reader.Close()

	contentType := download.Mime
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", url.PathEscape(download.FileName)))
	if download.Size > 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", download.Size))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, download.Reader)
}

func (h *Handler) cleanupSettings(w http.ResponseWriter, r *http.Request) {
	scope := tenancy.From(r)
	days := CleanupSetting(r.Context(), h.settings, scope)
	h.Data(w, r, http.StatusOK, CleanupSettingsResponse{Days: days})
}

func (h *Handler) updateCleanupSettings(w http.ResponseWriter, r *http.Request) {
	var req CleanupSettingsRequest
	if !h.Bind(w, r, &req) {
		return
	}
	scope := tenancy.From(r)
	if err := h.settings.Set(r.Context(), scope, CleanupSettingKey, req.Days); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, CleanupSettingsResponse{Days: req.Days})
}

// ownedJob loads a job by its public id and enforces ownership or the
// view-all permission.
func (h *Handler) ownedJob(w http.ResponseWriter, r *http.Request, scope tenancy.Scope, userID int64) (*Job, error) {
	jobID := chi.URLParam(r, "jobId")
	if jobID == "" {
		err := apperr.New(apperr.InvalidInput, "The provided identifier is invalid.")
		h.Error(w, r, err)
		return nil, err
	}
	job, err := h.service.GetByJobID(r.Context(), scope, jobID)
	if err != nil {
		h.Error(w, r, err)
		return nil, err
	}
	// A personal-data export belongs to whoever asked for it; no view-all
	// permission reads someone else's.
	if job.Type == TypePrivacyExport {
		if job.UserID != nil && *job.UserID == userID {
			return job, nil
		}
		notFound := apperr.New(apperr.ResourceNotFound, "Processing job not found.")
		h.Error(w, r, notFound)
		return nil, notFound
	}
	if job.TenantID == nil {
		if err := h.checker.MustCan(r.Context(), userID, authz.PermDataProcessingViewAll); err != nil {
			h.Error(w, r, err)
			return nil, err
		}
		return job, nil
	}
	if job.UserID != nil && *job.UserID == userID {
		return job, nil
	}
	if can, err := h.checker.Can(r.Context(), userID, authz.PermDataProcessingViewAll); err != nil {
		h.Error(w, r, err)
		return nil, err
	} else if can {
		return job, nil
	}
	forbidden := apperr.New(apperr.Forbidden, "You are not allowed to access this job.")
	h.Error(w, r, forbidden)
	return nil, forbidden
}

// can is route middleware requiring a permission (Laravel's can:...).
func (h *Handler) can(permission string) func(http.Handler) http.Handler {
	return authz.Require(h.checker, permission, h.Responder)
}
