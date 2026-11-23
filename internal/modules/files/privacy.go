package files

import (
	"context"
	"fmt"
	"path"
	"time"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/storage"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/privacy"
)

// PrivacyContributor covers files attached to the user (the profile picture
// and any other owner_type = "user" collection). Files the user uploaded for
// other owners belong to the tenant and stay; their created_by points at the
// tombstone. Erasure deletes the rows in the transaction and hands the stored
// objects to the privacy service, which removes them after the commit.
type PrivacyContributor struct {
	disks *storage.Registry
}

var _ privacy.Contributor = PrivacyContributor{}

// NewPrivacyContributor creates the contributor; disks reads the files the
// export copies.
func NewPrivacyContributor(disks *storage.Registry) PrivacyContributor {
	return PrivacyContributor{disks: disks}
}

// Name implements privacy.Contributor.
func (PrivacyContributor) Name() string { return "files" }

// Columns implements privacy.Contributor.
func (PrivacyContributor) Columns() []string { return []string{"files.owner_id"} }

type privacyFile struct {
	Collection *string    `json:"collection"`
	Name       string     `json:"name"`
	MimeType   string     `json:"mime_type"`
	Size       int64      `json:"size"`
	ExportedAs string     `json:"exported_as,omitempty"`
	CreatedAt  *time.Time `json:"created_at"`
}

// Export implements privacy.Contributor: an index plus a copy of each file.
// A file whose object is missing is listed without a copy.
func (c PrivacyContributor) Export(ctx context.Context, q *dbgen.Queries, s privacy.Subject, w privacy.Writer) error {
	rows, err := q.ListFilesOwnedBy(ctx, dbgen.ListFilesOwnedByParams{OwnerType: OwnerUser, OwnerID: s.UserID})
	if err != nil {
		return database.MapError("files.privacy_export", err)
	}
	index := make([]privacyFile, 0, len(rows))
	for _, row := range rows {
		entry := privacyFile{Collection: row.Collection, Name: row.Name, MimeType: row.MimeType, Size: row.Size, CreatedAt: privacy.Time(row.CreatedAt)}
		exported, err := c.copyFile(ctx, row, w)
		if err != nil {
			return err
		}
		entry.ExportedAs = exported
		index = append(index, entry)
	}
	return w.JSON("index", index)
}

// copyFile writes the stored object into the export. A disk that is not
// configured or an object that is gone yields "" (listed, not copied); a
// failure to write the export is an error.
func (c PrivacyContributor) copyFile(ctx context.Context, row dbgen.File, w privacy.Writer) (string, error) {
	if c.disks == nil {
		return "", nil
	}
	disk, err := c.disks.Disk(row.Disk)
	if err != nil {
		return "", nil
	}
	reader, err := disk.Get(ctx, row.Path)
	if err != nil {
		return "", nil
	}
	defer reader.Close()
	name := fmt.Sprintf("%d%s", row.ID, path.Ext(row.FileName))
	if err := w.File(name, reader); err != nil {
		return "", err
	}
	return name, nil
}

// Erase implements privacy.Contributor.
func (PrivacyContributor) Erase(ctx context.Context, q *dbgen.Queries, s privacy.Subject) (privacy.Report, error) {
	rows, err := q.DeleteFilesOwnedBy(ctx, dbgen.DeleteFilesOwnedByParams{OwnerType: OwnerUser, OwnerID: s.UserID})
	if err != nil {
		return privacy.Report{}, database.MapError("files.privacy_erase", err)
	}
	report := privacy.Report{Deleted: int64(len(rows))}
	for _, row := range rows {
		report.Objects = append(report.Objects, privacy.Object{Disk: row.Disk, Path: row.Path})
	}
	return report, nil
}
