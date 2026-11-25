package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/privacy"
)

// PrivacyContributor exports the profile and tombstones the users row
// (docs/design/account-deletion-and-export.md D1). Register it last: every
// other contributor runs while the row still holds the subject's details.
//
// It also guards erasure: the last active Admin of a tenant cannot be erased
// until another admin is assigned (D9).
type PrivacyContributor struct{}

var (
	_ privacy.Contributor = PrivacyContributor{}
	_ privacy.Guard       = PrivacyContributor{}
)

// Name implements privacy.Contributor.
func (PrivacyContributor) Name() string { return "identity" }

// Columns implements privacy.Contributor. Audit stamps on every table point
// at the tombstone, which no longer identifies anyone, so they are kept.
func (PrivacyContributor) Columns() []string {
	return []string{
		"*.created_by",
		"*.updated_by",
		"users.invited_by",
		"payments.recorded_by",
	}
}

type privacyProfile struct {
	ID                   int64      `json:"id"`
	TenantID             *int64     `json:"tenant_id"`
	BranchID             *int64     `json:"branch_id"`
	Name                 string     `json:"name"`
	FirstName            *string    `json:"first_name"`
	LastName             *string    `json:"last_name"`
	UserName             *string    `json:"user_name"`
	Email                string     `json:"email"`
	EmailVerifiedAt      *time.Time `json:"email_verified_at"`
	Phone                *string    `json:"phone"`
	PhoneVerifiedAt      *time.Time `json:"phone_verified_at"`
	IsActive             bool       `json:"is_active"`
	LastLoginAt          *time.Time `json:"last_login_at"`
	LastActiveDevice     *string    `json:"last_active_device"`
	InvitedAt            *time.Time `json:"invited_at"`
	InvitationAcceptedAt *time.Time `json:"invitation_accepted_at"`
	CreatedAt            *time.Time `json:"created_at"`
	UpdatedAt            *time.Time `json:"updated_at"`
}

// Export implements privacy.Contributor. The password hash and remember
// token are credentials, not data about the person, and are never exported.
func (PrivacyContributor) Export(ctx context.Context, q *dbgen.Queries, s privacy.Subject, w privacy.Writer) error {
	user, err := q.GetUserForPrivacy(ctx, s.UserID)
	if err != nil {
		return database.MapError("identity.privacy_export", err)
	}
	return w.JSON("profile", privacyProfile{
		ID: user.ID, TenantID: user.TenantID, BranchID: user.BranchID,
		Name: user.Name, FirstName: user.FirstName, LastName: user.LastName, UserName: user.UserName,
		Email: user.Email, EmailVerifiedAt: privacy.Time(user.EmailVerifiedAt),
		Phone: user.Phone, PhoneVerifiedAt: privacy.Time(user.PhoneVerifiedAt),
		IsActive: user.IsActive == 1, LastLoginAt: privacy.Time(user.LastLoginAt),
		LastActiveDevice: user.LastActiveDevice,
		InvitedAt:        privacy.Time(user.InvitedAt), InvitationAcceptedAt: privacy.Time(user.InvitationAcceptedAt),
		CreatedAt: privacy.Time(user.CreatedAt), UpdatedAt: privacy.Time(user.UpdatedAt),
	})
}

// Erase implements privacy.Contributor: the tombstone. The password becomes
// a random value that is not a bcrypt hash, so no password ever matches it.
func (PrivacyContributor) Erase(ctx context.Context, q *dbgen.Queries, s privacy.Subject) (privacy.Report, error) {
	unusable := make([]byte, 32)
	if _, err := rand.Read(unusable); err != nil {
		return privacy.Report{}, err
	}
	affected, err := q.EraseUser(ctx, dbgen.EraseUserParams{ID: s.UserID, Password: "!erased:" + hex.EncodeToString(unusable)})
	if err != nil {
		return privacy.Report{}, database.MapError("identity.privacy_erase", err)
	}
	return privacy.Report{Anonymized: affected}, nil
}

// CheckErasure implements privacy.Guard: a tenant keeps at least one active
// Admin, so the last one cannot be erased. A deleted tenant has nothing left to
// administer, so its last Admin can be.
func (PrivacyContributor) CheckErasure(ctx context.Context, q *dbgen.Queries, s privacy.Subject) ([]string, error) {
	// An organization's owner hands it over before their account can go.
	owner, err := q.IsTenantOwner(ctx, s.UserIDPtr())
	if err != nil {
		return nil, database.MapError("identity.privacy_guard", err)
	}
	if owner {
		return []string{"This user owns an organization. Transfer ownership to another administrator first."}, nil
	}
	if s.TenantID == nil {
		return nil, nil
	}
	if _, err := q.GetTenant(ctx, *s.TenantID); errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, database.MapError("identity.privacy_guard", err)
	}
	roles, err := q.UserRoleNames(ctx, dbgen.UserRoleNamesParams{ModelType: authz.ModelTypeUser, ModelID: s.UserID})
	if err != nil {
		return nil, database.MapError("identity.privacy_guard", err)
	}
	isAdmin := false
	for _, role := range roles {
		if role == authz.AdminRole {
			isAdmin = true
			break
		}
	}
	if !isAdmin {
		return nil, nil
	}
	others, err := q.CountOtherActiveRoleHolders(ctx, dbgen.CountOtherActiveRoleHoldersParams{
		ModelType: authz.ModelTypeUser, RoleName: authz.AdminRole, TenantID: s.TenantID, UserID: s.UserID,
	})
	if err != nil {
		return nil, database.MapError("identity.privacy_guard", err)
	}
	if others == 0 {
		return []string{"This is the tenant's last active Admin. Assign the Admin role to another user first."}, nil
	}
	return nil, nil
}
