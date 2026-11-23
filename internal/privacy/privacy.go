// Package privacy is the registry behind account erasure and personal-data
// export (docs/design/account-deletion-and-export.md).
//
// Every module that stores data about a user declares it with a Contributor:
// how to export it and how to erase it. The privacy module
// (internal/modules/privacy) runs the contributors; it knows no table itself.
// A coverage test fails when a migration adds a user-reference column that no
// contributor claims and no exemption explains.
//
// This package is a leaf like authz or tenancy: modules import it to declare
// their part and never import the privacy module.
package privacy

import (
	"context"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
)

// Subject is the person whose data is exported or erased. Email and Phone are
// read before erasure, so contributors can match rows keyed by address (failed
// logins) rather than by id.
type Subject struct {
	UserID   int64
	TenantID *int64
	Name     string
	Email    string
	Phone    string
}

// Contributor exports and erases one module's data about a user.
type Contributor interface {
	// Name is the export folder and the report key, e.g. "notifications".
	Name() string
	// Columns lists the user-reference columns this contributor handles, as
	// "table.column" ("*.column" for a column name on every table). The
	// coverage test checks the schema against these lists.
	Columns() []string
	// Export writes the subject's records through w. It never writes secrets
	// (password hashes, token hashes, 2FA secrets, recovery codes).
	Export(ctx context.Context, q *dbgen.Queries, s Subject, w Writer) error
	// Erase deletes or anonymizes inside the caller's transaction and
	// reports counts, never values. Storage objects to remove after the
	// commit go in Report.Objects.
	Erase(ctx context.Context, q *dbgen.Queries, s Subject) (Report, error)
}

// Guard can refuse an erasure (the last tenant admin, a legal hold). Each
// reason is shown to whoever asked.
type Guard interface {
	CheckErasure(ctx context.Context, q *dbgen.Queries, s Subject) ([]string, error)
}

// Writer receives an export: JSON documents and raw files, named relative to
// the contributor's folder.
type Writer interface {
	JSON(name string, v any) error
	File(name string, r io.Reader) error
}

// Report is what one contributor's Erase did.
type Report struct {
	Deleted    int64    `json:"deleted"`
	Anonymized int64    `json:"anonymized"`
	Objects    []Object `json:"-"`
}

// Add folds another report into this one.
func (r *Report) Add(other Report) {
	r.Deleted += other.Deleted
	r.Anonymized += other.Anonymized
	r.Objects = append(r.Objects, other.Objects...)
}

// Object is a stored file to delete once the erasure has committed.
type Object struct {
	Disk string
	Path string
}

// ErasedName is what an erased user (and an erased impersonator snapshot) is
// called from then on.
const ErasedName = "Deleted user"

// Time maps a nullable timestamp for an export document.
func Time(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time.UTC()
	return &t
}

// UserIDPtr returns the id as the nullable parameter some queries take.
func (s Subject) UserIDPtr() *int64 {
	id := s.UserID
	return &id
}
