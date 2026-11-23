package privacy

import (
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/dataprocessing"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// Handler serves the privacy routes.
type Handler struct {
	service  *Service
	accounts *authn.Service
	checker  *authz.Checker
	httpx.Kit
}

// NewHandler creates the privacy handler; accounts confirms passwords.
func NewHandler(service *Service, accounts *authn.Service, checker *authz.Checker, kit httpx.Kit) *Handler {
	return &Handler{service: service, accounts: accounts, checker: checker, Kit: kit}
}

func (h *Handler) can(permission string) func(http.Handler) http.Handler {
	return authz.Require(h.checker, permission, h.Responder)
}

// --- the caller's own account ---------------------------------------------

func (h *Handler) deletionStatus(w http.ResponseWriter, r *http.Request) {
	request, err := h.service.Deletion(r.Context(), authn.UserID(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewRequestResponse(*request))
}

func (h *Handler) requestDeletion(w http.ResponseWriter, r *http.Request) {
	var req PasswordRequest
	if !h.Bind(w, r, &req) {
		return
	}
	userID := authn.UserID(r)
	if _, err := h.accounts.ConfirmPassword(r.Context(), userID, req.Password); err != nil {
		h.Error(w, r, err)
		return
	}
	request, err := h.service.RequestDeletion(r.Context(), userID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusAccepted, NewRequestResponse(*request))
}

func (h *Handler) cancelDeletion(w http.ResponseWriter, r *http.Request) {
	request, err := h.service.CancelDeletion(r.Context(), authn.UserID(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewRequestResponse(*request))
}

func (h *Handler) exportOwn(w http.ResponseWriter, r *http.Request) {
	var req PasswordRequest
	if !h.Bind(w, r, &req) {
		return
	}
	userID := authn.UserID(r)
	if _, err := h.accounts.ConfirmPassword(r.Context(), userID, req.Password); err != nil {
		h.Error(w, r, err)
		return
	}
	// The caller's own account: no tenant boundary to check.
	job, err := h.service.StartExport(r.Context(), tenancy.Scope{Internal: true}, ExportInput{UserID: userID, Source: SourceSelf, RequestedBy: userID})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusAccepted, newExportJobResponse(job))
}

// --- another user (admins) ------------------------------------------------

func (h *Handler) erase(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	var req EraseRequest
	if r.ContentLength != 0 && !h.Bind(w, r, &req) {
		return
	}
	request, err := h.service.ScheduleErasure(r.Context(), tenancy.From(r), authn.UserID(r), id, req.Immediate)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	status := http.StatusAccepted
	if request.Status == StatusCompleted {
		status = http.StatusOK
	}
	h.Data(w, r, status, NewRequestResponse(*request))
}

func (h *Handler) cancelErase(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	request, err := h.service.CancelErasure(r.Context(), tenancy.From(r), authn.UserID(r), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewRequestResponse(*request))
}

func (h *Handler) exportUser(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	job, err := h.service.StartExport(r.Context(), tenancy.From(r), ExportInput{UserID: id, Source: SourceAdmin, RequestedBy: authn.UserID(r)})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusAccepted, newExportJobResponse(job))
}

func newExportJobResponse(job *dataprocessing.Job) ExportJobResponse {
	jobID := ""
	if job.QueueJobID != nil {
		jobID = *job.QueueJobID
	}
	return ExportJobResponse{JobID: jobID, Status: job.Status, Message: "Export queued. You will get an e-mail with the download link."}
}
