// Package activity is the tenant-scoped activity log ("who did what"), like
// spatie/laravel-activitylog. Services call Record after a change succeeds;
// tenant admins read their tenant's feed at GET /api/v1/activity.
//
// Rows with no tenant are the system audit log (internal users and roles,
// system settings, act-as). Only activity.view.all holders read them.
package activity

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/maintenance"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/telemetry"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

var (
	_ tenancy.ImpersonationAudit = (*Recorder)(nil)
	_ authn.LoginAudit           = (*Recorder)(nil)
	_ maintenance.Audit          = (*Recorder)(nil)
	_ authn.SessionAudit         = (*Recorder)(nil)
	_ authn.TwoFactorAudit       = (*Recorder)(nil)
	_ authn.InvitationAudit      = (*Recorder)(nil)
)

// Entry is one thing that happened.
//
//	recorder.Record(ctx, activity.Entry{
//		TenantID: user.TenantID, Action: "user.created",
//		Subject: "user", SubjectID: user.ID,
//		Changes: map[string]any{"name": user.Name, "email": user.Email},
//	})
type Entry struct {
	TenantID  int64          // 0 = a platform-level action (no tenant)
	ActorID   int64          // 0 = the authenticated user in ctx (or the system)
	Action    string         // "<subject>.<verb>", e.g. "user.created"
	Subject   string         // "user", "role", "branch", ...
	SubjectID int64          // 0 when there is no single row
	Changes   map[string]any // new values; secrets are redacted before storing
}

// Action names for act-as (phase 21). They are system rows: tenant users never
// see them.
const (
	ActionImpersonationStarted = "impersonation.started"
	ActionImpersonationRequest = "impersonation.request"
	ActionImpersonationDenied  = "impersonation.denied"
)

// ActionLogin is an API login attempt (password, OTP or social; phase 23).
const ActionLogin = "auth.login"

// ActionSessionRevoked is a user signing their own sessions out (phase 26.3).
const ActionSessionRevoked = "auth.session_revoked"

// Action names for maintenance mode (phase 24). They are system rows.
const (
	ActionMaintenanceDown = "maintenance.down"
	ActionMaintenanceUp   = "maintenance.up"
)

// Action names for suspending a tenant and bringing it back (phase 26). They
// are system rows: the platform acted on the tenant. A suspension made by
// billing (BILLING_SUSPEND_ON_EXPIRY) carries "reason": "subscription_expired".
const (
	ActionTenantSuspended   = "tenant.suspended"
	ActionTenantReactivated = "tenant.reactivated"
)

// MCPChangesKey holds, in an entry's changes, the MCP tool call that made
// the change: {"source": "mcp", "tool": …, "client": …}.
const MCPChangesKey = "_via"

// Recorded is an entry as stored: the changes redacted, the actor resolved.
// Sinks receive it after the row is written.
type Recorded struct {
	TenantID  int64
	ActorID   int64
	Action    string
	Subject   string
	SubjectID int64
	Changes   json.RawMessage
	At        time.Time
}

// Sink hears about every stored entry (outbound webhooks, phase 26.8). It
// must not block or fail: the change it reports already happened.
type Sink interface {
	Recorded(ctx context.Context, entry Recorded)
}

// Recorder writes activity entries. A nil *Recorder records nothing, so a
// service works without one (unit tests, tools that do not need the log).
type Recorder struct {
	queries  *dbgen.Queries
	redactor *telemetry.Redactor
	logger   *slog.Logger
	sink     Sink
}

// NewRecorder creates the activity recorder.
func NewRecorder(queries *dbgen.Queries, redactor *telemetry.Redactor, logger *slog.Logger) *Recorder {
	return &Recorder{queries: queries, redactor: redactor, logger: logger}
}

// WithSink passes every stored entry on (outbound webhooks).
func (r *Recorder) WithSink(sink Sink) *Recorder {
	r.sink = sink
	return r
}

// Record stores an entry. It never fails the caller: the change already
// happened, so a logging problem is reported in the application log instead.
// The IP and request id come from the request context when there is one.
//
// While someone acts as a user, the entry is marked with that person
// (impersonator_id and a name snapshot); the actor stays the user.
func (r *Recorder) Record(ctx context.Context, entry Entry) {
	if r == nil {
		return
	}
	var impersonatorID int64
	var impersonatorName string
	if identity, ok := authn.IdentityFromContext(ctx); ok && identity.Impersonated() {
		impersonatorID, impersonatorName = identity.ImpersonatorID, identity.ImpersonatorName
	}
	stored, err := r.insert(ctx, entry, impersonatorID, impersonatorName)
	if err != nil {
		if r.logger != nil {
			r.logger.WarnContext(ctx, "activity log write failed", "action", entry.Action, "error", err)
		}
		return
	}
	if r.sink != nil {
		r.sink.Recorded(context.WithoutCancel(ctx), stored)
	}
}

func (r *Recorder) insert(ctx context.Context, entry Entry, impersonatorID int64, impersonatorName string) (Recorded, error) {
	actorID := entry.ActorID
	if actorID == 0 {
		actorID = authn.UserIDFromContext(ctx)
	}
	values := entry.Changes
	// A change an AI agent made through an MCP tool says so: which tool,
	// which client (phase 22).
	if call, ok := httpx.MCPCallFrom(ctx); ok {
		values = make(map[string]any, len(entry.Changes)+1)
		for k, v := range entry.Changes {
			values[k] = v
		}
		values[MCPChangesKey] = map[string]any{"source": "mcp", "tool": call.Tool, "client": call.Client}
	}
	changes := []byte("{}")
	if len(values) > 0 {
		if encoded := r.redactor.AnyJSON(values); encoded != nil {
			changes = encoded
		}
	}
	stored := Recorded{
		TenantID: entry.TenantID, ActorID: actorID, Action: entry.Action,
		Subject: entry.Subject, SubjectID: entry.SubjectID, At: time.Now().UTC(),
	}
	// Sinks get the entry's own changes, redacted (not the MCP marker).
	stored.Changes = json.RawMessage("{}")
	if len(entry.Changes) > 0 {
		if encoded := r.redactor.AnyJSON(entry.Changes); encoded != nil {
			stored.Changes = encoded
		}
	}
	// The request may already be finishing; the insert must not be cancelled
	// with it.
	return stored, r.queries.InsertActivity(context.WithoutCancel(ctx), dbgen.InsertActivityParams{
		TenantID:         optional(entry.TenantID),
		ActorID:          optional(actorID),
		ImpersonatorID:   optional(impersonatorID),
		ImpersonatorName: optionalString(impersonatorName),
		Action:           entry.Action,
		SubjectType:      entry.Subject,
		SubjectID:        optional(entry.SubjectID),
		Changes:          changes,
		Ip:               optionalString(httpx.ClientIP(ctx)),
		RequestID:        optionalString(httpx.RequestIDFromContext(ctx)),
	})
}

// The methods below implement tenancy.ImpersonationAudit. Act-as rows are
// system rows (no tenant) written as the impersonator; the target is the
// subject, and the target's tenant and branch are in changes.

// ImpersonationStarted records the start of acting as a user. Its error is
// returned, so tenancy refuses the request when the row is not stored.
func (r *Recorder) ImpersonationStarted(ctx context.Context, event tenancy.Impersonation) error {
	if r == nil {
		return nil
	}
	_, err := r.insert(ctx, impersonationEntry(ActionImpersonationStarted, event, map[string]any{
		"target_name": event.TargetName,
		"tenant_id":   event.TenantID,
		"branch_id":   event.BranchID,
	}), event.ImpersonatorID, event.ImpersonatorName)
	if err != nil && r.logger != nil {
		r.logger.ErrorContext(ctx, "impersonation start not recorded", "impersonator_id", event.ImpersonatorID,
			"target_id", event.TargetID, "error", err)
	}
	return err
}

// ImpersonationRequested records one request made while acting as a user.
func (r *Recorder) ImpersonationRequested(ctx context.Context, event tenancy.ImpersonationRequest) {
	if r == nil {
		return
	}
	_, err := r.insert(ctx, impersonationEntry(ActionImpersonationRequest, event.Impersonation, map[string]any{
		"method":    event.Method,
		"route":     event.Route,
		"status":    event.Status,
		"tenant_id": event.TenantID,
		"branch_id": event.BranchID,
	}), event.ImpersonatorID, event.ImpersonatorName)
	if err != nil && r.logger != nil {
		r.logger.ErrorContext(ctx, "impersonation request not recorded", "impersonator_id", event.ImpersonatorID,
			"target_id", event.TargetID, "error", err)
	}
}

// ImpersonationDenied records a refused act-as attempt.
func (r *Recorder) ImpersonationDenied(ctx context.Context, callerID, targetID int64, reason string) {
	if r == nil {
		return
	}
	entry := Entry{
		ActorID: callerID, Action: ActionImpersonationDenied, Subject: "user", SubjectID: targetID,
		Changes: map[string]any{"reason": reason},
	}
	if _, err := r.insert(ctx, entry, 0, ""); err != nil && r.logger != nil {
		r.logger.WarnContext(ctx, "activity log write failed", "action", entry.Action, "error", err)
	}
}

// Login implements authn.LoginAudit. A successful login is a row in the
// user's tenant, so tenant admins see their users sign in; a failed one is a
// system row, since the attempt may not belong to any account.
func (r *Recorder) Login(ctx context.Context, event authn.LoginEvent) {
	if r == nil {
		return
	}
	entry := Entry{Action: ActionLogin, Subject: "user", Changes: map[string]any{
		"method": event.Method,
		"email":  event.Email,
		"device": event.Device,
		"status": "ok",
	}}
	if event.Err != nil {
		entry.Changes["status"] = "failed"
		entry.Changes["error"] = string(apperr.CodeOf(event.Err))
	} else {
		entry.TenantID, entry.ActorID, entry.SubjectID = event.User.TenantID, event.User.ID, event.User.ID
	}
	r.Record(ctx, entry)
}

// SessionsRevoked implements authn.SessionAudit: a tenant row (the user's
// tenant) naming the devices signed out; "others" marks "sign out
// everywhere else".
func (r *Recorder) SessionsRevoked(ctx context.Context, user *authn.Identity, devices []string, others bool) {
	if r == nil || user == nil {
		return
	}
	r.Record(ctx, Entry{
		TenantID: user.TenantID, ActorID: user.UserID, Action: ActionSessionRevoked,
		Subject: "user", SubjectID: user.UserID,
		Changes: map[string]any{"devices": devices, "others": others},
	})
}

// TwoFactorChanged implements authn.TwoFactorAudit: a tenant row on the
// user (the actor is whoever made the change: the user, or the admin who
// reset it).
func (r *Recorder) TwoFactorChanged(ctx context.Context, user *authn.UserRecord, action, method string) {
	if r == nil || user == nil {
		return
	}
	changes := map[string]any{}
	if method != "" {
		changes["method"] = method
	}
	r.Record(ctx, Entry{TenantID: user.TenantID, Action: action, Subject: "user", SubjectID: user.ID, Changes: changes})
}

// InvitationAccepted implements authn.InvitationAudit: the invitee set a
// password and the account is active (a tenant row, by the invitee).
func (r *Recorder) InvitationAccepted(ctx context.Context, user *authn.UserRecord) {
	if r == nil || user == nil {
		return
	}
	r.Record(ctx, Entry{
		TenantID: user.TenantID, ActorID: user.ID, Action: "user.invitation_accepted",
		Subject: "user", SubjectID: user.ID, Changes: map[string]any{"email": user.Email},
	})
}

// EmailChanged implements authn.EmailChangeAudit. Both addresses are
// masked: the entry says the address changed without keeping either.
func (r *Recorder) EmailChanged(ctx context.Context, user *authn.UserRecord, oldEmail, newEmail string) {
	if r == nil || user == nil {
		return
	}
	r.Record(ctx, Entry{
		TenantID: user.TenantID, ActorID: user.ID, Action: "user.email_changed",
		Subject: "user", SubjectID: user.ID,
		Changes: map[string]any{"from": authn.MaskEmail(oldEmail), "to": authn.MaskEmail(newEmail)},
	})
}

// Maintenance implements maintenance.Audit: `ims down` / `ims up` and the
// dashboard toggle are system rows, by the user who toggled (none from the
// CLI).
func (r *Recorder) Maintenance(ctx context.Context, event maintenance.Event) {
	if r == nil {
		return
	}
	entry := Entry{ActorID: event.ByUserID, Action: ActionMaintenanceUp, Subject: "maintenance", Changes: map[string]any{}}
	if event.Down {
		entry.Action = ActionMaintenanceDown
		entry.Changes = map[string]any{
			"message": event.Message, "retry_after": event.RetryAfter, "with_bypass": event.WithBypass,
		}
	} else if event.DownSince != nil {
		entry.Changes["down_since"] = event.DownSince.UTC().Format(time.RFC3339)
		entry.Changes["down_seconds"] = int(time.Since(*event.DownSince).Seconds())
	}
	r.Record(ctx, entry)
}

func impersonationEntry(action string, event tenancy.Impersonation, changes map[string]any) Entry {
	return Entry{
		ActorID: event.ImpersonatorID, Action: action, Subject: "user", SubjectID: event.TargetID,
		Changes: changes,
	}
}

func optional(value int64) *int64 {
	if value == 0 {
		return nil
	}
	return &value
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
