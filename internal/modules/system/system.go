package system

import (
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/i18n"
)

// Service provides system-module business behavior. It is the template for
// the handler â†’ service â†’ response-DTO pattern.
type Service struct {
	name      string
	env       string
	version   string
	timezone  string
	startedAt time.Time
	languages func() []i18n.Language
	locale    string
}

// NewService creates the system service.
func NewService(name, env, buildVersion, timezone string) *Service {
	return &Service{
		name:      name,
		env:       env,
		version:   buildVersion,
		timezone:  timezone,
		startedAt: time.Now(),
		languages: func() []i18n.Language { return i18n.Default().Supported() },
		locale:    i18n.Reference,
	}
}

// WithLanguages sets where the offered languages come from and the default
// language (APP_LOCALE).
func (s *Service) WithLanguages(languages func() []i18n.Language, defaultCode string) *Service {
	s.languages, s.locale = languages, defaultCode
	return s
}

// LanguagesResponse lists the languages a client may offer.
type LanguagesResponse struct {
	// Default is the language used when nothing else is chosen.
	Default   string          `json:"default"`
	Languages []i18n.Language `json:"languages"`
}

// Languages returns the offered languages, the reference first.
func (s *Service) Languages() LanguagesResponse {
	return LanguagesResponse{Default: s.locale, Languages: s.languages()}
}

// InfoResponse is the public representation of the system status.
type InfoResponse struct {
	Name          string    `json:"name"`
	Version       string    `json:"version"`
	Environment   string    `json:"environment"`
	Timezone      string    `json:"timezone"`
	GoVersion     string    `json:"go_version"`
	StartedAt     time.Time `json:"started_at"`
	UptimeSeconds int64     `json:"uptime_seconds"`
}

// Info returns the current system information.
func (s *Service) Info() InfoResponse {
	return InfoResponse{
		Name:          s.name,
		Version:       s.version,
		Environment:   s.env,
		Timezone:      s.timezone,
		GoVersion:     runtime.Version(),
		StartedAt:     s.startedAt,
		UptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
	}
}

// EchoRequest demonstrates transport validation.
type EchoRequest struct {
	Message string `json:"message" validate:"required,min=2,max=255"`
}

// EchoResponse is the echoed payload.
type EchoResponse struct {
	Message string `json:"message"`
	Length  int    `json:"length"`
}

// Echo trims and returns the message with its length.
func (s *Service) Echo(message string) EchoResponse {
	trimmed := strings.TrimSpace(message)
	return EchoResponse{Message: trimmed, Length: len(trimmed)}
}

// Module wires the system routes.
type Module struct {
	service *Service
	httpx.Kit
}

// NewModule creates the system module.
func NewModule(service *Service, kit httpx.Kit) *Module {
	return &Module{service: service, Kit: kit}
}

func (m *Module) info(w http.ResponseWriter, r *http.Request) {
	m.Data(w, r, http.StatusOK, m.service.Info())
}

func (m *Module) languages(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	m.Data(w, r, http.StatusOK, m.service.Languages())
}

func (m *Module) echo(w http.ResponseWriter, r *http.Request) {
	var req EchoRequest
	if !m.Bind(w, r, &req) {
		return
	}
	m.Data(w, r, http.StatusOK, m.service.Echo(req.Message))
}
