package identity

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
)

// User is the domain representation of a user.
type User struct {
	ID               int64
	TenantID         *int64
	BranchID         *int64
	ActiveBranchID   *int64
	FirstName        *string
	LastName         *string
	UserName         *string
	Name             string
	Email            string
	EmailVerifiedAt  *time.Time
	Phone            *string
	PhoneVerifiedAt  *time.Time
	Password         string
	IsActive         bool
	LastLoginAt      *time.Time
	LastActiveDevice *string
	// InvitedAt / InvitedBy: the latest invitation;
	// InvitationAcceptedAt: when the invitee set their password.
	InvitedAt            *time.Time
	InvitedBy            *int64
	InvitationAcceptedAt *time.Time
	// Locale is the language the user chose (a language code); nil = the
	// default.
	Locale    *string
	CreatedBy *int64
	UpdatedBy *int64
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

func mapUser(row dbgen.User) User {
	return User{
		ID:                   row.ID,
		TenantID:             row.TenantID,
		BranchID:             row.BranchID,
		ActiveBranchID:       row.ActiveBranchID,
		FirstName:            row.FirstName,
		LastName:             row.LastName,
		UserName:             row.UserName,
		Name:                 row.Name,
		Email:                row.Email,
		EmailVerifiedAt:      timePtr(row.EmailVerifiedAt),
		Phone:                row.Phone,
		PhoneVerifiedAt:      timePtr(row.PhoneVerifiedAt),
		Password:             row.Password,
		IsActive:             row.IsActive == 1,
		LastLoginAt:          timePtr(row.LastLoginAt),
		LastActiveDevice:     row.LastActiveDevice,
		InvitedAt:            timePtr(row.InvitedAt),
		InvitedBy:            row.InvitedBy,
		InvitationAcceptedAt: timePtr(row.InvitationAcceptedAt),
		CreatedBy:            row.CreatedBy,
		UpdatedBy:            row.UpdatedBy,
		CreatedAt:            row.CreatedAt.Time,
		UpdatedAt:            row.UpdatedAt.Time,
		DeletedAt:            timePtr(row.DeletedAt),
		Locale:               row.Locale,
	}
}

func (u User) TenantIDValue() int64 {
	if u.TenantID == nil {
		return 0
	}
	return *u.TenantID
}

func (u User) BranchIDValue() int64 {
	if u.BranchID == nil {
		return 0
	}
	return *u.BranchID
}

func (u User) ActiveBranchIDValue() int64 {
	if u.ActiveBranchID == nil {
		return 0
	}
	return *u.ActiveBranchID
}

// Invitation states.
const (
	InvitationNone     = "none"
	InvitationPending  = "pending"
	InvitationAccepted = "accepted"
)

// InvitationStatus is none (created with a password), pending (invited,
// no password set yet) or accepted.
func (u User) InvitationStatus() string {
	switch {
	case u.InvitationAcceptedAt != nil:
		return InvitationAccepted
	case u.InvitedAt != nil:
		return InvitationPending
	default:
		return InvitationNone
	}
}

func (u User) EmailVerified() bool {
	return u.EmailVerifiedAt != nil
}

func (u User) PhoneVerified() bool {
	return u.PhoneVerifiedAt != nil
}

func (u User) toRecord(tenantActive bool) *authn.UserRecord {
	return &authn.UserRecord{
		ID:             u.ID,
		Name:           u.Name,
		Email:          u.Email,
		Password:       u.Password,
		IsActive:       u.IsActive,
		EmailVerified:  u.EmailVerified(),
		PhoneVerified:  u.PhoneVerified(),
		TenantID:       u.TenantIDValue(),
		BranchID:       u.BranchIDValue(),
		ActiveBranchID: u.ActiveBranchIDValue(),
		TenantActive:   tenantActive,
		Locale:         derefString(u.Locale),
	}
}

func timePtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}

func timestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// optionalTimestamptz maps the zero time to SQL NULL.
func optionalTimestamptz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return timestamptz(t)
}

// invitedAt is now for a user created with an invitation.
func invitedAt(by *int64) pgtype.Timestamptz {
	if by == nil {
		return optionalTimestamptz(time.Time{})
	}
	return optionalTimestamptz(time.Now())
}
