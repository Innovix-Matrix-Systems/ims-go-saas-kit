package access

import (
	"strings"
	"time"
)

// RoleInsertUpdateRequest is the tenant role create/update payload.
type RoleInsertUpdateRequest struct {
	Name        string  `json:"name" validate:"required,min=2,max=100"`
	Description *string `json:"description" validate:"omitempty,max=255"`
}

// RoleInternalRequest is the internal role create payload.
type RoleInternalRequest struct {
	Name        string  `json:"name" validate:"required,min=2,max=100"`
	Description *string `json:"description" validate:"omitempty,max=255"`
	TenantID    *int64  `json:"tenant_id" validate:"omitempty,gt=0"`
	IsSystem    bool    `json:"is_system"`
}

// RolePermissionAssignRequest assigns permissions to a role.
type RolePermissionAssignRequest struct {
	ID          int64   `json:"id" validate:"required,gt=0"`
	Permissions []int64 `json:"permissions" validate:"omitempty,dive,gt=0"`
}

// PermissionInsertRequest is the custom permission payload.
type PermissionInsertRequest struct {
	Name        string  `json:"name" validate:"required,min=2,max=150"`
	Group       string  `json:"group" validate:"omitempty,max=100"`
	Description *string `json:"description" validate:"omitempty,max=255"`
}

// TenantSlim is the slim tenant representation embedded in role responses.
type TenantSlim struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"`
}

// NewTenantSlim maps a tenant reference to its slim representation.
func NewTenantSlim(ref TenantRef) *TenantSlim {
	return &TenantSlim{ID: ref.ID, Name: ref.Name, Code: ref.Code}
}

// RoleResponse is the public role representation.
type RoleResponse struct {
	ID          int64       `json:"id"`
	Name        string      `json:"name"`
	IsSystem    bool        `json:"is_system"`
	Description *string     `json:"description"`
	TenantID    *int64      `json:"tenant_id,omitempty"`
	Tenant      *TenantSlim `json:"tenant,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

// NewRoleResponse maps a role to its public representation. tenant is nil for
// system/global roles (no tenant_id) and is omitted from the payload.
func NewRoleResponse(role Role, tenant *TenantSlim) RoleResponse {
	return RoleResponse{
		ID:          role.ID,
		Name:        role.Name,
		IsSystem:    role.IsSystem,
		Description: role.Description,
		TenantID:    role.TenantID,
		Tenant:      tenant,
		CreatedAt:   role.CreatedAt,
		UpdatedAt:   role.UpdatedAt,
	}
}

// PermissionResponse is the public permission representation.
type PermissionResponse struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Group       string  `json:"group"`
	Guard       string  `json:"guard"`
	Description *string `json:"description"`
	IsSystem    bool    `json:"is_system"`
}

// NewPermissionResponse maps a permission to its public representation.
func NewPermissionResponse(permission Permission) PermissionResponse {
	return PermissionResponse{
		ID:          permission.ID,
		Name:        permission.Name,
		Group:       permission.Group,
		Guard:       permission.GuardName,
		Description: permission.Description,
		IsSystem:    permission.IsSystem,
	}
}

func trimString(value string) string {
	return strings.TrimSpace(value)
}
