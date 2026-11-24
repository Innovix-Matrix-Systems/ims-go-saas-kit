package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/storage"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/tracing"
)

// Size limits (uploads 100 MB, avatars 5 MB).
const (
	MaxFileSize   = 100 << 20
	MaxAvatarSize = 5 << 20
)

var allowedFileExtensions = map[string]bool{
	"pdf": true, "jpg": true, "jpeg": true, "png": true,
	"docx": true, "doc": true, "mp4": true, "mov": true, "mkv": true,
}

// SVG is deliberately not accepted: it can carry script (H17).
var allowedImageExtensions = map[string]bool{
	"jpg": true, "jpeg": true, "png": true, "bmp": true, "gif": true, "webp": true,
}

// StorageLimiter enforces the tenant's storage.mb plan limit (implemented by
// the billing guard).
type StorageLimiter interface {
	RequireStorage(ctx context.Context, tenantID, usedBytes, addBytes int64) error
}

// Service implements the file resource and attachment rules.
type Service struct {
	repo     Repository
	disks    *storage.Registry
	fileDisk string
	// publicDisk holds avatars when fileDisk is private (FILES_PUBLIC_DISK).
	publicDisk string
	tx         *database.TxManager
	logger     *slog.Logger
	storage    StorageLimiter
}

// avatarDisk is where profile photos live. Avatars are shown in <img> tags
// without a bearer token, so a private FILES_DISK keeps them on the public
// disk (FILES_PUBLIC_DISK); library files follow FILES_DISK.
func (s *Service) avatarDisk() string {
	if disk, err := s.disks.Disk(s.fileDisk); err == nil && disk.Public() {
		return s.fileDisk
	}
	return s.publicDisk
}

// WithPublicDisk sets the public disk used for avatars when FILES_DISK is
// private (default "public").
func (s *Service) WithPublicDisk(name string) *Service {
	if name != "" {
		s.publicDisk = name
	}
	return s
}

// WithStorageLimit enables the storage.mb quota check on uploads.
func (s *Service) WithStorageLimit(limiter StorageLimiter) *Service {
	s.storage = limiter
	return s
}

// NewService creates the file service.
func NewService(repo Repository, disks *storage.Registry, fileDisk string, tx *database.TxManager, logger *slog.Logger) *Service {
	return &Service{repo: repo, disks: disks, fileDisk: fileDisk, publicDisk: storage.DiskPublic, tx: tx, logger: logger}
}

// Create stores a standalone library file. tenantID stamps the effective
// tenant (0 for an internal upload — the row stays global and is only
// reachable through the internal file routes).
func (s *Service) Create(ctx context.Context, actorID int64, tenantID int64, header *multipart.FileHeader) (_ *File, err error) {
	ctx, end := tracing.Begin(ctx, "files.create",
		attribute.Int64("tenant.id", tenantID),
		attribute.Int64("file.size", header.Size),
	)
	defer end(&err)
	if s.storage != nil && tenantID != 0 {
		used, err := s.repo.UsedBytes(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		if err := s.storage.RequireStorage(ctx, tenantID, used, header.Size); err != nil {
			return nil, err
		}
	}
	uploaded, err := s.storeOnDisk(ctx, s.fileDisk, header, allowedFileExtensions, MaxFileSize, false)
	if err != nil {
		return nil, err
	}
	file, err := s.repo.Create(ctx, CreateInput{
		UUID:      uploaded.uuid,
		Name:      uploaded.name,
		FileName:  uploaded.fileName,
		Disk:      s.fileDisk,
		Path:      uploaded.path,
		MimeType:  uploaded.mime,
		Size:      uploaded.size,
		Type:      uploaded.fileType,
		TenantID:  tenantIDPtr(tenantID),
		CreatedBy: actorIDPtr(actorID),
	})
	if err != nil {
		s.removeFromDisk(ctx, s.fileDisk, uploaded.path)
		return nil, err
	}
	return file, nil
}

// UpdateAvatar replaces the user's profile file (single-file collection):
// the new file is stored first, old rows are swapped in one transaction, and
// the superseded disk files are removed afterwards. tenantID stamps the
// target user's tenant (0 keeps the row global).
func (s *Service) UpdateAvatar(ctx context.Context, actorID, userID int64, tenantID int64, header *multipart.FileHeader) (err error) {
	ctx, end := tracing.Begin(ctx, "files.update_avatar",
		attribute.Int64("tenant.id", tenantID),
		attribute.Int64("user.id", userID),
	)
	defer end(&err)
	if userID < 1 {
		return apperr.New(apperr.InvalidInput, "The provided identifier is invalid.")
	}
	_, err = s.replaceSingle(ctx, actorID, OwnerUser, userID, CollectionProfile, tenantID, header, "The profile file was replaced concurrently; please retry.")
	return err
}

// UpdateLogo replaces the organization's logo: an image on the public disk
// (e-mails link to it), attached to the tenant, so it is listed with the
// organization's files and counts against its storage.
func (s *Service) UpdateLogo(ctx context.Context, actorID, tenantID int64, header *multipart.FileHeader) (_ *File, err error) {
	ctx, end := tracing.Begin(ctx, "files.update_logo", attribute.Int64("tenant.id", tenantID))
	defer end(&err)
	if tenantID < 1 {
		return nil, apperr.New(apperr.Forbidden, "Only an organization has a logo.")
	}
	if s.storage != nil {
		used, err := s.repo.UsedBytes(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		if err := s.storage.RequireStorage(ctx, tenantID, used, header.Size); err != nil {
			return nil, err
		}
	}
	row, err := s.replaceSingle(ctx, actorID, OwnerTenant, tenantID, CollectionLogo, tenantID, header, "The logo was replaced concurrently; please retry.")
	if err != nil {
		return nil, err
	}
	file := mapFile(*row)
	return &file, nil
}

// replaceSingle stores an image on the public disk and swaps it in as the
// only file of an owner's single-file collection.
func (s *Service) replaceSingle(ctx context.Context, actorID int64, ownerType string, ownerID int64, collection string, tenantID int64, header *multipart.FileHeader, conflict string) (*dbgen.File, error) {
	disk := s.avatarDisk()
	uploaded, err := s.storeOnDisk(ctx, disk, header, allowedImageExtensions, MaxAvatarSize, true)
	if err != nil {
		return nil, err
	}

	actor := actorIDPtr(actorID)
	var created dbgen.File
	var superseded []dbgen.File
	err = s.tx.WithTx(ctx, func(q *dbgen.Queries) error {
		removed, err := q.DeleteFilesByOwner(ctx, dbgen.DeleteFilesByOwnerParams{
			OwnerType:  ownerType,
			OwnerID:    ownerID,
			Collection: collection,
		})
		if err != nil {
			return database.MapError("files.replace", err)
		}
		superseded = removed
		row, err := q.CreateFile(ctx, dbgen.CreateFileParams{
			Uuid:       uuidParam(uploaded.uuid),
			Name:       uploaded.name,
			FileName:   uploaded.fileName,
			Disk:       disk,
			Path:       uploaded.path,
			MimeType:   uploaded.mime,
			Size:       uploaded.size,
			Type:       uploaded.fileType,
			OwnerType:  &ownerType,
			OwnerID:    &ownerID,
			Collection: &collection,
			TenantID:   tenantIDPtr(tenantID),
			CreatedBy:  actor,
			UpdatedBy:  actor,
		})
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return apperr.New(apperr.Conflict, conflict)
			}
			return database.MapError("files.replace_create", err)
		}
		created = row
		return nil
	})
	if err != nil {
		s.removeFromDisk(ctx, disk, uploaded.path)
		return nil, err
	}

	for _, old := range superseded {
		s.removeFromDisk(ctx, old.Disk, old.Path)
	}
	return &created, nil
}

// Get loads a file by ID within the caller's tenant scope (allTenants is only
// true for tenantless system callers).
func (s *Service) Get(ctx context.Context, id int64, allTenants bool, tenantID int64) (*File, error) {
	return s.repo.GetInTenant(ctx, id, allTenants, tenantID)
}

// List returns files with pagination and filters (tenant-scoped by the filter).
func (s *Service) List(ctx context.Context, filter ListFilter) ([]File, int64, error) {
	items, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.Count(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Delete removes a file row and its disk file (attached files included; the
// owner simply loses the attachment). The delete is tenant-scoped: tenantless
// callers (allTenants) may delete any file; tenant-bound callers only their
// own tenant's.
func (s *Service) Delete(ctx context.Context, id int64, allTenants bool, tenantID int64) (err error) {
	ctx, end := tracing.Begin(ctx, "files.delete",
		attribute.Int64("tenant.id", tenantID),
		attribute.Int64("file.id", id),
	)
	defer end(&err)
	file, err := s.repo.GetInTenant(ctx, id, allTenants, tenantID)
	if err != nil {
		return err
	}
	if file.Generated() {
		return apperr.New(apperr.InvalidState, "This file belongs to a background job; remove the job instead.")
	}
	if err := s.repo.Delete(ctx, id, allTenants, tenantID); err != nil {
		return err
	}
	s.removeFromDisk(ctx, file.Disk, file.Path)
	return nil
}

func (s *Service) Open(ctx context.Context, id int64) (_ io.ReadCloser, _ *File, err error) {
	ctx, end := tracing.Begin(ctx, "files.open", attribute.Int64("file.id", id))
	defer end(&err)
	file, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	disk, err := s.disks.Disk(file.Disk)
	if err != nil {
		return nil, nil, apperr.Wrap(apperr.ServiceUnavailable, "The file storage is not available.", err)
	}
	reader, err := disk.Get(ctx, file.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, apperr.Wrap(apperr.ResourceNotFound, "The file content is not available.", err)
		}
		if exists, existsErr := disk.Exists(ctx, file.Path); existsErr == nil && !exists {
			return nil, nil, apperr.Wrap(apperr.ResourceNotFound, "The file content is not available.", err)
		}
		return nil, nil, apperr.Wrap(apperr.ServiceUnavailable, "The file content could not be read.", err)
	}
	return reader, file, nil
}

// URL resolves the public URL of a stored file; failures degrade to "".
// Files on a private disk (local or bucket) have no public URL; the handler
// points clients at the authorized download route instead.
func (s *Service) URL(ctx context.Context, file File) string {
	disk, err := s.disks.Disk(file.Disk)
	if err != nil {
		s.logWarn(ctx, "file disk unavailable", file, err)
		return ""
	}
	if !disk.Public() {
		return ""
	}
	url, err := disk.URL(ctx, file.Path)
	if err != nil {
		s.logWarn(ctx, "file URL resolution failed", file, err)
		return ""
	}
	return url
}

// PhotoURL resolves the profile photo URL for a user ("" when none).
func (s *Service) PhotoURL(ctx context.Context, userID int64) (string, error) {
	file, err := s.repo.GetByOwner(ctx, OwnerUser, userID, CollectionProfile)
	if err != nil {
		if apperr.Is(err, apperr.ResourceNotFound) {
			return "", nil
		}
		return "", err
	}
	return s.URL(ctx, *file), nil
}

// PhotoURLs resolves profile photo URLs for a set of users.
func (s *Service) PhotoURLs(ctx context.Context, userIDs []int64) (map[int64]string, error) {
	urls := make(map[int64]string, len(userIDs))
	if len(userIDs) == 0 {
		return urls, nil
	}
	files, err := s.repo.ListProfileFiles(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if file.OwnerID == nil {
			continue
		}
		urls[*file.OwnerID] = s.URL(ctx, file)
	}
	return urls, nil
}

type upload struct {
	uuid     uuid.UUID
	name     string
	fileName string
	path     string
	mime     string
	size     int64
	fileType string
}

// storeOnDisk validates the upload, writes it to its disk and returns the
// metadata needed for the file row.
func (s *Service) storeOnDisk(ctx context.Context, diskName string, header *multipart.FileHeader, allowed map[string]bool, maxSize int64, requireImage bool) (*upload, error) {
	if header == nil || header.Filename == "" {
		return nil, apperr.Validation(map[string][]string{"file": {"The file field is required."}})
	}
	if header.Size > maxSize {
		return nil, apperr.Validation(map[string][]string{
			"file": {fmt.Sprintf("The file field must not be greater than %d kilobytes.", maxSize>>10)},
		})
	}

	name := filepath.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
	extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	if !allowed[extension] {
		return nil, apperr.Validation(map[string][]string{
			"file": {"The file field must be a file of type: " + extensionList(allowed) + "."},
		})
	}
	fileName := sanitizeFileName(name)

	file, err := header.Open()
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "The uploaded file could not be read.", err)
	}
	defer file.Close()

	mime, err := mimetype.DetectReader(file)
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "The uploaded file could not be inspected.", err)
	}
	detected := mime.String()
	if requireImage && (!strings.HasPrefix(detected, "image/") || strings.Contains(detected, "svg")) {
		return nil, apperr.Validation(map[string][]string{"file": {"The file field must be an image."}})
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "The uploaded file could not be read.", err)
	}

	id := uuid.New()
	path := "files/" + id.String() + "/" + fileName
	disk, err := s.disks.Disk(diskName)
	if err != nil {
		return nil, apperr.Wrap(apperr.ServiceUnavailable, "The file storage is not available.", err)
	}
	if err := disk.Put(ctx, path, file, header.Size); err != nil {
		return nil, apperr.Wrap(apperr.ServiceUnavailable, "The file could not be stored.", err)
	}

	return &upload{
		uuid:     id,
		name:     name,
		fileName: fileName,
		path:     path,
		mime:     detected,
		size:     header.Size,
		fileType: TypeFromMIME(detected),
	}, nil
}

func (s *Service) removeFromDisk(ctx context.Context, diskName, path string) {
	disk, err := s.disks.Disk(diskName)
	if err != nil {
		s.logWarn(ctx, "file disk unavailable during cleanup", File{Disk: diskName, Path: path}, err)
		return
	}
	if err := disk.Delete(ctx, path); err != nil {
		s.logWarn(ctx, "file cleanup failed", File{Disk: diskName, Path: path}, err)
	}
}

func (s *Service) logWarn(ctx context.Context, message string, file File, err error) {
	if s.logger == nil {
		return
	}
	s.logger.WarnContext(ctx, message, "disk", file.Disk, "path", file.Path, "error", err)
}

// sanitizeFileName keeps a safe base name for storage paths.
func sanitizeFileName(raw string) string {
	name := filepath.Base(strings.ReplaceAll(raw, "\\", "/"))
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	clean := strings.Trim(b.String(), "._")
	if clean == "" {
		clean = "file"
	}
	if len(clean) > 180 {
		clean = clean[len(clean)-180:]
	}
	return clean
}

func extensionList(allowed map[string]bool) string {
	extensions := make([]string, 0, len(allowed))
	for extension := range allowed {
		extensions = append(extensions, extension)
	}
	sort.Strings(extensions)
	return strings.Join(extensions, ",")
}

func actorIDPtr(actorID int64) *int64 {
	if actorID < 1 {
		return nil
	}
	return &actorID
}

// tenantIDPtr maps the caller's effective tenant to the column (0 → NULL:
// the row is a global file owned by a tenantless platform account).
func tenantIDPtr(tenantID int64) *int64 {
	if tenantID < 1 {
		return nil
	}
	return &tenantID
}
