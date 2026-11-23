package onboarding

import (
	"context"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/access"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/billing/domain"
	billingservice "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/billing/service"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/branches"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/identity"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/tenants"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// Documents accepted at sign-up.
const (
	DocumentTerms   = "terms_of_service"
	DocumentPrivacy = "privacy_policy"
)

// TemplateAccountExists is mailed when someone signs up with an address that
// already has an account: the response never says so, the mailbox owner does.
const TemplateAccountExists = "signup-account-exists"

// Steps of the first-run setup the client may report as skipped.
var knownSteps = map[string]struct{}{
	"organization": {}, "package": {}, "invite": {}, "two_factor": {},
}

// Collaborators (narrow interfaces over other modules' services).
type (
	// TenantProvisioner creates (with its default branch and roles) and
	// removes tenants.
	TenantProvisioner interface {
		Create(ctx context.Context, actorID int64, input tenants.CreateInput) (*tenants.Tenant, error)
		Delete(ctx context.Context, actorID, id int64) error
	}
	// PlanAssigner starts a tenant's subscription.
	PlanAssigner interface {
		Assign(ctx context.Context, input billingservice.AssignInput) (*domain.Subscription, *domain.Payment, error)
	}
	// PackageCatalog lists packages.
	PackageCatalog interface {
		ListPublic(ctx context.Context) ([]domain.Package, error)
		Get(ctx context.Context, id int64) (*domain.Package, error)
	}
	// UserCreator creates users through the identity service rules.
	UserCreator interface {
		Create(ctx context.Context, scope tenancy.Scope, actorID int64, input identity.CreateParams) (*identity.User, error)
	}
	// AccountLookup answers whether an address or phone is taken.
	AccountLookup interface {
		EmailExists(ctx context.Context, email string, excludeID int64) (bool, error)
		PhoneExists(ctx context.Context, phone string, excludeID int64) (bool, error)
		GetByEmail(ctx context.Context, email string) (*identity.User, error)
	}
	// RoleFinder finds a tenant's role by name.
	RoleFinder interface {
		GetByName(ctx context.Context, tenantID int64, name string) (*access.Role, error)
	}
	// BranchLister lists a tenant's branches.
	BranchLister interface {
		List(ctx context.Context, filter branches.ListFilter) ([]branches.Branch, error)
	}
	// SettingsReader reads typed settings.
	SettingsReader interface {
		Get(ctx context.Context, scope tenancy.Scope, name string) (any, error)
		GetInt(ctx context.Context, scope tenancy.Scope, name string) (int, error)
		GetBool(ctx context.Context, scope tenancy.Scope, name string) (bool, error)
	}
	// Mailer sends a templated message.
	Mailer interface {
		Send(ctx context.Context, message delivery.Message) error
	}
)

// Deps are the service's collaborators.
type Deps struct {
	Repo     Repository
	Tenants  TenantProvisioner
	Plans    PlanAssigner
	Packages PackageCatalog
	Users    UserCreator
	Accounts AccountLookup
	Roles    RoleFinder
	Branches BranchLister
	Settings SettingsReader
	Mailer   Mailer
	// ClientURL is the client app's origin (login and password-reset links).
	ClientURL string
	Logger    *slog.Logger
}

// Service implements sign-up and first-run setup.
type Service struct {
	Deps
	activity *activity.Recorder
	now      func() time.Time
}

// NewService creates the onboarding service.
func NewService(deps Deps) *Service {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &Service{Deps: deps, now: time.Now}
}

// WithActivity records sign-ups and completed setups in the activity log.
func (s *Service) WithActivity(recorder *activity.Recorder) *Service {
	s.activity = recorder
	return s
}

// SignUpInput is one self-service sign-up.
type SignUpInput struct {
	Name             string
	Email            string
	Password         string
	OrganizationName string
	Phone            *string
	Locale           string
	AcceptTerms      bool
	IPAddress        string
	UserAgent        string
}

// SignUp creates an organization (tenant, default branch, roles), starts it
// on the sign-up package and creates its owner as an unverified Admin who is
// mailed a verification link. An address that already has an account gets a
// mail instead and the answer is the same, so sign-up never reveals whether
// an account exists.
func (s *Service) SignUp(ctx context.Context, input SignUpInput) error {
	input.Email = strings.TrimSpace(strings.ToLower(input.Email))
	input.Name = strings.TrimSpace(input.Name)
	input.OrganizationName = strings.TrimSpace(input.OrganizationName)
	if input.Phone != nil {
		phone := strings.TrimSpace(*input.Phone)
		input.Phone = &phone
		if phone == "" {
			input.Phone = nil
		}
	}

	if enabled, err := s.Settings.GetBool(ctx, tenancy.Scope{}, "signup.enabled"); err != nil {
		return err
	} else if !enabled {
		return apperr.New(apperr.Forbidden, "Sign-up is closed. Contact us to open an organization.")
	}
	if !input.AcceptTerms {
		return apperr.Validation(map[string][]string{"accept_terms": {"You must accept the terms of service and the privacy policy."}})
	}
	// Everything that can be refused is checked before anything is created.
	if err := authn.ValidateNewPassword(input.Password); err != nil {
		return err
	}
	exists, err := s.Accounts.EmailExists(ctx, input.Email, 0)
	if err != nil {
		return err
	}
	if exists {
		s.mailExistingAccount(ctx, input)
		return nil
	}
	if input.Phone != nil {
		taken, err := s.Accounts.PhoneExists(ctx, *input.Phone, 0)
		if err != nil {
			return err
		}
		if taken {
			return apperr.Validation(map[string][]string{"phone": {"This phone number is already in use."}})
		}
	}
	pkg, err := s.signupPackage(ctx)
	if err != nil {
		return err
	}

	tenant, err := s.Tenants.Create(ctx, 0, tenants.CreateInput{
		Name: input.OrganizationName, Email: &input.Email, Phone: input.Phone, IsActive: true,
	})
	if err != nil {
		return err
	}
	// The services below are not one transaction: if a later step fails, the
	// half-made organization is removed again (soft delete).
	if err := s.provision(ctx, tenant, pkg, input); err != nil {
		if cleanupErr := s.Tenants.Delete(ctx, 0, tenant.ID); cleanupErr != nil {
			s.Logger.Error("sign-up: remove half-created organization", "tenant_id", tenant.ID, "error", cleanupErr)
		}
		return err
	}
	return nil
}

func (s *Service) provision(ctx context.Context, tenant *tenants.Tenant, pkg *domain.Package, input SignUpInput) error {
	// The plan comes first: creating a user checks the plan's user limit.
	if _, _, err := s.Plans.Assign(ctx, billingservice.AssignInput{TenantID: tenant.ID, PackageID: pkg.ID}); err != nil {
		return err
	}
	admin, err := s.Roles.GetByName(ctx, tenant.ID, authz.AdminRole)
	if err != nil {
		return err
	}
	branchList, err := s.Branches.List(ctx, branches.ListFilter{TenantID: tenant.ID, OrderBy: "id", OrderDir: "asc", Limit: 1})
	if err != nil {
		return err
	}
	if len(branchList) == 0 {
		return apperr.New(apperr.InternalServerError, "The new organization has no branch.")
	}
	branchID := branchList[0].ID

	owner, err := s.Users.Create(ctx, tenancy.Scope{TenantID: tenant.ID}, 0, identity.CreateParams{
		Name: input.Name, Email: input.Email, Password: input.Password, Phone: input.Phone,
		BranchID: &branchID, Roles: []int64{admin.ID}, RequireVerification: true,
	})
	if err != nil {
		return err
	}
	if _, err := s.Repo.CreateProgress(ctx, tenant.ID, &owner.ID, "signup"); err != nil {
		return err
	}
	version := s.termsVersion(ctx)
	for _, document := range []string{DocumentTerms, DocumentPrivacy} {
		if _, err := s.Repo.RecordAgreement(ctx, Agreement{
			UserID: owner.ID, TenantID: &tenant.ID, Document: document, Version: version,
			IPAddress: optional(input.IPAddress), UserAgent: optional(input.UserAgent),
		}); err != nil {
			return err
		}
	}
	s.activity.Record(ctx, activity.Entry{
		TenantID: tenant.ID, ActorID: owner.ID, Action: "tenant.signed_up",
		Subject: "tenant", SubjectID: tenant.ID,
		Changes: map[string]any{"name": tenant.Name, "package_id": pkg.ID, "terms_version": version},
	})
	return nil
}

// signupPackage is the configured package, or the first active free one.
func (s *Service) signupPackage(ctx context.Context) (*domain.Package, error) {
	unavailable := apperr.New(apperr.ServiceUnavailable, "Sign-up is not available right now. Please try again later.")
	id, err := s.Settings.GetInt(ctx, tenancy.Scope{}, "signup.package_id")
	if err != nil {
		return nil, err
	}
	if id > 0 {
		pkg, err := s.Packages.Get(ctx, int64(id))
		if err != nil {
			s.Logger.Error("sign-up: signup.package_id points at a missing package", "package_id", id, "error", err)
			return nil, unavailable
		}
		return pkg, nil
	}
	active, err := s.Packages.ListPublic(ctx)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(active, func(i, j int) bool { return active[i].SortOrder < active[j].SortOrder })
	for i := range active {
		if active[i].Price == 0 {
			return &active[i], nil
		}
	}
	s.Logger.Warn("sign-up refused: no active free package and signup.package_id is 0")
	return nil, unavailable
}

func (s *Service) termsVersion(ctx context.Context) string {
	value, err := s.Settings.Get(ctx, tenancy.Scope{}, "signup.terms_version")
	if version, ok := value.(string); err == nil && ok && strings.TrimSpace(version) != "" {
		return strings.TrimSpace(version)
	}
	return "1"
}

// mailExistingAccount tells the owner of an address that someone tried to
// sign up with it (best effort; the caller gets the usual answer).
func (s *Service) mailExistingAccount(ctx context.Context, input SignUpInput) {
	if s.Mailer == nil {
		return
	}
	user, err := s.Accounts.GetByEmail(ctx, input.Email)
	if err != nil {
		return
	}
	base := strings.TrimRight(s.ClientURL, "/")
	err = s.Mailer.Send(ctx, delivery.Message{
		To:       delivery.Recipient{UserID: user.ID, TenantID: deref(user.TenantID), Name: user.Name, Email: user.Email, Locale: input.Locale},
		Template: TemplateAccountExists,
		Data: map[string]any{
			"login_url":  base + "/login",
			"reset_url":  base + "/forgot-password",
			"ip_address": input.IPAddress,
		},
	})
	if err != nil {
		s.Logger.Warn("sign-up: mail to existing account failed", "error", err)
	}
}

// Progress returns the caller's organization's first-run setup.
func (s *Service) Progress(ctx context.Context, scope tenancy.Scope) (*Progress, error) {
	tenantID := scope.EffectiveTenantID()
	if tenantID == 0 {
		return nil, apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	return s.Repo.GetProgress(ctx, tenantID)
}

// Complete marks the first-run setup done (only the owner finishes it).
func (s *Service) Complete(ctx context.Context, scope tenancy.Scope, actorID int64, skipped []string) (*Progress, error) {
	progress, err := s.Progress(ctx, scope)
	if err != nil {
		return nil, err
	}
	if progress.OwnerUserID == nil || *progress.OwnerUserID != actorID {
		return nil, apperr.New(apperr.Forbidden, "Only the organization's owner finishes its setup.")
	}
	clean := make([]string, 0, len(skipped))
	seen := map[string]struct{}{}
	for _, step := range skipped {
		if _, ok := knownSteps[step]; !ok {
			return nil, apperr.Validation(map[string][]string{"skipped_steps": {"Unknown setup step: " + step + "."}})
		}
		if _, dup := seen[step]; !dup {
			seen[step] = struct{}{}
			clean = append(clean, step)
		}
	}
	done, err := s.Repo.CompleteProgress(ctx, progress.TenantID, clean)
	if err != nil {
		return nil, err
	}
	if progress.CompletedAt == nil {
		s.activity.Record(ctx, activity.Entry{
			TenantID: progress.TenantID, ActorID: actorID, Action: "tenant.onboarding_completed",
			Subject: "tenant", SubjectID: progress.TenantID, Changes: map[string]any{"skipped_steps": clean},
		})
	}
	return done, nil
}

// ClientOrigin returns scheme://host of a URL (the client app's origin taken
// from one of its configured link templates), or "" when it does not parse.
func ClientOrigin(link string) string {
	parsed, err := url.Parse(strings.TrimSpace(link))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func optional(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if len(value) > 500 {
		value = value[:500]
	}
	return &value
}

func deref(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
