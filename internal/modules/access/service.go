package access

import (
	"context"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
)

// RoleService implements role business rules.
type RoleService struct {
	roles    RoleRepository
	perms    PermissionRepository
	checker  *authz.Checker
	tx       *database.TxManager
	activity *activity.Recorder
}

// WithActivity records tenant role changes in the activity log.
func (s *RoleService) WithActivity(recorder *activity.Recorder) *RoleService {
	s.activity = recorder
	return s
}

// NewRoleService creates the role service.
func NewRoleService(roles RoleRepository, perms PermissionRepository, checker *authz.Checker, tx *database.TxManager) *RoleService {
	return &RoleService{roles: roles, perms: perms, checker: checker, tx: tx}
}

// List returns tenant-scoped roles (tenant roles plus system roles).
func (s *RoleService) List(ctx context.Context, tenantID int64) ([]Role, error) {
	return s.roles.List(ctx, tenantID)
}

// Get returns one role visible to the tenant.
func (s *RoleService) Get(ctx context.Context, tenantID, id int64) (*Role, error) {
	return s.roles.Get(ctx, tenantID, id)
}

// Create adds a tenant role.
func (s *RoleService) Create(ctx context.Context, tenantID, actorID int64, name string, description *string) (*Role, error) {
	if isReservedRoleName(name) {
		return nil, apperr.Validation(map[string][]string{"name": {"The name has already been taken."}})
	}
	if _, err := s.roles.GetByName(ctx, tenantID, name); err == nil {
		return nil, apperr.Validation(map[string][]string{"name": {"The name has already been taken."}})
	} else if !apperr.Is(err, apperr.ResourceNotFound) {
		return nil, err
	}
	tenant := tenantID
	role, err := s.roles.Create(ctx, CreateRoleInput{
		TenantID:    &tenant,
		Name:        name,
		Description: description,
		ActorID:     &actorID,
	})
	if err != nil {
		return nil, err
	}
	s.activity.Record(ctx, activity.Entry{
		TenantID: tenantID, ActorID: actorID, Action: "role.created",
		Subject: "role", SubjectID: role.ID, Changes: map[string]any{"name": name, "description": description},
	})
	return role, nil
}

// CreateInternal adds a role from the internal (super-admin) surface.
func (s *RoleService) CreateInternal(ctx context.Context, tenantID *int64, actorID int64, name string, description *string, isSystem bool) (*Role, error) {
	if tenantID != nil {
		if _, err := s.roles.GetByName(ctx, *tenantID, name); err == nil {
			return nil, apperr.Validation(map[string][]string{"name": {"The name has already been taken."}})
		} else if !apperr.Is(err, apperr.ResourceNotFound) {
			return nil, err
		}
	} else if _, err := s.roles.GetSystemByName(ctx, name); err == nil {
		return nil, apperr.Validation(map[string][]string{"name": {"The name has already been taken."}})
	} else if !apperr.Is(err, apperr.ResourceNotFound) {
		return nil, err
	}
	return s.roles.Create(ctx, CreateRoleInput{
		TenantID:    tenantID,
		Name:        name,
		IsSystem:    isSystem,
		Description: description,
		ActorID:     &actorID,
	})
}

// EnsureDeveloperRole creates the hidden global Developer role when missing,
// marks an existing role hidden, and syncs authz.DeveloperPermissions onto it
// (dashboard access only, not data administration).
// It is idempotent and mirrors the seeder's Super-Admin permission sync.
func (s *RoleService) EnsureDeveloperRole(ctx context.Context, actorID int64) (*Role, int64, error) {
	role, err := s.roles.GetSystemByName(ctx, authz.DeveloperRole)
	if err != nil {
		if !apperr.Is(err, apperr.ResourceNotFound) {
			return nil, 0, err
		}
		description := "Developer dashboard access (hidden)"
		var actor *int64
		if actorID > 0 {
			actor = &actorID
		}
		role, err = s.roles.Create(ctx, CreateRoleInput{
			Name:        authz.DeveloperRole,
			IsSystem:    true,
			Hidden:      true,
			Description: &description,
			ActorID:     actor,
		})
		if err != nil {
			return nil, 0, err
		}
	} else if !role.Hidden {
		if err := s.roles.SetHidden(ctx, role.ID, true); err != nil {
			return nil, 0, err
		}
		role.Hidden = true
	}

	if _, err := s.perms.Upsert(ctx, authz.Catalog); err != nil {
		return nil, 0, err
	}
	devPerms := authz.DeveloperPermissions()
	names := make([]string, 0, len(devPerms))
	for _, permission := range devPerms {
		names = append(names, permission.Name)
	}
	permissions, err := s.perms.GetByNames(ctx, names)
	if err != nil {
		return nil, 0, err
	}
	ids := make([]int64, 0, len(permissions))
	for _, permission := range permissions {
		ids = append(ids, permission.ID)
	}
	if err := s.roles.ClearPermissions(ctx, role.ID); err != nil {
		return nil, 0, err
	}
	if err := s.roles.AssignPermissions(ctx, role.ID, ids); err != nil {
		return nil, 0, err
	}
	_ = s.checker.InvalidateRole(ctx, role.ID)
	return role, int64(len(ids)), nil
}

// Update modifies a tenant role; system roles are protected.
func (s *RoleService) Update(ctx context.Context, tenantID, id, actorID int64, name string, description *string) (*Role, error) {
	role, err := s.roles.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if role.IsSystem {
		return nil, apperr.New(apperr.Forbidden, "System roles cannot be modified.")
	}
	if name == authz.SuperAdminRole || name == authz.DeveloperRole {
		return nil, apperr.Validation(map[string][]string{"name": {"The name has already been taken."}})
	}
	updated, err := s.roles.UpdateInTenant(ctx, tenantID, id, name, description, &actorID)
	if err != nil {
		return nil, err
	}
	_ = s.checker.InvalidateRole(ctx, id)
	s.activity.Record(ctx, activity.Entry{
		TenantID: tenantID, ActorID: actorID, Action: "role.updated",
		Subject: "role", SubjectID: id, Changes: map[string]any{"name": name, "description": description},
	})
	return updated, nil
}

// UpdateInternal modifies any role, including system roles.
func (s *RoleService) UpdateInternal(ctx context.Context, id, actorID int64, name string, description *string) (*Role, error) {
	if _, err := s.roles.GetByID(ctx, id); err != nil {
		return nil, err
	}
	updated, err := s.roles.Update(ctx, id, name, description, &actorID)
	if err != nil {
		return nil, err
	}
	_ = s.checker.InvalidateRole(ctx, id)
	return updated, nil
}

// Delete removes a tenant role; system roles are protected.
func (s *RoleService) Delete(ctx context.Context, tenantID, id int64) error {
	role, err := s.roles.Get(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if role.IsSystem {
		return apperr.New(apperr.Forbidden, "System roles cannot be deleted.")
	}
	_ = s.checker.InvalidateRole(ctx, id)
	if err := s.roles.DeleteInTenant(ctx, tenantID, id); err != nil {
		return err
	}
	s.activity.Record(ctx, activity.Entry{
		TenantID: tenantID, Action: "role.deleted", Subject: "role", SubjectID: id,
		Changes: map[string]any{"name": role.Name},
	})
	return nil
}

// DeleteInternal removes any role, including system roles.
func (s *RoleService) DeleteInternal(ctx context.Context, id int64) error {
	if _, err := s.roles.GetByID(ctx, id); err != nil {
		return err
	}
	_ = s.checker.InvalidateRole(ctx, id)
	if err := s.roles.Delete(ctx, id); err != nil {
		return err
	}
	return nil
}

// AssignPermissions replaces a tenant role's permissions. System permissions
// are stripped for tenant roles.
func (s *RoleService) AssignPermissions(ctx context.Context, tenantID, roleID int64, permissionIDs []int64) error {
	role, err := s.roles.Get(ctx, tenantID, roleID)
	if err != nil {
		return err
	}
	if role.IsSystem {
		return apperr.New(apperr.Forbidden, "System roles cannot be modified.")
	}
	if err := s.replacePermissions(ctx, roleID, permissionIDs, true); err != nil {
		return err
	}
	s.activity.Record(ctx, activity.Entry{
		TenantID: tenantID, Action: "role.permissions_assigned", Subject: "role", SubjectID: roleID,
		Changes: map[string]any{"permissions": permissionIDs},
	})
	return nil
}

// AssignPermissionsInternal replaces permissions including system ones.
func (s *RoleService) AssignPermissionsInternal(ctx context.Context, roleID int64, permissionIDs []int64) error {
	if _, err := s.roles.GetByID(ctx, roleID); err != nil {
		return err
	}
	return s.replacePermissions(ctx, roleID, permissionIDs, false)
}

func (s *RoleService) replacePermissions(ctx context.Context, roleID int64, permissionIDs []int64, stripSystem bool) error {
	unique := make(map[int64]struct{}, len(permissionIDs))
	for _, id := range permissionIDs {
		unique[id] = struct{}{}
	}
	ids := make([]int64, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}

	existing, err := s.perms.GetByIDs(ctx, ids)
	if err != nil {
		return err
	}
	if len(existing) != len(ids) {
		return apperr.Validation(map[string][]string{
			"permissions": {"One or more selected permissions are invalid."},
		})
	}

	keep := make([]int64, 0, len(existing))
	for _, permission := range existing {
		if stripSystem && permission.IsSystem {
			continue
		}
		keep = append(keep, permission.ID)
	}

	if err := s.tx.WithTx(ctx, func(q *dbgen.Queries) error {
		if err := q.ClearRolePermissions(ctx, roleID); err != nil {
			return err
		}
		if len(keep) > 0 {
			if err := q.AssignRolePermissions(ctx, dbgen.AssignRolePermissionsParams{
				RoleID:        roleID,
				PermissionIds: keep,
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return database.MapError("access.replace_role_permissions", err)
	}

	_ = s.checker.InvalidateRole(ctx, roleID)
	return nil
}

// CreateDefaultRolesForTenant provisions the tenant Admin role (idempotent).
func (s *RoleService) CreateDefaultRolesForTenant(ctx context.Context, tenantID int64) error {
	if existing, err := s.roles.GetByName(ctx, tenantID, authz.AdminRole); err == nil && existing != nil {
		return nil
	} else if err != nil && !apperr.Is(err, apperr.ResourceNotFound) {
		return err
	}

	tenant := tenantID
	description := "Tenant administrator"
	role, err := s.roles.Create(ctx, CreateRoleInput{
		TenantID:    &tenant,
		Name:        authz.AdminRole,
		Description: &description,
	})
	if err != nil {
		return err
	}

	names := make([]string, 0, 32)
	for _, permission := range authz.AdminTenantPermissions() {
		names = append(names, permission.Name)
	}
	permissions, err := s.perms.GetByNames(ctx, names)
	if err != nil {
		return err
	}
	ids := make([]int64, 0, len(permissions))
	for _, permission := range permissions {
		ids = append(ids, permission.ID)
	}
	return s.roles.AssignPermissions(ctx, role.ID, ids)
}

func isReservedRoleName(name string) bool {
	return name == authz.SuperAdminRole || name == authz.DeveloperRole
}

// PermissionService implements permission business rules.
type PermissionService struct {
	perms   PermissionRepository
	checker *authz.Checker
}

// NewPermissionService creates the permission service.
func NewPermissionService(perms PermissionRepository, checker *authz.Checker) *PermissionService {
	return &PermissionService{perms: perms, checker: checker}
}

// List returns the full permission catalog.
func (s *PermissionService) List(ctx context.Context) ([]Permission, error) {
	return s.perms.List(ctx)
}

// ForUser returns permissions granted to a user through their roles.
func (s *PermissionService) ForUser(ctx context.Context, userID int64) ([]Permission, error) {
	return s.perms.ForUser(ctx, userID)
}

// Create adds a custom permission.
func (s *PermissionService) Create(ctx context.Context, name, group string, description *string) (*Permission, error) {
	existing, err := s.perms.GetByNames(ctx, []string{name})
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return nil, apperr.Validation(map[string][]string{"name": {"The name has already been taken."}})
	}
	return s.perms.Create(ctx, name, group, description)
}

// Delete removes a custom permission; system permissions are protected.
func (s *PermissionService) Delete(ctx context.Context, id int64) error {
	permission, err := s.perms.Get(ctx, id)
	if err != nil {
		return err
	}
	if permission.IsSystem {
		return apperr.New(apperr.Forbidden, "System permissions cannot be deleted.")
	}
	_ = s.checker.InvalidatePermission(ctx, id)
	return s.perms.Delete(ctx, id)
}

// SyncCatalog upserts the application permission catalog and removes system
// permissions that were retired from it.
func (s *PermissionService) SyncCatalog(ctx context.Context) (int64, error) {
	affected, err := s.perms.Upsert(ctx, authz.Catalog)
	if err != nil {
		return 0, err
	}
	keep := make([]string, 0, len(authz.Catalog))
	for _, permission := range authz.Catalog {
		keep = append(keep, permission.Name)
	}
	deleted, err := s.perms.DeleteStale(ctx, keep)
	if err != nil {
		return 0, err
	}
	return affected + deleted, nil
}
