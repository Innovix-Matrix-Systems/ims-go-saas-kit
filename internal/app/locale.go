package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/i18n"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

// The language someone is addressed in, most specific first:
//
//	screen:   the account's choice → this browser's pick (signed out) →
//	          the organization's default → the browser's languages → APP_LOCALE
//	          (decided by the client app, sent as Accept-Language)
//	API:      Accept-Language when it names a supported language → the
//	          account's choice → the organization's default → APP_LOCALE
//	e-mails:  the account's choice → the organization's default →
//	          the request's language → APP_FALLBACK_LOCALE
//
// The organization's default is the general.default_locale setting (tenant →
// global → empty).

// defaultLocaleSetting is the organization's language.
const defaultLocaleSetting = "general.default_locale"

// accountLocales finds a person's language from their account and their
// organization: the delivery system's LocaleSource.
type accountLocales struct {
	translator *i18n.Translator
	users      *dbgen.Queries
	settings   *settings.Service
	logger     *slog.Logger
}

// Locale implements delivery.LocaleSource.
func (a accountLocales) Locale(ctx context.Context, to delivery.Recipient) string {
	tenantID := to.TenantID
	if to.UserID > 0 && a.users != nil {
		user, err := a.users.GetUser(ctx, dbgen.GetUserParams{ID: to.UserID, AllTenants: true})
		switch {
		case err == nil:
			if user.Locale != nil && a.translator.IsSupported(*user.Locale) {
				return *user.Locale
			}
			if tenantID == 0 && user.TenantID != nil {
				tenantID = *user.TenantID
			}
		case !errors.Is(err, pgx.ErrNoRows):
			a.logger.WarnContext(ctx, "delivery: recipient language unreadable", "user_id", to.UserID, "error", err)
		}
	}
	return organizationLocale(ctx, a.translator, a.settings, tenantID)
}

// organizationLocale is a tenant's default language ("" when not set or no
// longer offered).
func organizationLocale(ctx context.Context, translator *i18n.Translator, service *settings.Service, tenantID int64) string {
	if service == nil {
		return ""
	}
	value, err := service.Get(ctx, tenancy.Scope{TenantID: tenantID}, defaultLocaleSetting)
	if err != nil {
		return ""
	}
	code, _ := value.(string)
	if !translator.IsSupported(code) {
		return ""
	}
	return code
}

// accountLocale answers a signed-in request without a matching
// Accept-Language in the account's language, then the organization's.
// Mounted right after authentication.
func accountLocale(translator *i18n.Translator, service *settings.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if i18n.FromHeader(ctx) {
				next.ServeHTTP(w, r)
				return
			}
			identity, ok := authn.IdentityFromContext(ctx)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			code := identity.Locale
			if !translator.IsSupported(code) {
				code = organizationLocale(ctx, translator, service, identity.TenantID)
			}
			if code != "" {
				r = r.WithContext(translator.Use(ctx, code))
			}
			next.ServeHTTP(w, r)
		})
	}
}
