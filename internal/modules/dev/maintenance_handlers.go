package dev

import (
	"net/http"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/maintenance"
)

// Maintenance mode: the System page card and the top-bar pill.

// maintenanceDownRequest is the body of POST /dev/api/maintenance/down.
type maintenanceDownRequest struct {
	Message    string `json:"message" validate:"max=500"`
	RetryAfter int    `json:"retry_after" validate:"omitempty,min=1,max=86400"`
	WithSecret bool   `json:"with_secret"`
}

// maintenanceResponse is the state, plus the bypass secret once after a
// down that generated one.
type maintenanceResponse struct {
	maintenance.State
	Secret *string `json:"secret,omitempty"`
}

func (h *Handler) maintenanceStatus(w http.ResponseWriter, r *http.Request) {
	state, err := h.maintenance.Status(r.Context())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, maintenanceResponse{State: state})
}

func (h *Handler) maintenanceDown(w http.ResponseWriter, r *http.Request) {
	var req maintenanceDownRequest
	if !h.Bind(w, r, &req) {
		return
	}
	actorID := authn.UserID(r)
	started := time.Now()
	state, secret, err := h.maintenance.Down(r.Context(), maintenance.DownInput{
		Message: req.Message, RetryAfter: req.RetryAfter, GenerateSecret: req.WithSecret, ByUserID: actorID,
	})
	h.audit.Record(r.Context(), actorID, "maintenance.down", "maintenance", map[string]any{
		"message": state.Message, "retry_after": state.RetryAfter, "with_secret": req.WithSecret,
	}, started, err)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	response := maintenanceResponse{State: state}
	if secret != "" {
		response.Secret = &secret
	}
	h.Data(w, r, http.StatusOK, response)
}

func (h *Handler) maintenanceUp(w http.ResponseWriter, r *http.Request) {
	actorID := authn.UserID(r)
	started := time.Now()
	changed, err := h.maintenance.Up(r.Context(), actorID)
	h.audit.Record(r.Context(), actorID, "maintenance.up", "maintenance", map[string]any{"was_down": changed}, started, err)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, maintenanceResponse{State: maintenance.State{}})
}
