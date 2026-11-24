package identity

import (
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
)

// InsertUpdateRequest is the user create/update payload.
type InsertUpdateRequest struct {
	FirstName *string `json:"first_name" validate:"omitempty,max=100"`
	LastName  *string `json:"last_name" validate:"omitempty,max=100"`
	UserName  *string `json:"user_name" validate:"omitempty,max=100"`
	Name      string  `json:"name" validate:"required,min=2,max=255"`
	Email     string  `json:"email" validate:"required,email,max=255"`
	Password  *string `json:"password" validate:"omitempty,min=8,max=72"`
	Phone     *string `json:"phone" validate:"omitempty,max=50"`
	IsActive  *bool   `json:"is_active"`
	// SendInvitation (create only) invites the user instead of setting a
	// password: the user is created inactive and sets their own password
	// from the mailed link, which activates the account.
	SendInvitation bool    `json:"send_invitation"`
	BranchID       *int64  `json:"branch_id" validate:"omitempty,gt=0"`
	Roles          []int64 `json:"roles" validate:"omitempty,dive,gt=0"`
	// UpdatedAt (update only, optional) is the updated_at the client last
	// read; when sent, a concurrent change answers 409 instead of being
	// silently overwritten.
	UpdatedAt *time.Time `json:"updated_at"`
}

// InternalInsertRequest extends creation with an explicit tenant.
type InternalInsertRequest struct {
	InsertUpdateRequest
	TenantID *int64 `json:"tenant_id" validate:"omitempty,gt=0"`
}

// AssignRoleRequest assigns roles to a user.
type AssignRoleRequest struct {
	UserID int64   `json:"user_id" validate:"required,gt=0"`
	Roles  []int64 `json:"roles" validate:"required,min=1,dive,gt=0"`
}

// ChangePasswordRequest is the admin password-set payload.
type ChangePasswordRequest struct {
	UserID   int64  `json:"user_id" validate:"required,gt=0"`
	Password string `json:"password" validate:"required,min=8,max=72"`
}

// ProfileUpdateRequest updates the authenticated user's own profile.
type ProfileUpdateRequest struct {
	FirstName *string `json:"first_name" validate:"omitempty,max=100"`
	LastName  *string `json:"last_name" validate:"omitempty,max=100"`
	Name      *string `json:"name" validate:"omitempty,min=2,max=255"`
	Phone     *string `json:"phone" validate:"omitempty,max=50"`
	// Locale is the user's language ("en", "bn"); "" clears it, absent keeps it.
	Locale *string `json:"locale" validate:"omitempty,max=10"`
}

// ProfileChangePasswordRequest changes the own password.
type ProfileChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" validate:"required"`
	Password        string `json:"password" validate:"required,min=8,max=72"`
}

// ExportFiltersRequest is the background export filter body.
type ExportFiltersRequest struct {
	Search         string `json:"search" validate:"omitempty,max=255"`
	IsActive       *bool  `json:"is_active"`
	RoleName       string `json:"role_name" validate:"omitempty,max=255"`
	OrderBy        string `json:"order_by" validate:"omitempty,oneof=id name email created_at"`
	OrderDirection string `json:"order_direction" validate:"omitempty,oneof=asc desc"`
}

// BackgroundJobResponse acknowledges a queued processing job.
type BackgroundJobResponse struct {
	JobID   string `json:"job_id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// RoleSlim is the slim role representation inside user responses.
type RoleSlim struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	IsSystem bool   `json:"is_system"`
}

// Response is the public user representation.
type Response struct {
	ID              int64      `json:"id"`
	FirstName       *string    `json:"first_name"`
	LastName        *string    `json:"last_name"`
	UserName        *string    `json:"user_name"`
	Name            string     `json:"name"`
	Email           string     `json:"email"`
	Phone           *string    `json:"phone"`
	IsActive        bool       `json:"is_active"`
	EmailVerifiedAt *time.Time `json:"email_verified_at"`
	LastLoginAt     *time.Time `json:"last_login_at"`
	// InvitationStatus is none, pending or accepted.
	InvitationStatus string     `json:"invitation_status"`
	TenantID         *int64     `json:"tenant_id"`
	BranchID         *int64     `json:"branch_id"`
	ActiveBranchID   *int64     `json:"active_branch_id"`
	Photo            *string    `json:"photo"`
	Locale           *string    `json:"locale"`
	Roles            []RoleSlim `json:"roles"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// NewResponse maps a user, their roles and profile photo to the public
// representation. photo is nil when the user has no avatar.
func NewResponse(user User, roles []authz.RoleRef, photo *string) Response {
	slim := make([]RoleSlim, 0, len(roles))
	for _, role := range roles {
		slim = append(slim, RoleSlim{ID: role.ID, Name: role.Name, IsSystem: role.IsSystem})
	}
	return Response{
		ID:               user.ID,
		FirstName:        user.FirstName,
		LastName:         user.LastName,
		UserName:         user.UserName,
		Name:             user.Name,
		Email:            user.Email,
		Phone:            user.Phone,
		IsActive:         user.IsActive,
		EmailVerifiedAt:  user.EmailVerifiedAt,
		LastLoginAt:      user.LastLoginAt,
		InvitationStatus: user.InvitationStatus(),
		TenantID:         user.TenantID,
		BranchID:         user.BranchID,
		ActiveBranchID:   user.ActiveBranchID,
		Photo:            photo,
		Locale:           user.Locale,
		Roles:            slim,
		CreatedAt:        user.CreatedAt,
		UpdatedAt:        user.UpdatedAt,
	}
}
func (req InsertUpdateRequest) toCreateInput() CreateParams {
	var password string
	if req.Password != nil {
		password = *req.Password
	}
	return CreateParams{
		SendInvitation: req.SendInvitation,
		BranchID:       req.BranchID,
		FirstName:      req.FirstName,
		LastName:       req.LastName,
		UserName:       req.UserName,
		Name:           strings.TrimSpace(req.Name),
		Email:          strings.TrimSpace(req.Email),
		Password:       password,
		Phone:          req.Phone,
		IsActive:       req.IsActive,
		Roles:          req.Roles,
	}
}
