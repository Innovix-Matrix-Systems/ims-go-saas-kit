package access

import (
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
)

// Role is the domain representation of a role.
type Role struct {
	ID          int64
	TenantID    *int64
	Name        string
	GuardName   string
	IsSystem    bool
	Hidden      bool
	Description *string
	CreatedBy   *int64
	UpdatedBy   *int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Managed reports whether the role is an organization's Admin role, which the
// app keeps in step with the permission catalog (see SyncTenantAdminRoles):
// it cannot be renamed, deleted or given a different set of permissions.
func (r Role) Managed() bool {
	return r.TenantID != nil && r.Name == authz.AdminRole
}

// TenantRef is a slim tenant reference (id, name, code) attached to role
// responses so clients do not need a second lookup.
type TenantRef struct {
	ID   int64
	Name string
	Code string
}

// Permission is the domain representation of a permission.
type Permission struct {
	ID          int64
	Name        string
	GuardName   string
	Group       string
	Description *string
	IsSystem    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func mapRole(row dbgen.Role) Role {
	return Role{
		ID:          row.ID,
		TenantID:    row.TenantID,
		Name:        row.Name,
		GuardName:   row.GuardName,
		IsSystem:    row.IsSystem,
		Hidden:      row.Hidden,
		Description: row.Description,
		CreatedBy:   row.CreatedBy,
		UpdatedBy:   row.UpdatedBy,
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}
}

func mapPermission(row dbgen.Permission) Permission {
	return Permission{
		ID:          row.ID,
		Name:        row.Name,
		GuardName:   row.GuardName,
		Group:       row.Group,
		Description: row.Description,
		IsSystem:    row.IsSystem,
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}
}
