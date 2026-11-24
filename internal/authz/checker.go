package authz

import (
	"context"
	"fmt"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
)

// DefaultPermissionCacheTTL bounds permission-set staleness. Role/permission
// mutations invalidate the affected users eagerly.
const DefaultPermissionCacheTTL = 10 * time.Minute

// RoleRef is a slim role reference used in response DTOs.
type RoleRef struct {
	ID       int64
	Name     string
	IsSystem bool
}

// Checker answers authorization questions and owns the permission cache.
type Checker struct {
	queries *dbgen.Queries
	cache   cache.Store
	ttl     time.Duration
}

// NewChecker creates the authorization checker.
func NewChecker(queries *dbgen.Queries, store cache.Store) *Checker {
	return &Checker{queries: queries, cache: store, ttl: DefaultPermissionCacheTTL}
}

// IsSuperAdmin reports whether the user holds the Super-Admin system role.
// The answer is cached with the user's permission set.
func (c *Checker) IsSuperAdmin(ctx context.Context, userID int64) (bool, error) {
	access, err := c.accessFor(ctx, userID)
	if err != nil {
		return false, err
	}
	return access.Super, nil
}

// Can reports whether the user holds the permission (Super-Admin bypasses).
// One cache read answers both questions, so a warm request costs no query.
func (c *Checker) Can(ctx context.Context, userID int64, permission string) (bool, error) {
	access, err := c.accessFor(ctx, userID)
	if err != nil {
		return false, err
	}
	if access.Super {
		return true, nil
	}
	for _, name := range access.Permissions {
		if name == permission {
			return true, nil
		}
	}
	return false, nil
}

// Grants reports whether the user is a Super-Admin and the permission names
// they hold through their roles (one cache read).
func (c *Checker) Grants(ctx context.Context, userID int64) (bool, []string, error) {
	access, err := c.accessFor(ctx, userID)
	if err != nil {
		return false, nil, err
	}
	return access.Super, access.Permissions, nil
}

// MustCan returns FORBIDDEN unless the user holds the permission.
func (c *Checker) MustCan(ctx context.Context, userID int64, permission string) error {
	ok, err := c.Can(ctx, userID, permission)
	if err != nil {
		return err
	}
	if !ok {
		return apperr.New(apperr.InsufficientPermissions, "You do not have permission to perform this action.")
	}
	return nil
}

// MustBeSuperAdmin returns FORBIDDEN unless the user holds Super-Admin.
func (c *Checker) MustBeSuperAdmin(ctx context.Context, userID int64) error {
	ok, err := c.IsSuperAdmin(ctx, userID)
	if err != nil {
		return err
	}
	if !ok {
		return apperr.New(apperr.InsufficientPermissions, "You do not have permission to perform this action.")
	}
	return nil
}

// RoleRefs returns the user's roles for response DTOs.
func (c *Checker) RoleRefs(ctx context.Context, userID int64) ([]RoleRef, error) {
	rows, err := c.queries.UserRoleRefs(ctx, dbgen.UserRoleRefsParams{
		ModelType: ModelTypeUser,
		ModelID:   userID,
	})
	if err != nil {
		return nil, database.MapError("authz.role_refs", err)
	}
	refs := make([]RoleRef, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, RoleRef{ID: row.ID, Name: row.Name, IsSystem: row.IsSystem})
	}
	return refs, nil
}

// RoleRefsForUsers returns role refs for many users with one query (list
// pages); users without roles map to an empty slice.
func (c *Checker) RoleRefsForUsers(ctx context.Context, userIDs []int64) (map[int64][]RoleRef, error) {
	out := make(map[int64][]RoleRef, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	rows, err := c.queries.UserRoleRefsForUsers(ctx, dbgen.UserRoleRefsForUsersParams{
		ModelType: ModelTypeUser,
		UserIds:   userIDs,
	})
	if err != nil {
		return nil, database.MapError("authz.role_refs_batch", err)
	}
	for _, row := range rows {
		out[row.UserID] = append(out[row.UserID], RoleRef{ID: row.ID, Name: row.Name, IsSystem: row.IsSystem})
	}
	return out, nil
}

// InvalidateUser drops the cached permission set for one user.
func (c *Checker) InvalidateUser(ctx context.Context, userID int64) {
	_ = c.cache.Delete(ctx, permissionCacheKey(userID))
}

// InvalidateRole drops cached permission sets for every user holding the role.
func (c *Checker) InvalidateRole(ctx context.Context, roleID int64) error {
	userIDs, err := c.queries.ListUserIDsWithRole(ctx, dbgen.ListUserIDsWithRoleParams{
		ModelType: ModelTypeUser,
		RoleID:    roleID,
	})
	if err != nil {
		return database.MapError("authz.invalidate_role", err)
	}
	for _, userID := range userIDs {
		c.InvalidateUser(ctx, userID)
	}
	return nil
}

// InvalidatePermission drops cached permission sets for every user granted the
// permission through one of their roles.
func (c *Checker) InvalidatePermission(ctx context.Context, permissionID int64) error {
	userIDs, err := c.queries.UserIDsWithPermission(ctx, dbgen.UserIDsWithPermissionParams{
		ModelType:    ModelTypeUser,
		PermissionID: permissionID,
	})
	if err != nil {
		return database.MapError("authz.invalidate_permission", err)
	}
	for _, userID := range userIDs {
		c.InvalidateUser(ctx, userID)
	}
	return nil
}

// userAccess is what the checker caches per user: the Super-Admin flag and
// the permission names granted through roles.
type userAccess struct {
	Super       bool     `json:"super"`
	Permissions []string `json:"permissions"`
}

func (c *Checker) accessFor(ctx context.Context, userID int64) (userAccess, error) {
	key := permissionCacheKey(userID)
	var access userAccess
	// A cache outage degrades to the database instead of failing requests.
	if found, err := c.cache.Get(ctx, key, &access); err == nil && found {
		return access, nil
	}
	super, err := c.queries.UserHasRole(ctx, dbgen.UserHasRoleParams{
		ModelType: ModelTypeUser,
		ModelID:   userID,
		RoleName:  SuperAdminRole,
	})
	if err != nil {
		return userAccess{}, database.MapError("authz.super_admin", err)
	}
	names, err := c.queries.UserPermissionNames(ctx, dbgen.UserPermissionNamesParams{
		ModelType: ModelTypeUser,
		ModelID:   userID,
	})
	if err != nil {
		return userAccess{}, database.MapError("authz.permissions", err)
	}
	access = userAccess{Super: super, Permissions: names}
	_ = c.cache.Set(ctx, key, access, c.ttl)
	return access, nil
}

func permissionCacheKey(userID int64) string {
	return fmt.Sprintf("authz:access:user:%d", userID)
}
