package app

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/mcp"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/dev"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/oauth"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/dedupe"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/docs"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/maintenance"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/ratelimit"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/storage"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/tracing"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/webui"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// Router assembles the HTTP handler with the standard middleware chain:
// RequestID → RealIP → SecurityHeaders → CORS → AccessLog → Recover → Metrics →
// Maintenance → Timeout → RateLimit → Locale, then the authentication and tenancy groups per
// route family.
func (d *Dependencies) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(httpx.RequestID)
	if !d.Config.IsLocal() {
		r.Use(httpx.PublicOrigin(d.Config.App.URL))
	}
	r.Use(httpx.RealIP(d.Config.HTTP.TrustedProxies))
	if d.Config.Tracing.Enabled {
		// Outside Recover, so a panic's span is still ended (and marked).
		r.Use(httpx.ShareRequestInfo, tracing.Middleware(spanIdentity, httpx.FromTrustedProxy))
	}
	if d.Config.Security.HeadersEnabled {
		r.Use(httpx.SecurityHeaders)
	}
	r.Use(httpx.CORS(
		d.Config.Ops.CORS.AllowedOrigins,
		d.Config.Ops.CORS.AllowedMethods,
		d.Config.Ops.CORS.AllowedHeaders,
		d.Config.Ops.CORS.MaxAge,
	))
	r.Use(httpx.AccessLog(d.Logger))
	r.Use(httpx.Recover(d.Logger, d.Responder))
	if d.Platform != nil && d.Platform.Telemetry != nil {
		r.Use(d.Platform.Telemetry.HTTPMiddleware)
	}
	if d.Config.Ops.MetricsEnabled && d.Metrics != nil {
		r.Use(d.Metrics.Middleware)
	}
	// Maintenance mode: before authentication and any work, after the access
	// log, telemetry and metrics so the 503s are still seen.
	if d.Platform != nil && d.Platform.Maintenance != nil {
		r.Use(maintenance.Guard(d.Platform.Maintenance.Checker(), d.Responder, maintenance.OpenPaths...))
	}
	r.Use(httpx.Timeout(d.Config.HTTP.RequestTimeout, d.Responder))
	// Health probes and metrics scrapes never spend the per-IP budget.
	r.Use(ratelimit.Middleware(d.Limiter, ratelimit.KeyByIP, d.Responder, "/up", "/api/health", "/metrics"))
	r.Use(d.I18n.Middleware(d.Config.App.SupportedLocales, d.Config.App.FallbackLocale))

	r.Get("/up", d.up)
	r.Get("/api/health", d.HealthHandler.ServeHTTP)
	r.Handle("/storage/*", http.StripPrefix("/storage/", storage.StaticHandler(d.Config.Storage.PublicDir)))

	if d.Config.Ops.DocsEnabled {
		r.Get("/docs", docs.UI)
		r.Get("/docs/openapi.yaml", docs.Spec)
	}
	if d.Config.Ops.MetricsEnabled && d.Metrics != nil {
		r.Handle("/metrics", httpx.ProtectOps(d.Config.Ops.MetricsToken, d.Metrics.Handler()))
	}

	// Developer dashboard: one gate for the SPA, its JSON API and the telemetry
	// capture surface. Registered unconditionally so the route table stays
	// stable for openapi:check; disabled deployments answer 404 before
	// authentication.
	r.Group(func(r chi.Router) {
		r.Use(dev.Gate(d.Config.DevDashboard.Enabled, d.Responder))
		r.Get("/dev", d.DevUI.ServeHTTP)
		r.HandleFunc("/dev/*", d.DevUI.ServeHTTP)
		r.Route("/dev/api", func(r chi.Router) {
			r.Use(authn.Authenticate(d.Authenticator, d.Responder))
			r.Use(tenancy.Exempt(d.Responder))
			r.Use(authz.Require(d.Checker, authz.PermDevView, d.Responder))
			d.Dev.RegisterRoutes(r)
		})
	})

	// MCP connector (phase 22): the OAuth authorization server and /mcp.
	// Registered unconditionally so the route table stays stable; with
	// MCP_ENABLED off they answer 404.
	d.OAuth.RegisterEndpoints(r, d.Config.MCP.Enabled)
	mcpEndpoint := &lateHandler{}
	r.With(oauth.Gate(d.Config.MCP.Enabled, d.Responder)).Handle(mcp.Path, mcpEndpoint)

	apiRouter := r.Route("/api/v1", func(r chi.Router) {
		d.System.RegisterRoutes(r)
		d.Auth.RegisterPublicRoutes(r)
		d.Billing.RegisterPublicRoutes(r)
		d.Notifications.RegisterPublicRoutes(r)
		d.Onboarding.RegisterPublicRoutes(r)

		// Public tenant-scoped routes: no sign-in, the tenant comes from
		// X-Tenant-Code (and optionally X-Branch-Id).
		r.Group(func(r chi.Router) {
			r.Use(tenancy.Public(tenancy.PublicOptions{
				Queries: d.TenancyOpts.Queries, Cache: d.TenancyOpts.Cache, Responder: d.Responder,
			}))
			d.Tenants.RegisterPublicTenantRoutes(r)
		})

		r.Group(func(r chi.Router) {
			r.Use(authn.Authenticate(d.Authenticator, d.Responder))
			// Duplicate-request protection (phase 26.7): a write repeated
			// while it runs, or just after it succeeded, is refused; an
			// Idempotency-Key replays the first answer.
			if d.Config.Dedupe.Enabled && d.Platform != nil {
				r.Use(dedupe.Middleware(d.Platform.Cache, d.Responder, dedupeOptions(d.Config, d.Logger)))
			}

			// Session routes act only on the caller's own token: no tenant
			// resolution, so tenantless accounts (super-admin, developer)
			// can sign out too.
			d.Auth.RegisterRoutes(r)
			// The caller's own deletion request and export, the same way.
			d.Privacy.RegisterRoutes(r)

			r.Group(func(r chi.Router) {
				r.Use(tenancy.Resolve(d.TenancyOpts))
				d.Users.RegisterRoutesResolve(r)
				d.Privacy.RegisterRoutesResolve(r)
				d.Branches.RegisterRoutes(r)
				d.Activity.RegisterRoutes(r)
				d.Platform.ActivityExport.RegisterRoutes(r)
				d.Roles.RegisterRoutesResolve(r)
				d.Billing.RegisterRoutesResolve(r)
				d.Notifications.RegisterRoutesResolve(r)
				d.Files.RegisterRoutesResolve(r)
				d.Webhooks.RegisterRoutes(r)
				d.Processing.RegisterRoutes(r)
				d.Settings.RegisterRoutes(r)
				d.Flags.RegisterRoutes(r)
				d.Onboarding.RegisterRoutes(r)
				d.Tenants.RegisterOrganizationRoutes(r)
				d.Dashboard.RegisterRoutes(r)
				d.Announcements.RegisterRoutes(r)
			})

			r.Group(func(r chi.Router) {
				r.Use(tenancy.Exempt(d.Responder))

				// Shared exempt surfaces: permission metadata carries no
				// tenant data.
				d.Permissions.RegisterRoutes(r)
				// The caller's own MCP connections.
				d.OAuth.RegisterRoutes(r)

				// System-only exempt families: tenant-bound identities are
				// rejected before their handlers run, so a granted system
				// permission alone can never reach cross-tenant endpoints.
				r.Group(func(r chi.Router) {
					r.Use(tenancy.InternalOnly(d.Responder))
					d.Tenants.RegisterRoutes(r)
					d.Users.RegisterRoutesExempt(r)
					d.Privacy.RegisterRoutesExempt(r)
					d.Roles.RegisterRoutesExempt(r)
					d.Billing.RegisterRoutesExempt(r)
					d.Notifications.RegisterRoutesExempt(r)
					d.Platform.ActivityExport.RegisterInternalRoutes(r)
					d.Processing.RegisterInternalRoutes(r)
					d.Dashboard.RegisterInternalRoutes(r)
					d.Announcements.RegisterInternalRoutes(r)
					d.Files.RegisterRoutesExempt(r)
					d.Settings.RegisterInternalRoutes(r)
					d.Flags.RegisterInternalRoutes(r)
					d.OAuth.RegisterInternalRoutes(r)
				})
			})
		})
	})

	mcpEndpoint.set(d.mcpHandler(apiRouter))

	// The client app owns the site root and every other path outside the API
	// and tool prefixes, so its client-side routes (and deep links in mails)
	// load the app; unknown API paths still answer a JSON 404.
	if d.AppUI != nil {
		r.Get("/", d.AppUI.ServeHTTP)
	}
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if d.AppUI != nil && (r.Method == http.MethodGet || r.Method == http.MethodHead) && webui.OwnsPath(r.URL.Path) {
			d.AppUI.ServeHTTP(w, r)
			return
		}
		d.Responder.Error(w, r, apperr.New(apperr.ResourceNotFound, "The requested resource was not found."))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		d.Responder.Error(w, r, apperr.New(apperr.MethodNotAllowed, "The HTTP method is not supported for this resource."))
	})

	d.routes = r
	return r
}

func (d *Dependencies) up(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// lateHandler lets /mcp be registered before the /api/v1 router it
// dispatches into exists.
type lateHandler struct{ h http.Handler }

func (l *lateHandler) set(h http.Handler) { l.h = h }

func (l *lateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) { l.h.ServeHTTP(w, r) }

// dedupeOptions is where duplicate-request protection is tuned: routes where
// identical repeats are normal skip it; routes that move money keep a
// succeeded request's fingerprint longer.
func dedupeOptions(cfg *config.Config, logger *slog.Logger) dedupe.Options {
	const money = 30 * time.Second
	return dedupe.Options{
		Window: cfg.Dedupe.Window,
		KeyTTL: cfg.Dedupe.KeyTTL,
		Skip: []string{
			"POST /api/v1/logout",
			"POST /api/v1/notification/{id}/read",
			"POST /api/v1/notification/read-all",
			"POST /api/v1/internal/notification/{id}/read",
			"POST /api/v1/internal/notification/read-all",
		},
		Windows: map[string]time.Duration{
			"POST /api/v1/billing/subscribe":                                 money,
			"POST /api/v1/billing/internal/payments":                         money,
			"POST /api/v1/billing/internal/payment-approvals/{id}/approve":   money,
			"POST /api/v1/billing/internal/payment-approvals/{id}/reject":    money,
			"POST /api/v1/billing/internal/tenants/{tenant_id}/subscription": money,
		},
		// Who sent it, and as whom: staff acting as a tenant or a user write
		// in that scope, so the same body for another tenant is not a repeat.
		Caller: func(r *http.Request) string {
			id := authn.UserID(r)
			if id <= 0 {
				return ""
			}
			return "user:" + strconv.FormatInt(id, 10) +
				"|tenant:" + r.Header.Get(tenancy.ActingTenantHeader) +
				"|branch:" + r.Header.Get(tenancy.ActingBranchHeader) +
				"|as:" + r.Header.Get(tenancy.ActingUserHeader)
		},
		Logger: logger,
	}
}

// spanIdentity reports who a request ran as, for its span.
func spanIdentity(ctx context.Context) (userID, tenantID, impersonatorID int64) {
	if info := httpx.RequestInfoFrom(ctx); info != nil {
		return info.UserID, info.TenantID, info.ImpersonatorID
	}
	return 0, 0, 0
}
