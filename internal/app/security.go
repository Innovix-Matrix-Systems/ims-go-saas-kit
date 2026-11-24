package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// Account security that needs both authn and the settings module: the
// two-factor policy and the new sign-in alert.

// twoFactorPolicy requires two-factor for a tenant's users when the tenant's
// security.require_two_factor setting is on, and for platform accounts
// (no tenant) with AUTH_REQUIRE_2FA_PLATFORM.
type twoFactorPolicy struct {
	settings *settings.Service
	cfg      config.Auth
	logger   *slog.Logger
}

// Required implements authn.TwoFactorPolicy.
func (p twoFactorPolicy) Required(ctx context.Context, user *authn.UserRecord) bool {
	if user.TenantID == 0 {
		return p.cfg.RequireTwoFactorPlatform
	}
	required, err := p.settings.GetBool(ctx, tenancy.Scope{TenantID: user.TenantID}, "security.require_two_factor")
	if err != nil {
		p.logger.Warn("two-factor policy unavailable", "tenant_id", user.TenantID, "error", err)
		return false
	}
	return required
}

// signInAlerts mails a user who signs in from an IP address they have not
// signed in from in the last 90 days (setting security.new_sign_in_alert,
// per tenant). A first sign-in ever is not "new": there is nothing to compare.
// It never blocks the sign-in: problems are logged.
type signInAlerts struct {
	settings *settings.Service
	queries  *dbgen.Queries
	delivery *delivery.Service
	logger   *slog.Logger
}

// TemplateNewSignIn is the alert's delivery template.
const TemplateNewSignIn = "new-sign-in"

// SignedIn implements authn.SignInWatcher.
func (a signInAlerts) SignedIn(ctx context.Context, user *authn.UserRecord, device string) {
	ip := httpx.ClientIP(ctx)
	if ip == "" || user.Email == "" {
		return
	}
	enabled, err := a.settings.GetBool(ctx, tenancy.Scope{TenantID: user.TenantID}, "security.new_sign_in_alert")
	if err != nil || !enabled {
		return
	}
	seen, err := a.queries.RecentLoginFrom(ctx, dbgen.RecentLoginFromParams{UserID: user.ID, Ip: ip})
	if err != nil {
		a.logger.Warn("new sign-in alert: history unavailable", "user_id", user.ID, "error", err)
		return
	}
	if !seen.AnyLogin || seen.FromIp {
		return
	}
	err = a.delivery.Send(ctx, delivery.Message{
		To:       delivery.Recipient{UserID: user.ID, TenantID: user.TenantID, Name: user.Name, Email: user.Email},
		Template: TemplateNewSignIn,
		Data: map[string]any{
			"device":  device,
			"ip":      ip,
			"browser": httpx.DescribeUserAgent(httpx.UserAgent(ctx)),
			"when":    time.Now().UTC().Format("2 Jan 2006, 15:04 UTC"),
		},
	})
	if err != nil {
		a.logger.Warn("new sign-in alert not sent", "user_id", user.ID, "error", err)
	}
}
