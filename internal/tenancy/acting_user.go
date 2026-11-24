package tenancy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
)

// ActingUserHeader makes one request run as another user.
const ActingUserHeader = "X-Acting-User"

// The acting headers a platform account sends to work inside one tenant.
const (
	ActingTenantHeader = "X-Acting-Tenant"
	ActingBranchHeader = "X-Acting-Branch"
)

// ImpersonationWindow is how long one impersonation.started row covers a
// caller acting as the same target.
const ImpersonationWindow = 30 * time.Minute

// Impersonation is one caller acting as one target, as written to the system
// audit log.
type Impersonation struct {
	ImpersonatorID   int64
	ImpersonatorName string
	TargetID         int64
	TargetName       string
	TenantID         int64 // the target's tenant
	BranchID         int64 // the target's active branch
}

// ImpersonationRequest is one request made while acting as a user.
type ImpersonationRequest struct {
	Impersonation
	Method string
	Route  string
	Status int
}

// ImpersonationAudit writes act-as to the system audit log. The activity
// module implements it; tenancy only decides when to call it.
type ImpersonationAudit interface {
	// ImpersonationStarted must return an error when the row was not stored:
	// the request is then refused, so nothing runs as the target unrecorded.
	ImpersonationStarted(ctx context.Context, event Impersonation) error
	ImpersonationRequested(ctx context.Context, event ImpersonationRequest)
	ImpersonationDenied(ctx context.Context, callerID, targetID int64, reason string)
}

// impersonationKey marks a caller/target pair whose start is already logged.
func impersonationKey(callerID, targetID int64) string {
	return fmt.Sprintf("impersonation:%d:%d", callerID, targetID)
}

// actAsUser handles a request carrying X-Acting-User: it checks the caller
// and the target, swaps the identity for the target's, records the start and
// every request, and continues down the tenant-user path as the target.
func actAsUser(opts ResolveOptions, next http.Handler, w http.ResponseWriter, r *http.Request, caller *authn.Identity, raw string) {
	ctx := r.Context()
	targetID := parseIDHeader(raw)

	deny := func(reason, message string) {
		if opts.Audit != nil {
			opts.Audit.ImpersonationDenied(ctx, caller.UserID, targetID, reason)
		}
		opts.Responder.Error(w, r, apperr.New(apperr.ImpersonationForbidden, message))
	}

	if caller.Impersonated() {
		deny("chained", "You are already acting as another user.")
		return
	}
	allowed, err := opts.Checker.Can(ctx, caller.UserID, authz.PermUserImpersonate)
	if err != nil {
		opts.Responder.Error(w, r, err)
		return
	}
	if !allowed {
		deny("permission", "You do not have permission to act as another user.")
		return
	}
	if r.Header.Get("X-Acting-Tenant") != "" || r.Header.Get("X-Acting-Branch") != "" {
		opts.Responder.Error(w, r, apperr.New(apperr.TenantContextNotResolved,
			"X-Acting-User cannot be combined with X-Acting-Tenant or X-Acting-Branch: the user's own tenant applies."))
		return
	}
	if targetID == 0 {
		opts.Responder.Error(w, r, apperr.Validation(map[string][]string{
			ActingUserHeader: {"The X-Acting-User header must be a user id."},
		}))
		return
	}
	if targetID == caller.UserID {
		deny("self", "You cannot act as yourself.")
		return
	}

	user, err := opts.Queries.GetUser(ctx, dbgen.GetUserParams{ID: targetID, AllTenants: true})
	if errors.Is(err, pgx.ErrNoRows) {
		deny("not_found", "The user to act as does not exist.")
		return
	}
	if err != nil {
		opts.Responder.Error(w, r, apperr.Wrap(apperr.DatabaseError, "load acting user", err))
		return
	}
	if user.IsActive != 1 {
		deny("inactive", "The user to act as is deactivated.")
		return
	}
	if user.TenantID == nil || *user.TenantID == 0 {
		deny("platform_account", "You cannot act as a platform account.")
		return
	}
	targetCanImpersonate, err := opts.Checker.Can(ctx, user.ID, authz.PermUserImpersonate)
	if err != nil {
		opts.Responder.Error(w, r, err)
		return
	}
	if targetCanImpersonate {
		deny("privileged", "You cannot act as a user who can act as others.")
		return
	}

	target := &authn.Identity{
		UserID:           user.ID,
		Name:             user.Name,
		Email:            user.Email,
		TenantID:         *user.TenantID,
		BranchID:         deref(user.BranchID),
		ActiveBranchID:   deref(user.ActiveBranchID),
		IsActive:         true,
		ImpersonatorID:   caller.UserID,
		ImpersonatorName: caller.Name,
	}
	event := Impersonation{
		ImpersonatorID: caller.UserID, ImpersonatorName: caller.Name,
		TargetID: target.UserID, TargetName: target.Name,
		TenantID: target.TenantID, BranchID: target.EffectiveBranchID(),
	}
	if err := recordStart(ctx, opts, event); err != nil {
		opts.Responder.Error(w, r, apperr.Wrap(apperr.ServiceUnavailable,
			"Acting as a user is unavailable because the audit log cannot be written.", err))
		return
	}

	httpx.NoteUser(ctx, target.UserID, caller.UserID)
	actingCtx := authn.WithIdentity(ctx, target)
	recorder := &httpx.ResponseRecorder{ResponseWriter: w, Status: http.StatusOK}
	defer func() {
		if opts.Audit == nil {
			return
		}
		route := ""
		if routeCtx := chi.RouteContext(actingCtx); routeCtx != nil {
			route = routeCtx.RoutePattern()
		}
		opts.Audit.ImpersonationRequested(actingCtx, ImpersonationRequest{
			Impersonation: event, Method: r.Method, Route: route, Status: recorder.Status,
		})
	}()
	resolveTenantUser(opts, next, recorder, r.WithContext(actingCtx), target)
}

// recordStart writes impersonation.started once per caller/target window. A
// cache failure never skips the row; a write failure is returned (fail
// closed) and frees the window so the next request tries again.
func recordStart(ctx context.Context, opts ResolveOptions, event Impersonation) error {
	if opts.Audit == nil {
		return nil
	}
	key := impersonationKey(event.ImpersonatorID, event.TargetID)
	fresh, err := opts.Cache.SetNX(ctx, key, true, ImpersonationWindow)
	if err == nil && !fresh {
		return nil
	}
	if err := opts.Audit.ImpersonationStarted(ctx, event); err != nil {
		_ = opts.Cache.Delete(ctx, key)
		return err
	}
	return nil
}

func deref(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
