package app

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"regexp"
	"strconv"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing"
	billingdomain "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
	billingservice "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/service"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/notifications"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/onboarding"
	privacymodule "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/privacy"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/crypto"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/mail"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/sms"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/storage"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

// newDelivery builds the delivery system: mail through the
// configured mail driver, SMS through the SMS driver, push to the log until
// a provider is added; the e-mail brand from settings.
func newDelivery(cfg *config.Config, repo delivery.Repository, users *dbgen.Queries, engine queue.Engine, mailDriver mail.Driver,
	settingsService *settings.Service, guard *billingservice.Guard, disks *storage.Registry, encrypter *crypto.Encrypter, logger *slog.Logger,
) (*delivery.Service, error) {
	smsDriver, err := sms.New(cfg.SMS, logger, cfg.IsDebug())
	if err != nil {
		return nil, err
	}
	templates, err := newTemplateRegistry()
	if err != nil {
		return nil, err
	}
	return delivery.NewService(templates, repo, engine, logger).
		WithSender(delivery.Mail, delivery.MailSender{Driver: mailDriver}).
		WithSender(delivery.SMS, delivery.SMSSender{Driver: smsDriver}).
		WithSender(delivery.Push, delivery.LogPushSender{Logger: logger, Debug: cfg.IsDebug()}).
		WithBrand(settingsBrand{settings: settingsService, guard: guard, logger: logger}).
		WithAttachments(storageOpener{disks: disks}, int64(cfg.Mail.MaxAttachmentMB)<<20).
		WithEncrypter(encrypter).
		WithLocales(accountLocales{users: users, settings: settingsService, logger: logger}).
		WithFallbackLocale(cfg.App.FallbackLocale), nil
}

//go:embed all:templates
var appTemplatesFS embed.FS

// newTemplateRegistry registers every module's delivery templates. The
// platform brings the mail layout only; a module that contacts people adds
// its templates here. A later folder with the same name replaces an earlier
// one, so a project changes a kit template by registering its own last.
func newTemplateRegistry() (*delivery.Registry, error) {
	appTemplates, err := fs.Sub(appTemplatesFS, "templates")
	if err != nil {
		return nil, err
	}
	registry := delivery.NewRegistry()
	for _, templates := range []fs.FS{
		authn.Templates(),
		onboarding.Templates(),
		notifications.Templates(),
		billing.Templates(),
		privacymodule.Templates(),
		appTemplates, // new-sign-in
	} {
		if err := registry.Register(templates); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// settingsBrand reads the e-mail brand from the branding.* settings. A
// tenant's own values apply only when its plan has custom.branding;
// otherwise every tenant's mail shows the platform brand.
type settingsBrand struct {
	settings *settings.Service
	guard    *billingservice.Guard
	logger   *slog.Logger
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Brand implements delivery.BrandSource.
func (b settingsBrand) Brand(ctx context.Context, tenantID int64) delivery.Brand {
	scope := tenancy.Scope{}
	if tenantID > 0 && b.guard != nil {
		if custom, err := b.guard.Can(ctx, tenantID, billingdomain.FeatureCustomBranding); err == nil && custom {
			scope = tenancy.Scope{TenantID: tenantID}
		}
	}
	brand := delivery.Brand{
		Name:    b.text(ctx, scope, "branding.name"),
		LogoURL: b.text(ctx, scope, "branding.logo_url"),
		Color:   b.text(ctx, scope, "branding.color"),
		Footer:  b.text(ctx, scope, "branding.footer"),
	}
	if brand.Name == "" {
		brand.Name = b.text(ctx, tenancy.Scope{}, "system.name")
	}
	if !hexColor.MatchString(brand.Color) {
		brand.Color = "#2563eb"
	}
	return brand
}

func (b settingsBrand) text(ctx context.Context, scope tenancy.Scope, name string) string {
	value, err := b.settings.Get(ctx, scope, name)
	if err != nil {
		b.logger.Warn("delivery: brand setting unavailable", "setting", name, "error", err)
		return ""
	}
	text, _ := value.(string)
	return text
}

// storageOpener reads mail attachments from the storage disks.
type storageOpener struct{ disks *storage.Registry }

// Open implements delivery.AttachmentOpener.
func (s storageOpener) Open(ctx context.Context, disk, path string) (io.ReadCloser, error) {
	d, err := s.disks.Disk(disk)
	if err != nil {
		return nil, err
	}
	return d.Get(ctx, path)
}

// accountDeliverer sends authn's account messages (one-time codes, recovery
// links) through the delivery system. They are sensitive: the code or link
// signs someone in.
type accountDeliverer struct{ delivery *delivery.Service }

// DeliverAccount implements authn.Deliverer.
func (a accountDeliverer) DeliverAccount(ctx context.Context, message authn.AccountMessage) error {
	return a.delivery.Send(ctx, delivery.Message{
		To: delivery.Recipient{
			UserID: message.User.ID, TenantID: message.User.TenantID,
			Name: message.User.Name, Email: message.User.Email,
		},
		Template:  message.Template,
		Data:      message.Data,
		Sensitive: true,
	})
}

// DeliveryTestMessage is the message `ims delivery:test` and the dashboard's
// delivery:test command send: a plain test message, or a template rendered
// with its sample data.
func DeliveryTestMessage(service *delivery.Service, channel delivery.Channel, to, template, locale string) (delivery.Message, error) {
	message := delivery.Message{Channels: []delivery.Channel{channel}, To: delivery.Recipient{Locale: locale}}
	switch channel {
	case delivery.Mail:
		message.To.Email = to
	case delivery.SMS:
		message.To.Phone = to
	case delivery.Push:
		id, err := strconv.ParseInt(to, 10, 64)
		if err != nil || id < 1 {
			return delivery.Message{}, fmt.Errorf("push needs a user id as the recipient, got %q", to)
		}
		message.To.UserID = id
	default:
		return delivery.Message{}, fmt.Errorf("unknown channel %q (mail|sms|push)", channel)
	}
	if template == "" {
		message.Subject = "Test message"
		message.Text = "This is a test message from ims delivery:test. If you can read it, delivery works."
		message.HTML = "<p>This is a test message from <code>ims delivery:test</code>.</p><p>If you can read it, delivery works.</p>"
		return message, nil
	}
	sample, err := service.Templates().Sample(template)
	if err != nil {
		return delivery.Message{}, err
	}
	message.Template, message.Data = template, sample
	return message, nil
}

// billingFeatures answers the notifications module's plan questions
// (sms.notifications) from the billing guard.
type billingFeatures struct{ guard *billingservice.Guard }

// Allows implements notifications.FeatureGate. An error counts as "no".
func (b billingFeatures) Allows(ctx context.Context, tenantID int64, feature string) bool {
	allowed, err := b.guard.Can(ctx, tenantID, billingdomain.Feature(feature))
	return err == nil && allowed
}
