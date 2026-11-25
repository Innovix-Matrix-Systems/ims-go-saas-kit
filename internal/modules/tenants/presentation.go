package tenants

import (
	"context"
	"strings"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

// SettingReader reads a tenant's effective settings (the settings service).
type SettingReader interface {
	Get(ctx context.Context, scope tenancy.Scope, name string) (any, error)
}

// Presentation is how an organization presents itself: its e-mail branding
// (which clients also use for its mark) and its default language. Unset or
// unreadable values are nil.
type Presentation struct {
	LogoURL  *string
	Color    *string
	Language *string
}

// WithSettings lets the service read the organization's branding and language.
func (s *Service) WithSettings(settings SettingReader) *Service {
	s.settings = settings
	return s
}

// Presentation reads the tenant's branding and default language. A setting
// that cannot be read is left out rather than failing the response.
func (s *Service) Presentation(ctx context.Context, tenantID int64) Presentation {
	if s.settings == nil {
		return Presentation{}
	}
	scope := tenancy.Scope{TenantID: tenantID}
	text := func(name string) *string {
		value, err := s.settings.Get(ctx, scope, name)
		if err != nil {
			return nil
		}
		if v, ok := value.(string); ok && strings.TrimSpace(v) != "" {
			trimmed := strings.TrimSpace(v)
			return &trimmed
		}
		return nil
	}
	return Presentation{
		LogoURL:  text("branding.logo_url"),
		Color:    text("branding.color"),
		Language: text("general.default_locale"),
	}
}
