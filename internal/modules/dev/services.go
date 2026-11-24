package dev

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/access"
	filesmod "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/files"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/identity"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// The dashboard's service layer. Handlers parse the request, call one of
// these, and write the response; rules ("you cannot remove your own
// Developer role") and the audit trail live here, so they hold no matter
// which handler (or test) calls them. Read-only panels embed their
// repository: a list is a list, a wrapper would add nothing.

// ── audit ─────────────────────────────────────────────────────────────────

// Auditor writes the dashboard audit log (dev_actions). Writes are detached
// from the request, so a client that disconnects mid-action never loses the
// record of what it changed (M54).
type Auditor struct {
	actions ActionRepository
}

// NewAuditor creates the audit writer (nil-safe: a nil repository records
// nothing).
func NewAuditor(actions ActionRepository) *Auditor {
	return &Auditor{actions: actions}
}

// Record stores one audited mutation and how it ended.
func (a *Auditor) Record(ctx context.Context, actorID int64, action, target string, params any, started time.Time, opErr error) {
	if a == nil || a.actions == nil {
		return
	}
	payload, err := json.Marshal(params)
	if err != nil || len(payload) == 0 || string(payload) == "null" {
		payload = json.RawMessage("{}")
	}
	status := actionStatusOK
	var message *string
	if opErr != nil {
		status = "error"
		value := opErr.Error()
		message = &value
	}
	var targetRef *string
	if target != "" {
		targetRef = &target
	}
	_, _ = a.actions.Insert(context.WithoutCancel(ctx), InsertActionInput{
		ActorUserID:  actorID,
		Action:       action,
		Target:       targetRef,
		Params:       payload,
		Status:       status,
		ErrorMessage: message,
		DurationMS:   int32(max(time.Since(started), 0).Milliseconds()),
		IP:           optionalText(httpx.ClientIP(ctx)),
		UserAgent:    optionalText(httpx.UserAgent(ctx)),
	})
}

// SignIn records a dashboard sign-in attempt (auth.login). Failed attempts
// keep the e-mail that was tried, never the password.
func (a *Auditor) SignIn(ctx context.Context, email string, userID int64, err error) {
	a.Record(ctx, userID, "auth.login", email, map[string]any{"email": email, "method": "password"}, time.Now(), err)
}

// SignOut records a dashboard sign-out (auth.logout).
func (a *Auditor) SignOut(ctx context.Context, userID int64) {
	a.Record(ctx, userID, "auth.logout", "", map[string]any{}, time.Now(), nil)
}

// Search filters the audit log; it also returns the matching total.
func (a *Auditor) Search(ctx context.Context, filter ActionFilter) ([]Action, int64, error) {
	return a.actions.Search(ctx, filter)
}

// Get returns one audit row.
func (a *Auditor) Get(ctx context.Context, id int64) (*Action, error) { return a.actions.Get(ctx, id) }

// Facets lists the actions and people that appear in the log (filter menus).
func (a *Auditor) Facets(ctx context.Context) ([]ActionKind, []ActionActor, error) {
	kinds, err := a.actions.Kinds(ctx)
	if err != nil {
		return nil, nil, err
	}
	actors, err := a.actions.Actors(ctx)
	return kinds, actors, err
}

// Stats is the header: actions and failed sign-ins in the last day.
func (a *Auditor) Stats(ctx context.Context, retentionDays int) (ActionStats, error) {
	stats, err := a.actions.Stats(ctx)
	stats.RetentionDays = retentionDays
	return stats, err
}

// List returns the audit log, newest first.
func (a *Auditor) List(ctx context.Context, beforeID int64, limit int32) ([]Action, error) {
	return a.actions.List(ctx, beforeID, limit)
}

// ── telemetry ─────────────────────────────────────────────────────────────

// TelemetryService serves the Telescope panels (requests, logs, exceptions).
type TelemetryService struct {
	TelemetryRepository
	audit         *Auditor
	retentionDays int
}

// Prune deletes telemetry older than days (0 = the configured retention).
func (s *TelemetryService) Prune(ctx context.Context, actorID int64, days int) (int, map[string]int64, error) {
	if days == 0 {
		days = s.retentionDays
	}
	started := time.Now()
	deleted, err := s.TelemetryRepository.Prune(ctx, days)
	s.audit.Record(ctx, actorID, "telemetry.prune", "", map[string]any{"retention_days": days}, started, err)
	return days, deleted, err
}

// ── sessions ──────────────────────────────────────────────────────────────

// SessionService manages API tokens: who is signed in, force logout, pruning.
type SessionService struct {
	repo  SessionRepository
	audit *Auditor
}

// Stats summarises active tokens.
func (s *SessionService) Stats(ctx context.Context) (SessionStats, error) { return s.repo.Stats(ctx) }

// Users lists users that hold tokens.
func (s *SessionService) Users(ctx context.Context, filter SessionUserFilter) ([]SessionUser, error) {
	return s.repo.ListUsers(ctx, filter)
}

// Tokens lists a user's tokens; currentTokenID is flagged as the caller's.
func (s *SessionService) Tokens(ctx context.Context, userID, currentTokenID int64) ([]SessionToken, error) {
	if err := s.requireUser(ctx, userID); err != nil {
		return nil, err
	}
	tokens, err := s.repo.ListTokens(ctx, userID)
	for i := range tokens {
		tokens[i].Current = tokens[i].ID == currentTokenID
	}
	return tokens, err
}

// RevokeToken signs one device out.
func (s *SessionService) RevokeToken(ctx context.Context, actorID, tokenID int64) error {
	started := time.Now()
	deleted, err := s.repo.DeleteToken(ctx, tokenID)
	if err == nil && !deleted {
		err = apperr.Newf(apperr.ResourceNotFound, "Session token with ID '%d' was not found.", tokenID)
	}
	s.audit.Record(ctx, actorID, "sessions.token.revoke", formatTarget("token", tokenID),
		map[string]any{"token_id": tokenID}, started, err)
	return err
}

// RevokeUser signs a user out everywhere.
func (s *SessionService) RevokeUser(ctx context.Context, actorID, userID int64) (int64, error) {
	if err := s.requireUser(ctx, userID); err != nil {
		return 0, err
	}
	started := time.Now()
	revoked, err := s.repo.DeleteUserTokens(ctx, userID)
	s.audit.Record(ctx, actorID, "sessions.user.revoke", formatTarget("user", userID),
		map[string]any{"user_id": userID}, started, err)
	return revoked, err
}

// RevokeAllExcept signs everyone out except the caller's own token.
func (s *SessionService) RevokeAllExcept(ctx context.Context, actorID, keepTokenID int64) (int64, error) {
	if keepTokenID == 0 {
		return 0, apperr.New(apperr.Unauthorized, "Authentication is required.")
	}
	started := time.Now()
	revoked, err := s.repo.DeleteAllExcept(ctx, keepTokenID)
	s.audit.Record(ctx, actorID, "sessions.logout_all", "sessions",
		map[string]any{"excluded_token_id": keepTokenID, "revoked": revoked}, started, err)
	return revoked, err
}

// Prune removes tokens idle for idleDays (default 30); dryRun only counts.
func (s *SessionService) Prune(ctx context.Context, actorID int64, idleDays int32, dryRun bool) (SessionPruneResult, error) {
	if idleDays == 0 {
		idleDays = 30
	}
	started := time.Now()
	affected, err := s.repo.Prune(ctx, idleDays, dryRun)
	s.audit.Record(ctx, actorID, "sessions.prune", "sessions",
		map[string]any{"idle_days": idleDays, "dry_run": dryRun, "affected": affected}, started, err)
	return SessionPruneResult{IdleDays: idleDays, DryRun: dryRun, Affected: affected}, err
}

func (s *SessionService) requireUser(ctx context.Context, userID int64) error {
	exists, err := s.repo.UserExists(ctx, userID)
	if err != nil {
		return err
	}
	if !exists {
		return apperr.Newf(apperr.ResourceNotFound, "User with ID '%d' was not found.", userID)
	}
	return nil
}

// ── developers ────────────────────────────────────────────────────────────

// DeveloperService manages accounts holding the hidden Developer role.
type DeveloperService struct {
	repo        DeveloperRepository
	roles       access.RoleRepository
	roleService *access.RoleService
	identity    *identity.Service
	audit       *Auditor
}

// NewDeveloperInput is a new developer account.
type NewDeveloperInput struct {
	Name     string
	Email    string
	Password string
	IsActive bool
}

// List returns developers; actorID's own row is flagged.
func (s *DeveloperService) List(ctx context.Context, filter DeveloperFilter, actorID int64) ([]Developer, int64, error) {
	items, total, err := s.repo.List(ctx, filter)
	for i := range items {
		items[i].IsSelf = items[i].UserID == actorID
	}
	return items, total, err
}

// Stats reports developer count and the role's state.
func (s *DeveloperService) Stats(ctx context.Context) (DeveloperStats, error) {
	developers, err := s.repo.Count(ctx)
	if err != nil {
		return DeveloperStats{}, err
	}
	stats := DeveloperStats{
		Developers:       developers,
		RoleName:         authz.DeveloperRole,
		TotalPermissions: int64(len(authz.DeveloperPermissions())),
	}
	role, err := s.role(ctx)
	if err != nil || role == nil {
		return stats, err
	}
	stats.RoleExists = true
	roleID := role.ID
	stats.RoleID = &roleID
	stats.RolePermissions, err = s.roles.CountPermissions(ctx, role.ID)
	return stats, err
}

// Create adds a tenantless account with the Developer role.
func (s *DeveloperService) Create(ctx context.Context, scope tenancy.Scope, actorID int64, input NewDeveloperInput) (*Developer, error) {
	role, err := s.role(ctx)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, apperr.New(apperr.Conflict, "The Developer role is missing. Create the role from the Developers panel first.")
	}
	started := time.Now()
	isActive := input.IsActive
	created, err := s.identity.Create(ctx, scope, actorID, identity.CreateParams{
		Name: input.Name, Email: input.Email, Password: input.Password, IsActive: &isActive, Roles: []int64{role.ID},
	})
	target := input.Email
	if created != nil {
		target = formatTarget("user", created.ID)
	}
	s.audit.Record(ctx, actorID, "developers.create", target,
		map[string]any{"name": input.Name, "email": input.Email}, started, err)
	if err != nil {
		return nil, err
	}
	developer := developerFromUser(created, actorID)
	return &developer, nil
}

// EnsureRole creates the Developer role (and its permissions) if missing.
func (s *DeveloperService) EnsureRole(ctx context.Context, actorID int64) (DeveloperStats, error) {
	started := time.Now()
	role, assigned, err := s.roleService.EnsureDeveloperRole(ctx, actorID)
	target := authz.DeveloperRole
	if role != nil {
		target = formatTarget("role", role.ID)
	}
	s.audit.Record(ctx, actorID, "developers.role.ensure", target,
		map[string]any{"role": authz.DeveloperRole, "permissions": assigned}, started, err)
	if err != nil {
		return DeveloperStats{}, err
	}
	return s.Stats(ctx)
}

// Remove takes the Developer role away from a user (never from yourself).
func (s *DeveloperService) Remove(ctx context.Context, scope tenancy.Scope, actorID, userID int64) error {
	if userID == actorID {
		return apperr.New(apperr.Forbidden, "You cannot remove your own Developer role.")
	}
	role, err := s.role(ctx)
	if err != nil {
		return err
	}
	if role == nil {
		return apperr.New(apperr.ResourceNotFound, "The Developer role does not exist.")
	}
	started := time.Now()
	_, err = s.identity.RevokeRole(ctx, scope, userID, role.ID)
	s.audit.Record(ctx, actorID, "developers.unassign", formatTarget("user", userID),
		map[string]any{"user_id": userID, "role": authz.DeveloperRole}, started, err)
	return err
}

func (s *DeveloperService) role(ctx context.Context) (*access.Role, error) {
	role, err := s.roles.GetSystemByName(ctx, authz.DeveloperRole)
	if apperr.Is(err, apperr.ResourceNotFound) {
		return nil, nil
	}
	return role, err
}

// ── files ─────────────────────────────────────────────────────────────────

// FileBrowser is the cross-tenant file manager behind the Files panel.
type FileBrowser struct {
	FileRepository
	files *filesmod.Service
	audit *Auditor
}

// URL is the public URL of a file ("" for private disks).
func (s *FileBrowser) URL(ctx context.Context, item FileItem) string {
	return s.files.URL(ctx, filesDomain(item))
}

// Open streams a file's content (any tenant: the dashboard is a platform
// surface).
func (s *FileBrowser) Open(ctx context.Context, id int64) (io.ReadCloser, *filesmod.File, error) {
	return s.files.Open(ctx, id)
}

// Delete removes a file and records what was removed.
func (s *FileBrowser) Delete(ctx context.Context, actorID, id int64) error {
	started := time.Now()
	file, err := s.files.Get(ctx, id, true, 0)
	if err == nil {
		err = s.files.Delete(ctx, id, true, 0)
	}
	params := map[string]any{"file_id": id}
	if file != nil {
		params["name"], params["disk"], params["path"], params["size"] = file.Name, file.Disk, file.Path, file.Size
	}
	s.audit.Record(ctx, actorID, "files.delete", formatTarget("file", id), params, started, err)
	return err
}

// ── targets ───────────────────────────────────────────────────────────────

// TargetService searches tenants and users for the flags/settings panels.
type TargetService struct {
	TargetRepository
}
