package app

import (
	"log/slog"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/access"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/announcements"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/auth"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/dataprocessing"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/dev"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/featureflags"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/files"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/identity"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/notifications"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/oauth"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/onboarding"
	privacymodule "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/privacy"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/webhooks"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/storage"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/privacy"
)

// newPrivacy builds the privacy registry and service. Each module that stores
// data about a user registers its contributor here; identity tombstones the
// users row, so it goes last. The coverage test
// (internal/modules/privacy/coverage_integration_test.go) fails when a
// migration adds a user-reference column that is neither claimed below nor
// exempted with a reason.
func newPrivacy(cfg *config.Config, tx *database.TxManager, disks *storage.Registry, checker *authz.Checker, authenticator authn.Authenticator,
	recorder *activity.Recorder, settingsService *settings.Service, deliveryService *delivery.Service,
	processing *dataprocessing.Service, engine queue.Engine, jobs *queue.Registry, logger *slog.Logger) (*privacymodule.Service, error) {
	registry := privacy.NewRegistry()
	for _, contributor := range []privacy.Contributor{
		auth.PrivacyContributor{},
		oauth.PrivacyContributor{},
		access.PrivacyContributor{},
		notifications.PrivacyContributor{},
		announcements.PrivacyContributor{},
		onboarding.PrivacyContributor{},
		files.NewPrivacyContributor(disks),
		delivery.PrivacyContributor{},
		activity.PrivacyContributor{},
		webhooks.PrivacyContributor{},
		dataprocessing.PrivacyContributor{},
		settings.PrivacyContributor{},
		featureflags.PrivacyContributor{},
		dev.PrivacyContributor{},
		identity.PrivacyContributor{}, // last: the tombstone
	} {
		if err := registry.Register(contributor); err != nil {
			return nil, err
		}
	}
	for column, reason := range privacyExemptions {
		registry.Exempt(column, reason)
	}
	service := privacymodule.NewService(privacymodule.NewRepository(tx.Queries()), tx, registry, checker, authenticator, logger).
		WithStorage(disks).
		WithActivity(recorder).
		WithSettings(settingsService).
		WithDelivery(deliveryService).
		WithExports(processing, cfg.Processing, engine, cfg.App.URL)
	if err := service.RegisterJobs(jobs); err != nil {
		return nil, err
	}
	return service, nil
}

// privacyExemptions are user-reference columns that erasure deliberately
// leaves alone. Each one points at the tombstone afterwards, which no longer
// identifies anyone.
var privacyExemptions = map[string]string{
	"privacy_requests.user_id":      "the erasure ledger: user ids and counts only, kept as proof of erasure",
	"tenants.owner_user_id":         "an owner cannot be erased (the identity guard refuses until ownership is transferred), so it never points at an erased user",
	"privacy_requests.requested_by": "the erasure ledger: who asked, by id",
	"backup_runs.actor_user_id":     "audit stamp: who started a backup",
	"maintenance.by_user_id":        "audit stamp: who put the app into maintenance",
}
