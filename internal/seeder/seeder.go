// Package seeder provisions the application's baseline data idempotently:
// permission catalog, Super-Admin role, default tenant/branch, tenant Admin
// role and the super-admin user.
package seeder

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/access"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/branches"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/identity"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/tenants"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
)

// SuperAdminEmail is the seeded super-admin account e-mail.
const SuperAdminEmail = "superadmin@ims.com"

// DeveloperEmail is the seeded developer account e-mail. The account holds the
// hidden Developer role (developer-dashboard permissions only) and is the non-super-admin
// access path to the developer dashboard.
const DeveloperEmail = "developer@ims.com"

// Run executes the full, idempotent seed.
func Run(ctx context.Context, cfg *config.Config, queries *dbgen.Queries, tx *database.TxManager, checker *authz.Checker, logger *slog.Logger) error {
	permRepo := access.NewPermissionRepository(queries)
	roleRepo := access.NewRoleRepository(queries)
	roleSvc := access.NewRoleService(roleRepo, permRepo, checker, tx)
	permSvc := access.NewPermissionService(permRepo, checker)
	tenantRepo := tenants.NewRepository(queries)
	branchRepo := branches.NewRepository(queries)
	userRepo := identity.NewRepository(queries)

	upserted, err := permSvc.SyncCatalog(ctx)
	if err != nil {
		return fmt.Errorf("sync permission catalog: %w", err)
	}
	logger.Info("permission catalog synced", "affected", upserted)

	superRole, err := ensureSuperAdminRole(ctx, roleRepo)
	if err != nil {
		return err
	}
	if err := assignPermissions(ctx, roleRepo, permRepo, superRole.ID, authz.Catalog); err != nil {
		return err
	}

	devRole, err := ensureDeveloperRole(ctx, roleRepo)
	if err != nil {
		return err
	}
	if err := assignPermissions(ctx, roleRepo, permRepo, devRole.ID, authz.DeveloperPermissions()); err != nil {
		return err
	}

	tenant, err := ensureDefaultTenant(ctx, cfg, tenantRepo)
	if err != nil {
		return err
	}
	if _, err := branchRepo.CreateWithID(ctx, branches.CreateInput{
		ID:       cfg.Tenancy.DefaultBranchID,
		TenantID: tenant.ID,
		Name:     cfg.Tenancy.DefaultBranchName,
	}); err != nil {
		return fmt.Errorf("seed default branch: %w", err)
	}
	// Explicit-id seeding does not advance bigserial sequences; sync them so
	// the next auto-generated ids do not collide with the seeded rows.
	if err := tenantRepo.SyncIDSequence(ctx); err != nil {
		return fmt.Errorf("sync tenant id sequence: %w", err)
	}
	if err := branchRepo.SyncIDSequence(ctx); err != nil {
		return fmt.Errorf("sync branch id sequence: %w", err)
	}
	if err := roleSvc.CreateDefaultRolesForTenant(ctx, tenant.ID); err != nil {
		return fmt.Errorf("seed tenant roles: %w", err)
	}

	if err := ensureSuperAdminUser(ctx, cfg, userRepo, checker, superRole.ID, logger); err != nil {
		return err
	}
	if err := ensureDeveloperUser(ctx, cfg, userRepo, checker, devRole.ID, logger); err != nil {
		return err
	}

	logger.Info("seeding completed",
		"tenant_id", tenant.ID,
		"super_admin_role_id", superRole.ID,
		"developer_role_id", devRole.ID,
	)
	return nil
}

func ensureSuperAdminRole(ctx context.Context, roleRepo access.RoleRepository) (*access.Role, error) {
	role, err := roleRepo.GetSystemByName(ctx, authz.SuperAdminRole)
	if err == nil {
		return role, nil
	}
	if !apperr.Is(err, apperr.ResourceNotFound) {
		return nil, fmt.Errorf("lookup super admin role: %w", err)
	}
	description := "System super administrator"
	created, err := roleRepo.Create(ctx, access.CreateRoleInput{
		Name:        authz.SuperAdminRole,
		IsSystem:    true,
		Description: &description,
	})
	if err != nil {
		return nil, fmt.Errorf("create super admin role: %w", err)
	}
	return created, nil
}

func ensureDeveloperRole(ctx context.Context, roleRepo access.RoleRepository) (*access.Role, error) {
	role, err := roleRepo.GetSystemByName(ctx, authz.DeveloperRole)
	if err == nil {
		if !role.Hidden {
			if hiddenErr := roleRepo.SetHidden(ctx, role.ID, true); hiddenErr != nil {
				return nil, fmt.Errorf("mark developer role hidden: %w", hiddenErr)
			}
		}
		return role, nil
	}
	if !apperr.Is(err, apperr.ResourceNotFound) {
		return nil, fmt.Errorf("lookup developer role: %w", err)
	}
	description := "Developer dashboard access (hidden)"
	created, err := roleRepo.Create(ctx, access.CreateRoleInput{
		Name:        authz.DeveloperRole,
		IsSystem:    true,
		Hidden:      true,
		Description: &description,
	})
	if err != nil {
		return nil, fmt.Errorf("create developer role: %w", err)
	}
	return created, nil
}

func assignPermissions(ctx context.Context, roleRepo access.RoleRepository, permRepo access.PermissionRepository, roleID int64, set []authz.Permission) error {
	names := make([]string, 0, len(set))
	for _, permission := range set {
		names = append(names, permission.Name)
	}
	permissions, err := permRepo.GetByNames(ctx, names)
	if err != nil {
		return fmt.Errorf("load catalog permissions: %w", err)
	}
	ids := make([]int64, 0, len(permissions))
	for _, permission := range permissions {
		ids = append(ids, permission.ID)
	}
	if err := roleRepo.ClearPermissions(ctx, roleID); err != nil {
		return fmt.Errorf("clear permissions for role %d: %w", roleID, err)
	}
	if err := roleRepo.AssignPermissions(ctx, roleID, ids); err != nil {
		return fmt.Errorf("assign permissions to role %d: %w", roleID, err)
	}
	return nil
}

func ensureDefaultTenant(ctx context.Context, cfg *config.Config, tenantRepo tenants.Repository) (*tenants.Tenant, error) {
	tenant, err := tenantRepo.Get(ctx, cfg.Tenancy.DefaultTenantID)
	if err == nil {
		return tenant, nil
	}
	if !apperr.Is(err, apperr.ResourceNotFound) {
		return nil, fmt.Errorf("lookup default tenant: %w", err)
	}
	slug := cfg.Tenancy.DefaultTenantSlug
	email := cfg.Tenancy.DefaultTenantEmail
	var phone *string
	if cfg.Tenancy.DefaultTenantPhone != "" {
		value := cfg.Tenancy.DefaultTenantPhone
		phone = &value
	}
	created, err := tenantRepo.CreateWithID(ctx, tenants.CreateInput{
		ID:       cfg.Tenancy.DefaultTenantID,
		Name:     cfg.Tenancy.DefaultTenantName,
		Code:     cfg.Tenancy.DefaultTenantCode,
		Slug:     &slug,
		Email:    &email,
		Phone:    phone,
		IsActive: cfg.Tenancy.DefaultTenantActive,
	})
	if err != nil {
		return nil, fmt.Errorf("seed default tenant: %w", err)
	}
	if created == nil {
		return tenantRepo.Get(ctx, cfg.Tenancy.DefaultTenantID)
	}
	return created, nil
}

func ensureSuperAdminUser(ctx context.Context, cfg *config.Config, userRepo identity.Repository, checker *authz.Checker, roleID int64, logger *slog.Logger) error {
	user, err := userRepo.GetByEmail(ctx, SuperAdminEmail)
	if err == nil {
		if assignErr := userRepo.AssignRoles(ctx, user.ID, []int64{roleID}); assignErr != nil {
			return fmt.Errorf("assign super admin role: %w", assignErr)
		}
		checker.InvalidateUser(ctx, user.ID)
		return nil
	}
	if !apperr.Is(err, apperr.ResourceNotFound) {
		return fmt.Errorf("lookup super admin user: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.Auth.DefaultPassword), cfg.Auth.BcryptRounds)
	if err != nil {
		return fmt.Errorf("hash super admin password: %w", err)
	}
	created, err := userRepo.Create(ctx, identity.CreateInput{
		Name:            "Super Admin",
		Email:           SuperAdminEmail,
		Password:        string(hash),
		IsActive:        true,
		EmailVerifiedAt: time.Now(),
	})
	if err != nil {
		return fmt.Errorf("seed super admin user: %w", err)
	}
	if err := userRepo.AssignRoles(ctx, created.ID, []int64{roleID}); err != nil {
		return fmt.Errorf("assign super admin role: %w", err)
	}
	checker.InvalidateUser(ctx, created.ID)
	logger.Info("super admin user created", "email", SuperAdminEmail)
	return nil
}

func ensureDeveloperUser(ctx context.Context, cfg *config.Config, userRepo identity.Repository, checker *authz.Checker, roleID int64, logger *slog.Logger) error {
	user, err := userRepo.GetByEmail(ctx, DeveloperEmail)
	if err == nil {
		if assignErr := userRepo.AssignRoles(ctx, user.ID, []int64{roleID}); assignErr != nil {
			return fmt.Errorf("assign developer role: %w", assignErr)
		}
		checker.InvalidateUser(ctx, user.ID)
		return nil
	}
	if !apperr.Is(err, apperr.ResourceNotFound) {
		return fmt.Errorf("lookup developer user: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.Auth.DefaultPassword), cfg.Auth.BcryptRounds)
	if err != nil {
		return fmt.Errorf("hash developer password: %w", err)
	}
	created, err := userRepo.Create(ctx, identity.CreateInput{
		Name:            "Developer",
		Email:           DeveloperEmail,
		Password:        string(hash),
		IsActive:        true,
		EmailVerifiedAt: time.Now(),
	})
	if err != nil {
		return fmt.Errorf("seed developer user: %w", err)
	}
	if err := userRepo.AssignRoles(ctx, created.ID, []int64{roleID}); err != nil {
		return fmt.Errorf("assign developer role: %w", err)
	}
	checker.InvalidateUser(ctx, created.ID)
	logger.Info("developer user created", "email", DeveloperEmail)
	return nil
}
