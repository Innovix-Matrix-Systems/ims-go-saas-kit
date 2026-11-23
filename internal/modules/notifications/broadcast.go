package notifications

import (
	"context"
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// TenantLister returns the ids of the organizations a broadcast reaches by
// default: every active one.
type TenantLister func(ctx context.Context) ([]int64, error)

// WithTenantLister enables broadcasting to every active organization.
func (s *Service) WithTenantLister(lister TenantLister) *Service {
	s.tenants = lister
	return s
}

// BroadcastInput is a staff announcement.
type BroadcastInput struct {
	Priority string
	Title    string
	Body     *string
	Channels []string
	// TenantIDs limits the broadcast; empty reaches every active organization.
	TenantIDs []int64
}

// Broadcast sends an announcement (platform.announcement) to the members of
// each organization: one notification per organization, since every
// notification belongs to one (GLOBAL targets reach platform staff only).
// It returns how many organizations it reached.
func (s *Service) Broadcast(ctx context.Context, scope tenancy.Scope, actorID int64, input BroadcastInput) (int, error) {
	tenantIDs := input.TenantIDs
	if len(tenantIDs) == 0 {
		if s.tenants == nil {
			return 0, apperr.Validation(map[string][]string{"tenant_ids": {"Choose the organizations to send to."}})
		}
		listed, err := s.tenants(ctx)
		if err != nil {
			return 0, err
		}
		tenantIDs = listed
	}
	sent := 0
	for _, tenantID := range tenantIDs {
		id := tenantID
		if _, _, err := s.Create(ctx, scope, actorID, CreateInput{
			TenantID:  &id,
			EventType: EventAnnouncement,
			Priority:  input.Priority,
			Title:     input.Title,
			Body:      input.Body,
			Targets:   []TargetInput{{Type: TargetTenant}},
			Channels:  input.Channels,
		}); err != nil {
			return sent, err
		}
		sent++
	}
	return sent, nil
}

// BroadcastRequest is the POST /internal/notification/broadcast payload.
type BroadcastRequest struct {
	Priority  string   `json:"priority" validate:"required,oneof=INFO WARNING CRITICAL"`
	Title     string   `json:"title" validate:"required,max=255"`
	Body      *string  `json:"body" validate:"omitempty,max=5000"`
	Channels  []string `json:"channels" validate:"omitempty,max=4"`
	TenantIDs []int64  `json:"tenant_ids" validate:"omitempty,dive,gt=0"`
}

// BroadcastResponse says how many organizations were reached.
type BroadcastResponse struct {
	Organizations int `json:"organizations"`
}

func (h *Handler) broadcast(w http.ResponseWriter, r *http.Request) {
	var req BroadcastRequest
	if !h.Bind(w, r, &req) {
		return
	}
	sent, err := h.service.Broadcast(r.Context(), tenancy.From(r), authn.UserID(r), BroadcastInput{
		Priority: req.Priority, Title: req.Title, Body: req.Body, Channels: req.Channels, TenantIDs: req.TenantIDs,
	})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, BroadcastResponse{Organizations: sent})
}
