// Package files implements the file resource (upload/list/show/delete) and
// file attachments (polymorphic owner_type/owner_id/collection with a
// collection registry), backed by the storage disk abstraction.
package files

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
)

// File types derived from the MIME type.
const (
	TypeImage    = "image"
	TypeVideo    = "video"
	TypeDocument = "document"
	TypeOther    = "other"
)

// Attachment owner kinds and collections.
const (
	OwnerUser         = "user"
	CollectionProfile = "profile"
	OwnerTenant       = "tenant"
	CollectionLogo    = "logo"
)

// collections is the collection registry: owner type → collection → single
// file? Future owners (supplier documents, …) register here; a single-file
// collection is replaced atomically by the service.
var collections = map[string]map[string]bool{
	OwnerUser: {
		CollectionProfile: true,
	},
	OwnerTenant: {
		CollectionLogo: true,
	},
}

// CollectionPolicy reports whether a collection keeps a single file and
// whether the owner/collection pair is registered.
func CollectionPolicy(ownerType, collection string) (single bool, known bool) {
	byOwner, ok := collections[ownerType]
	if !ok {
		return false, false
	}
	single, known = byOwner[collection]
	return single, known
}

// TypeFromMIME maps a MIME type to the file type (only
// image/*, video/* and application/pdf are classified).
func TypeFromMIME(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return TypeImage
	case strings.HasPrefix(mime, "video/"):
		return TypeVideo
	case mime == "application/pdf":
		return TypeDocument
	default:
		return TypeOther
	}
}

// File is the domain representation of a stored file.
type File struct {
	ID         int64
	UUID       uuid.UUID
	Name       string
	FileName   string
	Disk       string
	Path       string
	MimeType   string
	Size       int64
	Type       string
	OwnerType  *string
	OwnerID    *int64
	Collection *string
	TenantID   *int64
	CreatedBy  *int64
	UpdatedBy  *int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Attached reports whether the file belongs to an owner.
func (f File) Attached() bool {
	return f.OwnerType != nil && f.OwnerID != nil
}

// generatedOwnerType marks a background job's output (an export): it is
// listed with the uploads but lives and dies with its job.
const generatedOwnerType = "data_processing_job"

// Generated reports whether a background job wrote the file.
func (f File) Generated() bool {
	return f.OwnerType != nil && *f.OwnerType == generatedOwnerType
}

func mapFile(row dbgen.File) File {
	file := File{
		ID:         row.ID,
		Name:       row.Name,
		FileName:   row.FileName,
		Disk:       row.Disk,
		Path:       row.Path,
		MimeType:   row.MimeType,
		Size:       row.Size,
		Type:       row.Type,
		OwnerType:  row.OwnerType,
		OwnerID:    row.OwnerID,
		Collection: row.Collection,
		TenantID:   row.TenantID,
		CreatedBy:  row.CreatedBy,
		UpdatedBy:  row.UpdatedBy,
		CreatedAt:  row.CreatedAt.Time,
		UpdatedAt:  row.UpdatedAt.Time,
	}
	if row.Uuid.Valid {
		file.UUID = uuid.UUID(row.Uuid.Bytes)
	}
	return file
}

func uuidParam(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}
