package dev

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/access"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/backup"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/featureflags"
	filesmod "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/files"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/identity"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/maintenance"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/scheduler"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/telemetry"
)

const (
	defaultAdminLimit = 25
	maxAdminLimit     = 100
)

// Deps is the dependency set for the developer dashboard handler. It replaces
// the positional constructor so each management panel can grow its own
// dependencies without churn at every call site.
type Deps struct {
	Checker       *authz.Checker
	Permissions   *access.PermissionService
	QueueAdmin    queue.Admin
	Queue         queue.Engine
	Playground    bool
	Actions       ActionRepository
	Telemetry     TelemetryRepository
	Sessions      SessionRepository
	Developers    DeveloperRepository
	Files         FileRepository
	FileService   *filesmod.Service
	Backups       *backup.Service
	Targets       TargetRepository
	FeatureFlags  *featureflags.Service
	Settings      *settings.Service
	Identity      *identity.Service
	Exchange      *identity.ExchangeService
	Roles         access.RoleRepository
	RoleService   *access.RoleService
	Collector     *telemetry.Collector
	Logger        *slog.Logger
	RetentionDays int
	// AuditRetentionDays is how long audit rows are kept (shown in the UI).
	AuditRetentionDays int
	Inspector          *InspectorService
	Queries            *dbgen.Queries
	Commands           *CommandService
	// Maintenance is maintenance mode (System page card, top-bar pill).
	Maintenance *maintenance.Service
	// Deliveries is the delivery system (Deliveries panel).
	Deliveries *delivery.Service
	// Health is the cached health report (Overview).
	Health HealthSource
	// Scheduler is the task runner (Scheduler panel); nil hides the panel.
	Scheduler *scheduler.Runner
	Kit       httpx.Kit
}

// panelBase carries what every dashboard panel shares: the permission
// checker, the audit writer and the same httpx.Kit (Bind, PathID, Data, ...)
// that every module handler embeds.
type panelBase struct {
	checker     *authz.Checker
	permissions *access.PermissionService
	audit       *Auditor
	logger      *slog.Logger
	httpx.Kit
}

// manage is route middleware for dashboard mutations (dev.manage).
func (h *Handler) manage(next http.Handler) http.Handler {
	return authz.Require(h.checker, authz.PermDevManage, h.Responder)(next)
}

func parseLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultAdminLimit, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > maxAdminLimit {
		return 0, apperr.Validation(map[string][]string{
			"limit": {"The limit must be between 1 and 100."},
		})
	}
	return value, nil
}

func parseCursor(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, apperr.Validation(map[string][]string{"cursor": {"The cursor is invalid."}})
	}
	return value, nil
}

func parseOptionalID(raw, field string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 1 {
		return 0, apperr.Validation(map[string][]string{field: {"The identifier is invalid."}})
	}
	return value, nil
}
