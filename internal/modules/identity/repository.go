package identity

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// Repository is the persistence contract for users. Single-row reads and
// writes take a tenancy.Filter so tenant isolation is enforced in SQL.
type Repository interface {
	Get(ctx context.Context, tenant tenancy.Filter, id int64) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	List(ctx context.Context, filter ListFilter) ([]User, error)
	Count(ctx context.Context, filter ListFilter) (int64, error)
	Create(ctx context.Context, input CreateInput) (*User, error)
	Update(ctx context.Context, input UpdateInput) (*User, error)
	Delete(ctx context.Context, tenant tenancy.Filter, id int64, actorID *int64) error
	UpdatePassword(ctx context.Context, tenant tenancy.Filter, id int64, hash string, actorID *int64) error
	SwitchBranch(ctx context.Context, userID, branchID int64) error
	EmailExists(ctx context.Context, email string, excludeID int64) (bool, error)
	PhoneExists(ctx context.Context, phone string, excludeID int64) (bool, error)
	ClearRoles(ctx context.Context, userID int64) error
	AssignRoles(ctx context.Context, userID int64, roleIDs []int64) error
	AddRole(ctx context.Context, userID, roleID int64) error
	RemoveRole(ctx context.Context, userID, roleID int64) (bool, error)
	ListForExport(ctx context.Context, filter ExportFilter) ([]ExportRow, error)

	// authn.UserLookup
	FindByEmail(ctx context.Context, email string) (*authn.UserRecord, error)
	FindByID(ctx context.Context, id int64) (*authn.UserRecord, error)
	TouchLogin(ctx context.Context, userID int64, device string) error

	// branches.UserChecker
	ExistsInTenant(ctx context.Context, tenantID, userID int64) (bool, error)

	// authn.AccountWriter
	SetPassword(ctx context.Context, userID int64, hash string) error
	AcceptInvitation(ctx context.Context, userID int64, hash string) (bool, error)
	MarkInvited(ctx context.Context, id int64, by *int64) (bool, error)
	MarkEmailVerified(ctx context.Context, userID int64) error
}

// ListFilter carries user list filters.
type ListFilter struct {
	AllTenants bool
	TenantID   int64
	Search     string
	IsActive   *bool
	RoleName   string
	// Invitation is "", pending or accepted.
	Invitation string
	OrderBy    string
	OrderDir   string
	Limit      int32
	Offset     int32
}

// ExportFilter carries user export filters (a snapshot of the request).
type ExportFilter struct {
	AllTenants bool
	TenantID   int64
	Search     string
	IsActive   *bool
	RoleName   string
	OrderBy    string
	OrderDir   string
	Limit      int32
	Offset     int32
}

// ExportRow is one export-ready user row (roles/branch pre-joined).
type ExportRow struct {
	ID         int64
	Name       string
	Email      string
	Phone      *string
	IsActive   bool
	BranchName string
	RoleNames  string
	CreatedAt  time.Time
}

// CreateInput carries user creation input.
type CreateInput struct {
	TenantID        *int64
	BranchID        *int64
	ActiveBranchID  *int64
	FirstName       *string
	LastName        *string
	UserName        *string
	Name            string
	Email           string
	Password        string
	Phone           *string
	IsActive        bool
	EmailVerifiedAt time.Time
	ActorID         *int64
	// InvitedBy is set when the user is created with an invitation.
	InvitedBy *int64
}

// UpdateInput carries user update input.
type UpdateInput struct {
	Tenant    tenancy.Filter
	ID        int64
	FirstName *string
	LastName  *string
	Name      string
	Phone     *string
	IsActive  bool
	BranchID  *int64
	ActorID   *int64
	// ExpectedUpdatedAt, when set, makes the update fail with 409 if the row
	// changed since the client read it (opt-in optimistic locking).
	ExpectedUpdatedAt *time.Time
}

type repository struct {
	queries *dbgen.Queries
}

// NewRepository creates the sqlc-backed user repository.
func NewRepository(queries *dbgen.Queries) Repository {
	return &repository{queries: queries}
}

func (r *repository) Get(ctx context.Context, tenant tenancy.Filter, id int64) (*User, error) {
	return r.findByID(ctx, tenant, id, "users.get")
}

func (r *repository) GetByEmail(ctx context.Context, email string) (*User, error) {
	row, err := r.queries.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "User with email '%s' was not found.", email)
		}
		return nil, database.MapError("users.get_by_email", err)
	}
	user := mapUser(row)
	return &user, nil
}

func (r *repository) List(ctx context.Context, filter ListFilter) ([]User, error) {
	rows, err := r.queries.ListUsers(ctx, dbgen.ListUsersParams{
		AllTenants: filter.AllTenants,
		TenantID:   optionalInt64(filter.TenantID),
		Search:     optionalString(filter.Search),
		IsActive:   boolToSmallint(filter.IsActive),
		RoleName:   optionalString(filter.RoleName),
		Invitation: filter.Invitation,
		OrderBy:    filter.OrderBy,
		OrderDir:   filter.OrderDir,
		PageLimit:  filter.Limit,
		PageOffset: filter.Offset,
	})
	if err != nil {
		return nil, database.MapError("users.list", err)
	}
	items := make([]User, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapUser(row))
	}
	return items, nil
}

func (r *repository) Count(ctx context.Context, filter ListFilter) (int64, error) {
	total, err := r.queries.CountUsers(ctx, dbgen.CountUsersParams{
		AllTenants: filter.AllTenants,
		TenantID:   optionalInt64(filter.TenantID),
		Search:     optionalString(filter.Search),
		IsActive:   boolToSmallint(filter.IsActive),
		RoleName:   optionalString(filter.RoleName),
		Invitation: filter.Invitation,
	})
	if err != nil {
		return 0, database.MapError("users.count", err)
	}
	return total, nil
}

// ListForExport streams export-ready user rows (roles and branch pre-joined).
func (r *repository) ListForExport(ctx context.Context, filter ExportFilter) ([]ExportRow, error) {
	rows, err := r.queries.ListUsersForExport(ctx, dbgen.ListUsersForExportParams{
		AllTenants: filter.AllTenants,
		TenantID:   filter.TenantID,
		Search:     optionalString(filter.Search),
		IsActive:   filter.IsActive,
		RoleName:   optionalString(filter.RoleName),
		OrderBy:    filter.OrderBy,
		OrderDir:   filter.OrderDir,
		PageLimit:  filter.Limit,
		PageOffset: filter.Offset,
	})
	if err != nil {
		return nil, database.MapError("users.list_export", err)
	}
	items := make([]ExportRow, 0, len(rows))
	for _, row := range rows {
		items = append(items, ExportRow{
			ID:         row.ID,
			Name:       row.Name,
			Email:      row.Email,
			Phone:      row.Phone,
			IsActive:   row.IsActive == 1,
			BranchName: row.BranchName,
			RoleNames:  row.RoleNames,
			CreatedAt:  row.CreatedAt.Time,
		})
	}
	return items, nil
}

func (r *repository) Create(ctx context.Context, input CreateInput) (*User, error) {
	row, err := r.queries.CreateUser(ctx, dbgen.CreateUserParams{
		TenantID:        input.TenantID,
		BranchID:        input.BranchID,
		ActiveBranchID:  input.ActiveBranchID,
		FirstName:       input.FirstName,
		LastName:        input.LastName,
		UserName:        input.UserName,
		Name:            input.Name,
		Email:           input.Email,
		EmailVerifiedAt: optionalTimestamptz(input.EmailVerifiedAt),
		Phone:           input.Phone,
		Password:        input.Password,
		IsActive:        boolToInt16(input.IsActive),
		CreatedBy:       input.ActorID,
		UpdatedBy:       input.ActorID,
		InvitedAt:       invitedAt(input.InvitedBy),
		InvitedBy:       input.InvitedBy,
	})
	if err != nil {
		return nil, database.MapError("users.create", err)
	}
	user := mapUser(row)
	return &user, nil
}

func (r *repository) Update(ctx context.Context, input UpdateInput) (*User, error) {
	params := dbgen.UpdateUserParams{
		ID:         input.ID,
		FirstName:  input.FirstName,
		LastName:   input.LastName,
		Name:       input.Name,
		Phone:      input.Phone,
		IsActive:   boolToInt16(input.IsActive),
		BranchID:   input.BranchID,
		UpdatedBy:  input.ActorID,
		AllTenants: input.Tenant.All,
		TenantID:   input.Tenant.TenantID,
	}
	if input.ExpectedUpdatedAt != nil {
		params.ExpectedUpdatedAt = timestamptz(*input.ExpectedUpdatedAt)
	}
	row, err := r.queries.UpdateUser(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if input.ExpectedUpdatedAt != nil {
				return nil, apperr.New(apperr.Conflict, "The user was changed by someone else. Reload it and try again.")
			}
			return nil, apperr.Newf(apperr.ResourceNotFound, "User with ID '%d' was not found.", input.ID)
		}
		return nil, database.MapError("users.update", err)
	}
	user := mapUser(row)
	return &user, nil
}

func (r *repository) Delete(ctx context.Context, tenant tenancy.Filter, id int64, actorID *int64) error {
	affected, err := r.queries.SoftDeleteUser(ctx, dbgen.SoftDeleteUserParams{
		ID:         id,
		UpdatedBy:  actorID,
		AllTenants: tenant.All,
		TenantID:   tenant.TenantID,
	})
	if err != nil {
		return database.MapError("users.delete", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.ResourceNotFound, "User with ID '%d' was not found.", id)
	}
	return nil
}

func (r *repository) UpdatePassword(ctx context.Context, tenant tenancy.Filter, id int64, hash string, actorID *int64) error {
	affected, err := r.queries.UpdateUserPassword(ctx, dbgen.UpdateUserPasswordParams{
		ID:         id,
		Password:   hash,
		UpdatedBy:  actorID,
		AllTenants: tenant.All,
		TenantID:   tenant.TenantID,
	})
	if err != nil {
		return database.MapError("users.update_password", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.ResourceNotFound, "User with ID '%d' was not found.", id)
	}
	return nil
}

func (r *repository) SwitchBranch(ctx context.Context, userID, branchID int64) error {
	affected, err := r.queries.SwitchUserBranch(ctx, dbgen.SwitchUserBranchParams{
		ID:       userID,
		BranchID: &branchID,
	})
	if err != nil {
		return database.MapError("users.switch_branch", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.ResourceNotFound, "User with ID '%d' was not found.", userID)
	}
	return nil
}

func (r *repository) EmailExists(ctx context.Context, email string, excludeID int64) (bool, error) {
	exists, err := r.queries.EmailExists(ctx, dbgen.EmailExistsParams{Email: email, ExcludeID: excludeID})
	if err != nil {
		return false, database.MapError("users.email_exists", err)
	}
	return exists, nil
}

func (r *repository) PhoneExists(ctx context.Context, phone string, excludeID int64) (bool, error) {
	exists, err := r.queries.PhoneExists(ctx, dbgen.PhoneExistsParams{Phone: optionalString(phone), ExcludeID: excludeID})
	if err != nil {
		return false, database.MapError("users.phone_exists", err)
	}
	return exists, nil
}

func (r *repository) ClearRoles(ctx context.Context, userID int64) error {
	if err := r.queries.ClearUserRoles(ctx, dbgen.ClearUserRolesParams{
		ModelType: authz.ModelTypeUser,
		ModelID:   userID,
	}); err != nil {
		return database.MapError("users.clear_roles", err)
	}
	return nil
}

func (r *repository) AssignRoles(ctx context.Context, userID int64, roleIDs []int64) error {
	if len(roleIDs) == 0 {
		return nil
	}
	if err := r.queries.AssignUserRoles(ctx, dbgen.AssignUserRolesParams{
		RoleIds:   roleIDs,
		ModelType: authz.ModelTypeUser,
		ModelID:   userID,
	}); err != nil {
		return database.MapError("users.assign_roles", err)
	}
	return nil
}

func (r *repository) AddRole(ctx context.Context, userID, roleID int64) error {
	if err := r.queries.AssignUserRoles(ctx, dbgen.AssignUserRolesParams{
		RoleIds:   []int64{roleID},
		ModelType: authz.ModelTypeUser,
		ModelID:   userID,
	}); err != nil {
		return database.MapError("users.add_role", err)
	}
	return nil
}

func (r *repository) RemoveRole(ctx context.Context, userID, roleID int64) (bool, error) {
	removed, err := r.queries.RemoveUserRole(ctx, dbgen.RemoveUserRoleParams{
		RoleID:    roleID,
		ModelType: authz.ModelTypeUser,
		ModelID:   userID,
	})
	if err != nil {
		return false, database.MapError("users.remove_role", err)
	}
	return removed > 0, nil
}

// FindByEmail implements authn.UserLookup: the user plus their tenant's state.
func (r *repository) FindByEmail(ctx context.Context, email string) (*authn.UserRecord, error) {
	row, err := r.queries.GetAuthUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "User with email '%s' was not found.", email)
		}
		return nil, database.MapError("users.find_by_email", err)
	}
	return mapUser(row.User).toRecord(row.TenantActive), nil
}

// FindByID implements authn.UserLookup. It runs on every authenticated
// request, so the tenant's state comes from the same query.
func (r *repository) FindByID(ctx context.Context, id int64) (*authn.UserRecord, error) {
	row, err := r.queries.GetAuthUser(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "User with ID '%d' was not found.", id)
		}
		return nil, database.MapError("users.find_by_id", err)
	}
	return mapUser(row.User).toRecord(row.TenantActive), nil
}

// MarkInvited records a (re)sent invitation; false when the user accepted
// one or has signed in before (an account in use is not invited).
func (r *repository) MarkInvited(ctx context.Context, id int64, by *int64) (bool, error) {
	affected, err := r.queries.MarkUserInvited(ctx, dbgen.MarkUserInvitedParams{ID: id, InvitedBy: by})
	if err != nil {
		return false, database.MapError("users.mark_invited", err)
	}
	return affected > 0, nil
}

// AcceptInvitation implements authn.AccountWriter: the invitee's password,
// an active account and a verified address, for a pending invitation only.
func (r *repository) AcceptInvitation(ctx context.Context, userID int64, hash string) (bool, error) {
	affected, err := r.queries.AcceptUserInvitation(ctx, dbgen.AcceptUserInvitationParams{ID: userID, Password: hash})
	if err != nil {
		return false, database.MapError("users.accept_invitation", err)
	}
	return affected > 0, nil
}

// SetPassword implements authn.AccountWriter (password reset by token).
func (r *repository) SetPassword(ctx context.Context, userID int64, hash string) error {
	return r.UpdatePassword(ctx, tenancy.Unscoped(), userID, hash, &userID)
}

// MarkEmailVerified implements authn.AccountWriter.
func (r *repository) MarkEmailVerified(ctx context.Context, userID int64) error {
	if _, err := r.queries.MarkUserEmailVerified(ctx, userID); err != nil {
		return database.MapError("users.mark_email_verified", err)
	}
	return nil
}

func (r *repository) TouchLogin(ctx context.Context, userID int64, device string) error {
	if _, err := r.queries.TouchUserLogin(ctx, dbgen.TouchUserLoginParams{
		ID:     userID,
		Device: optionalString(device),
	}); err != nil {
		return database.MapError("users.touch_login", err)
	}
	return nil
}

func (r *repository) ExistsInTenant(ctx context.Context, tenantID, userID int64) (bool, error) {
	if _, err := r.Get(ctx, tenancy.InTenant(tenantID), userID); err != nil {
		if apperr.Is(err, apperr.ResourceNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *repository) findByID(ctx context.Context, tenant tenancy.Filter, id int64, op string) (*User, error) {
	row, err := r.queries.GetUser(ctx, dbgen.GetUserParams{ID: id, AllTenants: tenant.All, TenantID: tenant.TenantID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "User with ID '%d' was not found.", id)
		}
		return nil, database.MapError(op, err)
	}
	user := mapUser(row)
	return &user, nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func optionalInt64(value int64) *int64 {
	if value == 0 {
		return nil
	}
	return &value
}

func boolToInt16(value bool) int16 {
	if value {
		return 1
	}
	return 0
}

func boolToSmallint(value *bool) *int16 {
	if value == nil {
		return nil
	}
	converted := boolToInt16(*value)
	return &converted
}
