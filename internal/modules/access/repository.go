package access

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
)

// RoleRepository is the persistence contract for roles.
type RoleRepository interface {
	List(ctx context.Context, tenantID int64) ([]Role, error)
	Get(ctx context.Context, tenantID, id int64) (*Role, error)
	GetByID(ctx context.Context, id int64) (*Role, error)
	GetByName(ctx context.Context, tenantID int64, name string) (*Role, error)
	GetSystemByName(ctx context.Context, name string) (*Role, error)
	Create(ctx context.Context, input CreateRoleInput) (*Role, error)
	Update(ctx context.Context, id int64, name string, description *string, actorID *int64) (*Role, error)
	SetHidden(ctx context.Context, id int64, hidden bool) error
	Delete(ctx context.Context, id int64) error
	// UpdateInTenant / DeleteInTenant only touch non-system roles owned by
	// tenantID (isolation enforced in SQL).
	UpdateInTenant(ctx context.Context, tenantID, id int64, name string, description *string, actorID *int64) (*Role, error)
	DeleteInTenant(ctx context.Context, tenantID, id int64) error
	ExistsForTenant(ctx context.Context, tenantID, id int64) (bool, error)
	ClearPermissions(ctx context.Context, roleID int64) error
	AssignPermissions(ctx context.Context, roleID int64, permissionIDs []int64) error
	CountPermissions(ctx context.Context, roleID int64) (int64, error)
	PermissionIDs(ctx context.Context, roleID int64) ([]int64, error)
	IDsByNames(ctx context.Context, tenantID int64, names []string) (map[string]int64, error)
	// GrantToTenantRoles adds the named permissions to every tenant role
	// called roleName (add-only) and returns how many grants were new.
	GrantToTenantRoles(ctx context.Context, roleName string, permissionNames []string) (int64, error)
}

// TenantLookup resolves slim tenant references in bulk so role responses can
// embed the owning tenant without an N+1 (see docs/guides/modules.md).
type TenantLookup interface {
	RefsByIDs(ctx context.Context, ids []int64) (map[int64]TenantRef, error)
}

type tenantLookup struct {
	queries *dbgen.Queries
}

// NewTenantLookup creates the sqlc-backed tenant reference lookup.
func NewTenantLookup(queries *dbgen.Queries) TenantLookup {
	return &tenantLookup{queries: queries}
}

func (l *tenantLookup) RefsByIDs(ctx context.Context, ids []int64) (map[int64]TenantRef, error) {
	refs := make(map[int64]TenantRef, len(ids))
	if len(ids) == 0 {
		return refs, nil
	}
	rows, err := l.queries.ListTenantsByIDs(ctx, ids)
	if err != nil {
		return nil, database.MapError("access.tenants_by_ids", err)
	}
	for _, row := range rows {
		refs[row.ID] = TenantRef{ID: row.ID, Name: row.Name, Code: row.Code}
	}
	return refs, nil
}

// CreateRoleInput carries role creation input.
type CreateRoleInput struct {
	TenantID    *int64
	Name        string
	IsSystem    bool
	Hidden      bool
	Description *string
	ActorID     *int64
}

type roleRepository struct {
	queries *dbgen.Queries
}

// NewRoleRepository creates the sqlc-backed role repository.
func NewRoleRepository(queries *dbgen.Queries) RoleRepository {
	return &roleRepository{queries: queries}
}

func (r *roleRepository) List(ctx context.Context, tenantID int64) ([]Role, error) {
	rows, err := r.queries.ListRoles(ctx, &tenantID)
	if err != nil {
		return nil, database.MapError("access.list_roles", err)
	}
	items := make([]Role, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapRole(row))
	}
	return items, nil
}

func (r *roleRepository) Get(ctx context.Context, tenantID, id int64) (*Role, error) {
	row, err := r.queries.GetRole(ctx, dbgen.GetRoleParams{ID: id, TenantID: &tenantID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Role with ID '%d' was not found.", id)
		}
		return nil, database.MapError("access.get_role", err)
	}
	role := mapRole(row)
	return &role, nil
}

func (r *roleRepository) GetByID(ctx context.Context, id int64) (*Role, error) {
	row, err := r.queries.GetRoleByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Role with ID '%d' was not found.", id)
		}
		return nil, database.MapError("access.get_role_by_id", err)
	}
	role := mapRole(row)
	return &role, nil
}

func (r *roleRepository) GetSystemByName(ctx context.Context, name string) (*Role, error) {
	row, err := r.queries.GetRoleByName(ctx, dbgen.GetRoleByNameParams{Name: name, TenantID: nil})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Role '%s' was not found.", name)
		}
		return nil, database.MapError("access.get_system_role", err)
	}
	role := mapRole(row)
	return &role, nil
}

func (r *roleRepository) GetByName(ctx context.Context, tenantID int64, name string) (*Role, error) {
	row, err := r.queries.GetRoleByName(ctx, dbgen.GetRoleByNameParams{Name: name, TenantID: &tenantID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Role '%s' was not found.", name)
		}
		return nil, database.MapError("access.get_role_by_name", err)
	}
	role := mapRole(row)
	return &role, nil
}

func (r *roleRepository) Create(ctx context.Context, input CreateRoleInput) (*Role, error) {
	row, err := r.queries.CreateRole(ctx, dbgen.CreateRoleParams{
		TenantID:    input.TenantID,
		Name:        input.Name,
		IsSystem:    input.IsSystem,
		Hidden:      input.Hidden,
		Description: input.Description,
		CreatedBy:   input.ActorID,
		UpdatedBy:   input.ActorID,
	})
	if err != nil {
		return nil, database.MapError("access.create_role", err)
	}
	role := mapRole(row)
	return &role, nil
}

func (r *roleRepository) Update(ctx context.Context, id int64, name string, description *string, actorID *int64) (*Role, error) {
	row, err := r.queries.UpdateRole(ctx, dbgen.UpdateRoleParams{
		ID:          id,
		Name:        name,
		Description: description,
		UpdatedBy:   actorID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Role with ID '%d' was not found.", id)
		}
		return nil, database.MapError("access.update_role", err)
	}
	role := mapRole(row)
	return &role, nil
}

func (r *roleRepository) SetHidden(ctx context.Context, id int64, hidden bool) error {
	if err := r.queries.SetRoleHidden(ctx, dbgen.SetRoleHiddenParams{ID: id, Hidden: hidden}); err != nil {
		return database.MapError("access.set_role_hidden", err)
	}
	return nil
}

func (r *roleRepository) Delete(ctx context.Context, id int64) error {
	affected, err := r.queries.DeleteRole(ctx, id)
	if err != nil {
		return database.MapError("access.delete_role", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.ResourceNotFound, "Role with ID '%d' was not found.", id)
	}
	return nil
}

func (r *roleRepository) UpdateInTenant(ctx context.Context, tenantID, id int64, name string, description *string, actorID *int64) (*Role, error) {
	row, err := r.queries.UpdateTenantRole(ctx, dbgen.UpdateTenantRoleParams{
		ID:          id,
		TenantID:    tenantID,
		Name:        name,
		Description: description,
		UpdatedBy:   actorID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Role with ID '%d' was not found.", id)
		}
		return nil, database.MapError("access.update_tenant_role", err)
	}
	role := mapRole(row)
	return &role, nil
}

func (r *roleRepository) DeleteInTenant(ctx context.Context, tenantID, id int64) error {
	affected, err := r.queries.DeleteTenantRole(ctx, dbgen.DeleteTenantRoleParams{ID: id, TenantID: tenantID})
	if err != nil {
		return database.MapError("access.delete_tenant_role", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.ResourceNotFound, "Role with ID '%d' was not found.", id)
	}
	return nil
}

func (r *roleRepository) ExistsForTenant(ctx context.Context, tenantID, id int64) (bool, error) {
	exists, err := r.queries.RoleAssignableToTenant(ctx, dbgen.RoleAssignableToTenantParams{ID: id, TenantID: &tenantID})
	if err != nil {
		return false, database.MapError("access.role_exists", err)
	}
	return exists, nil
}

// IDsByNames maps role names to IDs visible to a tenant (0 = internal).
func (r *roleRepository) IDsByNames(ctx context.Context, tenantID int64, names []string) (map[string]int64, error) {
	rows, err := r.queries.ListRoleIDsByNames(ctx, dbgen.ListRoleIDsByNamesParams{
		Names:    names,
		TenantID: tenantID,
	})
	if err != nil {
		return nil, database.MapError("access.role_ids_by_names", err)
	}
	ids := make(map[string]int64, len(rows))
	for _, row := range rows {
		ids[row.Name] = row.ID
	}
	return ids, nil
}

func (r *roleRepository) ClearPermissions(ctx context.Context, roleID int64) error {
	if err := r.queries.ClearRolePermissions(ctx, roleID); err != nil {
		return database.MapError("access.clear_role_permissions", err)
	}
	return nil
}

func (r *roleRepository) GrantToTenantRoles(ctx context.Context, roleName string, permissionNames []string) (int64, error) {
	if len(permissionNames) == 0 {
		return 0, nil
	}
	granted, err := r.queries.GrantPermissionsToTenantRoles(ctx, dbgen.GrantPermissionsToTenantRolesParams{
		RoleName:        roleName,
		PermissionNames: permissionNames,
	})
	if err != nil {
		return 0, database.MapError("access.grant_tenant_role_permissions", err)
	}
	return granted, nil
}

func (r *roleRepository) AssignPermissions(ctx context.Context, roleID int64, permissionIDs []int64) error {
	if len(permissionIDs) == 0 {
		return nil
	}
	if err := r.queries.AssignRolePermissions(ctx, dbgen.AssignRolePermissionsParams{
		RoleID:        roleID,
		PermissionIds: permissionIDs,
	}); err != nil {
		return database.MapError("access.assign_role_permissions", err)
	}
	return nil
}

func (r *roleRepository) CountPermissions(ctx context.Context, roleID int64) (int64, error) {
	count, err := r.queries.CountRolePermissions(ctx, roleID)
	if err != nil {
		return 0, database.MapError("access.count_role_permissions", err)
	}
	return count, nil
}

func (r *roleRepository) PermissionIDs(ctx context.Context, roleID int64) ([]int64, error) {
	ids, err := r.queries.RolePermissionIDs(ctx, roleID)
	if err != nil {
		return nil, database.MapError("access.role_permission_ids", err)
	}
	return ids, nil
}

// PermissionRepository is the persistence contract for permissions.
type PermissionRepository interface {
	List(ctx context.Context) ([]Permission, error)
	Get(ctx context.Context, id int64) (*Permission, error)
	GetByIDs(ctx context.Context, ids []int64) ([]Permission, error)
	GetByNames(ctx context.Context, names []string) ([]Permission, error)
	Create(ctx context.Context, name, group string, description *string) (*Permission, error)
	Delete(ctx context.Context, id int64) error
	Upsert(ctx context.Context, permissions []authz.Permission) (int64, error)
	DeleteStale(ctx context.Context, keepNames []string) (int64, error)
	ForUser(ctx context.Context, userID int64) ([]Permission, error)
}

type permissionRepository struct {
	queries *dbgen.Queries
}

// NewPermissionRepository creates the sqlc-backed permission repository.
func NewPermissionRepository(queries *dbgen.Queries) PermissionRepository {
	return &permissionRepository{queries: queries}
}

func (r *permissionRepository) List(ctx context.Context) ([]Permission, error) {
	rows, err := r.queries.ListPermissions(ctx)
	if err != nil {
		return nil, database.MapError("access.list_permissions", err)
	}
	items := make([]Permission, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapPermission(row))
	}
	return items, nil
}

func (r *permissionRepository) Get(ctx context.Context, id int64) (*Permission, error) {
	row, err := r.queries.GetPermission(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Permission with ID '%d' was not found.", id)
		}
		return nil, database.MapError("access.get_permission", err)
	}
	permission := mapPermission(row)
	return &permission, nil
}

func (r *permissionRepository) GetByIDs(ctx context.Context, ids []int64) ([]Permission, error) {
	rows, err := r.queries.GetPermissionsByIDs(ctx, ids)
	if err != nil {
		return nil, database.MapError("access.get_permissions_by_ids", err)
	}
	items := make([]Permission, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapPermission(row))
	}
	return items, nil
}

func (r *permissionRepository) GetByNames(ctx context.Context, names []string) ([]Permission, error) {
	rows, err := r.queries.GetPermissionsByNames(ctx, names)
	if err != nil {
		return nil, database.MapError("access.get_permissions_by_names", err)
	}
	items := make([]Permission, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapPermission(row))
	}
	return items, nil
}

func (r *permissionRepository) Create(ctx context.Context, name, group string, description *string) (*Permission, error) {
	row, err := r.queries.CreatePermission(ctx, dbgen.CreatePermissionParams{
		Name:        name,
		GroupName:   group,
		Description: description,
		IsSystem:    false,
	})
	if err != nil {
		return nil, database.MapError("access.create_permission", err)
	}
	permission := mapPermission(row)
	return &permission, nil
}

func (r *permissionRepository) Delete(ctx context.Context, id int64) error {
	affected, err := r.queries.DeletePermission(ctx, id)
	if err != nil {
		return database.MapError("access.delete_permission", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.ResourceNotFound, "Permission with ID '%d' was not found.", id)
	}
	return nil
}

func (r *permissionRepository) Upsert(ctx context.Context, permissions []authz.Permission) (int64, error) {
	names := make([]string, 0, len(permissions))
	groups := make([]string, 0, len(permissions))
	descriptions := make([]string, 0, len(permissions))
	isSystem := make([]bool, 0, len(permissions))
	for _, permission := range permissions {
		names = append(names, permission.Name)
		groups = append(groups, permission.Group)
		descriptions = append(descriptions, permission.Description)
		isSystem = append(isSystem, permission.IsSystem)
	}
	affected, err := r.queries.UpsertPermissions(ctx, dbgen.UpsertPermissionsParams{
		Names:         names,
		Groups:        groups,
		Descriptions:  descriptions,
		IsSystemFlags: isSystem,
	})
	if err != nil {
		return 0, database.MapError("access.upsert_permissions", err)
	}
	return affected, nil
}

// DeleteStaleSystem removes system permissions that are no longer part of the
// catalog (e.g. permissions retired by a rename). Role assignments cascade.
func (r *permissionRepository) DeleteStale(ctx context.Context, keepNames []string) (int64, error) {
	deleted, err := r.queries.DeleteStaleSystemPermissions(ctx, keepNames)
	if err != nil {
		return 0, database.MapError("access.delete_stale_permissions", err)
	}
	return deleted, nil
}

func (r *permissionRepository) ForUser(ctx context.Context, userID int64) ([]Permission, error) {
	rows, err := r.queries.UserPermissions(ctx, dbgen.UserPermissionsParams{
		ModelType: authz.ModelTypeUser,
		ModelID:   userID,
	})
	if err != nil {
		return nil, database.MapError("access.user_permissions", err)
	}
	items := make([]Permission, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapPermission(row))
	}
	return items, nil
}
