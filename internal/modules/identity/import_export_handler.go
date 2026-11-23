package identity

import (
	"fmt"
	"net/http"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// User import/export: templates, background jobs and internal sync runs.

// importUsersBackground queues a background user import (multipart upload).
func (h *Handler) importUsersBackground(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	scope := tenancy.From(r)
	permission := authz.PermUserImport
	if scope.Internal {
		permission = authz.PermUserInternalImport
	}
	if err := h.checker.MustCan(r.Context(), identity.UserID, permission); err != nil {
		h.Error(w, r, err)
		return
	}
	if h.exchange == nil {
		h.Error(w, r, apperr.New(apperr.ServiceUnavailable, "User imports are not available."))
		return
	}
	form, err := httpx.Multipart(w, r, maxImportBytes+importMultipartSlack)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	header, err := httpx.FormFile(form, "file")
	if err != nil {
		h.Error(w, r, err)
		return
	}
	job, err := h.exchange.StartImport(r.Context(), scope, identity.UserID, header)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, BackgroundJobResponse{
		JobID:   derefString(job.QueueJobID),
		Status:  job.Status,
		Message: "Job queued successfully.",
	})
}

// exportUsersBackground queues a background user export.
func (h *Handler) exportUsersBackground(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	scope := tenancy.From(r)
	permission := authz.PermUserExport
	if scope.Internal {
		permission = authz.PermUserInternalExport
	}
	if err := h.checker.MustCan(r.Context(), identity.UserID, permission); err != nil {
		h.Error(w, r, err)
		return
	}
	if h.exchange == nil {
		h.Error(w, r, apperr.New(apperr.ServiceUnavailable, "User exports are not available."))
		return
	}
	var req ExportFiltersRequest
	if !h.Bind(w, r, &req) {
		return
	}
	job, err := h.exchange.StartExport(r.Context(), scope, identity.UserID, ExportRequest{
		Search:   req.Search,
		IsActive: req.IsActive,
		RoleName: req.RoleName,
		OrderBy:  req.OrderBy,
		OrderDir: req.OrderDirection,
	})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, BackgroundJobResponse{
		JobID:   derefString(job.QueueJobID),
		Status:  job.Status,
		Message: "Job queued successfully.",
	})
}

// importTemplate downloads the CSV import template (heading + sample rows).
func (h *Handler) importTemplate(w http.ResponseWriter, r *http.Request) {
	if h.exchange == nil {
		h.Error(w, r, apperr.New(apperr.ServiceUnavailable, "User imports are not available."))
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="user_import_template.csv"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(h.exchange.ImportTemplate()))
}

// --- Internal (tenant-exempt) counterparts ---

// exportUsersInternal streams an xlsx export across tenants.
func (h *Handler) exportUsersInternal(w http.ResponseWriter, r *http.Request) {
	if h.exchange == nil {
		h.Error(w, r, apperr.New(apperr.ServiceUnavailable, "User exports are not available."))
		return
	}
	var req ExportFiltersRequest
	if !h.Bind(w, r, &req) {
		return
	}
	scope := tenancy.From(r)
	data, err := h.exchange.ExportUsers(r.Context(), scope, ExportRequest{
		Search:   req.Search,
		IsActive: req.IsActive,
		RoleName: req.RoleName,
		OrderBy:  req.OrderBy,
		OrderDir: req.OrderDirection,
	})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	fileName := fmt.Sprintf("internal_user_data_%s.xlsx", time.Now().Format("2006-01-02_H-04-05"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+fileName+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// importUsersInternal imports a spreadsheet synchronously across tenants.
func (h *Handler) importUsersInternal(w http.ResponseWriter, r *http.Request) {
	if h.exchange == nil {
		h.Error(w, r, apperr.New(apperr.ServiceUnavailable, "User imports are not available."))
		return
	}
	form, err := httpx.Multipart(w, r, maxImportBytes+importMultipartSlack)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	header, err := httpx.FormFile(form, "file")
	if err != nil {
		h.Error(w, r, err)
		return
	}
	scope := tenancy.From(r)
	result, err := h.exchange.ImportUsers(r.Context(), scope, authn.UserID(r), header)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, result)
}
