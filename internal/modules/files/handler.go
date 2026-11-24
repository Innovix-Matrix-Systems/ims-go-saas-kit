package files

import (
	"context"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// multipartSlack leaves room for multipart framing beyond the file limit.
const multipartSlack = 1 << 20

// Handler serves the file resource endpoints.
type Handler struct {
	service *Service
	checker *authz.Checker
	httpx.Kit
}

// NewHandler creates the file handler.
func NewHandler(service *Service, checker *authz.Checker, kit httpx.Kit) *Handler {
	return &Handler{service: service, checker: checker, Kit: kit}
}

// surface is one of the two file route families. The tenant family
// (/files, behind tenancy.Resolve) scopes every read and write to the
// effective tenant — the caller's own, or the acting tenant when a platform
// account impersonates. The internal family (/internal/files, InternalOnly)
// spans every tenant and uploads global (tenantless) files.
type surface struct {
	internal   bool
	prefix     string
	permView   string
	permAll    string
	permCreate string
	permDelete string
}

var (
	tenantSurface = surface{
		prefix:     "/api/v1/files/",
		permView:   authz.PermFileView,
		permAll:    authz.PermFileViewAll,
		permCreate: authz.PermFileCreate,
		permDelete: authz.PermFileDelete,
	}
	internalSurface = surface{
		internal:   true,
		prefix:     "/api/v1/internal/files/",
		permView:   authz.PermFileInternalView,
		permAll:    authz.PermFileInternalViewAll,
		permCreate: authz.PermFileInternalCreate,
		permDelete: authz.PermFileInternalDelete,
	}
)

// filter is the tenant predicate for reads and deletes: every tenant on the
// internal family; on the tenant family the resolved scope decides (a
// platform account's global read spans every tenant, otherwise the
// effective tenant).
func (s surface) filter(r *http.Request) tenancy.Filter {
	if s.internal {
		return tenancy.Unscoped()
	}
	return tenancy.From(r).Filter()
}

// uploadTenant is the tenant stamped on a new upload: 0 (global) on the
// internal family, the effective tenant on the tenant family. Resolve
// rejects a platform account's write without acting headers, so the tenant
// family never stamps 0.
func (s surface) uploadTenant(r *http.Request) int64 {
	if s.internal {
		return 0
	}
	return tenancy.From(r).EffectiveTenantID()
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, tenantSurface)
}

func (h *Handler) indexInternal(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, internalSurface)
}

func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	h.upload(w, r, tenantSurface)
}

func (h *Handler) storeInternal(w http.ResponseWriter, r *http.Request) {
	h.upload(w, r, internalSurface)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	h.showFile(w, r, tenantSurface)
}

func (h *Handler) showInternal(w http.ResponseWriter, r *http.Request) {
	h.showFile(w, r, internalSurface)
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	h.downloadFile(w, r, tenantSurface)
}

func (h *Handler) downloadInternal(w http.ResponseWriter, r *http.Request) {
	h.downloadFile(w, r, internalSurface)
}

func (h *Handler) destroy(w http.ResponseWriter, r *http.Request) {
	h.deleteFile(w, r, tenantSurface)
}

func (h *Handler) destroyInternal(w http.ResponseWriter, r *http.Request) {
	h.deleteFile(w, r, internalSurface)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, s surface) {
	scope := s.filter(r)
	page, perPage, offset := httpx.PageParams(r)
	filter := ListFilter{
		AllTenants: scope.All,
		TenantID:   scope.TenantID,
		Type:       r.URL.Query().Get("type"),
		Limit:      int32(perPage),
		Offset:     int32(offset),
	}
	if raw := r.URL.Query().Get("attached"); raw != "" {
		if value, err := strconv.ParseBool(raw); err == nil {
			filter.Attached = &value
		}
	}

	items, total, err := h.service.List(r.Context(), filter)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]Response, 0, len(items))
	for _, file := range items {
		data = append(data, NewResponse(file, h.fileURL(r, s, file)))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request, s surface) {
	form, err := httpx.Multipart(w, r, MaxFileSize+multipartSlack)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	header, err := httpx.FormFile(form, "file")
	if err != nil {
		h.Error(w, r, err)
		return
	}
	file, err := h.service.Create(r.Context(), authn.UserID(r), s.uploadTenant(r), header)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, NewResponse(*file, h.fileURL(r, s, *file)))
}

func (h *Handler) showFile(w http.ResponseWriter, r *http.Request, s surface) {
	file, _ := h.ownedFile(w, r, s, viewDecision)
	if file == nil {
		return
	}
	h.Data(w, r, http.StatusOK, NewResponse(*file, h.fileURL(r, s, *file)))
}

func (h *Handler) deleteFile(w http.ResponseWriter, r *http.Request, s surface) {
	file, _ := h.ownedFile(w, r, s, deleteDecision)
	if file == nil {
		return
	}
	scope := s.filter(r)
	if err := h.service.Delete(r.Context(), file.ID, scope.All, scope.TenantID); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

// downloadFile streams the file through an authorized route (the only way to
// read files on the private disk). Same access rule as show.
func (h *Handler) downloadFile(w http.ResponseWriter, r *http.Request, s surface) {
	file, _ := h.ownedFile(w, r, s, viewDecision)
	if file == nil {
		return
	}
	reader, _, err := h.service.Open(r.Context(), file.ID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", file.MimeType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": file.FileName}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", "private, no-store")
	if file.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
	}
	_, _ = io.Copy(w, reader)
}

// fileURL is the public URL, or the authorized download route (of the same
// route family) for private files.
func (h *Handler) fileURL(r *http.Request, s surface, file File) string {
	if url := h.service.URL(r.Context(), file); url != "" {
		return url
	}
	return httpx.AbsoluteURL(r, s.prefix+strconv.FormatInt(file.ID, 10)+"/download")
}

type decision int

const (
	viewDecision decision = iota
	deleteDecision
)

// ownedFile loads the file (scoped by the route family) and applies the
// access policy: full-permission roles may access any file in scope;
// otherwise the creator needs the granular permission (view) or, for delete,
// the family's create permission.
func (h *Handler) ownedFile(w http.ResponseWriter, r *http.Request, s surface, decision decision) (*File, error) {
	id, err := filePathID(r)
	if err != nil {
		h.Error(w, r, err)
		return nil, err
	}
	scope := s.filter(r)
	file, err := h.service.Get(r.Context(), id, scope.All, scope.TenantID)
	if err != nil {
		h.Error(w, r, err)
		return nil, err
	}

	allowed, err := h.mayAccess(r, authn.UserID(r), s, *file, decision)
	if err != nil {
		h.Error(w, r, err)
		return nil, err
	}
	if !allowed {
		forbidden := apperr.New(apperr.Forbidden, "You are not allowed to access this file.")
		h.Error(w, r, forbidden)
		return nil, forbidden
	}
	return file, nil
}

func (h *Handler) mayAccess(r *http.Request, userID int64, s surface, file File, decision decision) (bool, error) {
	ctx := r.Context()
	if decision == viewDecision {
		if can, err := h.checker.Can(ctx, userID, s.permAll); err != nil {
			return false, err
		} else if can {
			return true, nil
		}
		return h.creatorWith(ctx, userID, file, s.permView)
	}

	if can, err := h.checker.Can(ctx, userID, s.permDelete); err != nil {
		return false, err
	} else if can {
		return true, nil
	}
	return h.creatorWith(ctx, userID, file, s.permCreate)
}

func (h *Handler) creatorWith(ctx context.Context, userID int64, file File, permission string) (bool, error) {
	if file.CreatedBy == nil || *file.CreatedBy != userID {
		return false, nil
	}
	return h.checker.Can(ctx, userID, permission)
}

func filePathID(r *http.Request) (int64, error) {
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, apperr.New(apperr.InvalidInput, "The provided identifier is invalid.")
	}
	return id, nil
}

// can is route middleware requiring a permission (Laravel's can:...).
func (h *Handler) can(permission string) func(http.Handler) http.Handler {
	return authz.Require(h.checker, permission, h.Responder)
}

// uploadLogo replaces the organization's logo (multipart field "logo"). The
// response is the stored file; its url is public, for the e-mail branding.
func (h *Handler) uploadLogo(w http.ResponseWriter, r *http.Request) {
	form, err := httpx.Multipart(w, r, MaxAvatarSize+multipartSlack)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	header, err := httpx.FormFile(form, "logo")
	if err != nil {
		h.Error(w, r, err)
		return
	}
	file, err := h.service.UpdateLogo(r.Context(), authn.UserID(r), tenancy.From(r).EffectiveTenantID(), header)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, NewResponse(*file, h.fileURL(r, tenantSurface, *file)))
}
