package dev

import (
	"net/http"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/backup"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/featureflags"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/identity"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/maintenance"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/scheduler"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/telemetry"
)

// Handler serves the developer dashboard JSON API at /dev/api.
type Handler struct {
	panelBase
	queueAdmin        queue.Admin
	queue             queue.Engine
	playgroundEnabled bool
	telemetry         *TelemetryService
	sessions          *SessionService
	developers        *DeveloperService
	files             *FileBrowser
	targets           *TargetService
	tenants           *TenantService
	backups           *backup.Service
	flags             *featureflags.Service
	settings          *settings.Service
	exchange          *identity.ExchangeService
	collector         *telemetry.Collector
	inspector         *InspectorService
	commands          *CommandService
	maintenance       *maintenance.Service
	deliveries        *delivery.Service
	translations      *TranslationService
	overview          *OverviewService
	pools             PoolSource
	stats             *StatsService
	scheduler         *scheduler.Runner
	// auditRetentionDays is how long dev_actions rows are kept.
	auditRetentionDays int
}

// NewHandler creates the developer dashboard handler.
func NewHandler(deps Deps) *Handler {
	if deps.RetentionDays < 1 {
		deps.RetentionDays = 7
	}
	audit := NewAuditor(deps.Actions)
	return &Handler{
		panelBase: panelBase{
			checker:     deps.Checker,
			permissions: deps.Permissions,
			audit:       audit,
			logger:      deps.Logger,
			Kit:         deps.Kit,
		},
		queueAdmin:        deps.QueueAdmin,
		queue:             deps.Queue,
		playgroundEnabled: deps.Playground,
		telemetry:         &TelemetryService{TelemetryRepository: deps.Telemetry, audit: audit, retentionDays: deps.RetentionDays},
		sessions:          &SessionService{repo: deps.Sessions, audit: audit},
		developers: &DeveloperService{repo: deps.Developers, roles: deps.Roles, roleService: deps.RoleService,
			identity: deps.Identity, audit: audit},
		files:              &FileBrowser{FileRepository: deps.Files, files: deps.FileService, audit: audit},
		targets:            &TargetService{TargetRepository: deps.Targets},
		tenants:            &TenantService{queries: deps.Queries},
		backups:            deps.Backups,
		flags:              deps.FeatureFlags,
		settings:           deps.Settings,
		exchange:           deps.Exchange,
		collector:          deps.Collector,
		inspector:          deps.Inspector,
		commands:           deps.Commands,
		maintenance:        deps.Maintenance,
		deliveries:         deps.Deliveries,
		translations:       newTranslationService(deps),
		overview:           newOverviewService(deps),
		pools:              deps.Pools,
		stats:              &StatsService{telemetry: deps.Telemetry, deliveries: deps.Deliveries, now: time.Now},
		scheduler:          deps.Scheduler,
		auditRetentionDays: deps.AuditRetentionDays,
	}
}

// Auditor exposes the audit writer (the auth module records dashboard
// sign-ins through it).
func (h *Handler) Auditor() *Auditor { return h.audit }

// MeResponse is the SPA bootstrap payload: the current identity plus the
// effective permission set (super-admins bypass permission checks, so the
// seeded role permissions are returned as-is).
type MeResponse struct {
	UserID       int64    `json:"user_id"`
	Name         string   `json:"name"`
	Email        string   `json:"email"`
	TenantID     *int64   `json:"tenant_id"`
	BranchID     *int64   `json:"branch_id"`
	IsSuperAdmin bool     `json:"is_super_admin"`
	Playground   bool     `json:"playground"`
	Permissions  []string `json:"permissions"`
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	identity, ok := authn.IdentityFromContext(r.Context())
	if !ok {
		h.Error(w, r, apperr.New(apperr.Unauthorized, "Authentication is required."))
		return
	}
	permissions, err := h.permissions.ForUser(r.Context(), identity.UserID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	super, err := h.checker.IsSuperAdmin(r.Context(), identity.UserID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	names := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		names = append(names, permission.Name)
	}
	var tenantID *int64
	if identity.TenantID != 0 {
		value := identity.TenantID
		tenantID = &value
	}
	var branchID *int64
	if identity.BranchID != 0 {
		value := identity.BranchID
		branchID = &value
	}
	h.Data(w, r, http.StatusOK, MeResponse{
		UserID:       identity.UserID,
		Name:         identity.Name,
		Email:        identity.Email,
		TenantID:     tenantID,
		BranchID:     branchID,
		IsSuperAdmin: super,
		Playground:   h.playgroundEnabled,
		Permissions:  names,
	})
}
