// Package storage is the disk abstraction for file storage, like Laravel's
// Storage facade: code asks the Registry for a disk by name and calls
// Put/Get/Delete/URL on it, without knowing whether the bytes live on the
// server's disk or in an S3-compatible bucket (AWS S3, Cloudflare R2, MinIO,
// DigitalOcean Spaces, Backblaze B2). Disks are declared in config (see
// internal/config/storage.go), so switching providers is a .env change.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
)

// Built-in disk names.
const (
	DiskPrivate = "private"
	DiskPublic  = "public"
	DiskS3      = "s3"
)

// ErrNoTemporaryURL is returned by disks that cannot sign download links
// (local disks: serve the file through the application instead).
var ErrNoTemporaryURL = errors.New("storage: disk does not support temporary URLs")

// Disk stores and serves file contents. Paths are slash-separated keys
// ("files/<uuid>/photo.png"); every driver treats them the same way.
type Disk interface {
	Put(ctx context.Context, path string, contents io.Reader, size int64) error
	Get(ctx context.Context, path string) (io.ReadCloser, error)
	Delete(ctx context.Context, path string) error
	Exists(ctx context.Context, path string) (bool, error)
	Size(ctx context.Context, path string) (int64, error)
	// URL is the permanent public URL of a file on a public disk.
	URL(ctx context.Context, path string) (string, error)
	// TemporaryURL is a signed link that expires (Laravel's temporaryUrl),
	// or ErrNoTemporaryURL when the driver cannot sign links.
	TemporaryURL(ctx context.Context, path string, ttl time.Duration) (string, error)
	// Public reports whether files on this disk may be linked directly.
	// Private files must be read through the application.
	Public() bool
}

// Registry exposes the configured disks by name.
type Registry struct {
	disks map[string]Disk
}

// New builds a disk for every configured disk (built-ins plus STORAGE_DISKS).
func New(cfg *config.Config, logger *slog.Logger) (*Registry, error) {
	disks := map[string]Disk{}
	for _, disk := range cfg.Storage.AllDisks(cfg.App.URL) {
		built, err := build(disk)
		if err != nil {
			return nil, fmt.Errorf("storage: disk %q: %w", disk.Name, err)
		}
		disks[disk.Name] = built
	}
	if _, ok := disks[DiskS3]; !ok && logger != nil {
		logger.Debug("s3 disk not configured; S3_* settings are empty")
	}
	return &Registry{disks: disks}, nil
}

func build(disk config.Disk) (Disk, error) {
	switch disk.Driver {
	case config.DriverLocal:
		return newLocalDisk(disk.Name, disk.Root, disk.URL, disk.Public), nil
	case config.DriverS3:
		return newS3Disk(disk.S3, disk.Public)
	default:
		return nil, fmt.Errorf("driver %q is not supported", disk.Driver)
	}
}

// NewRegistry wraps already-built disks (tests, custom drivers).
func NewRegistry(disks map[string]Disk) *Registry {
	return &Registry{disks: disks}
}

// Disk returns a configured disk by name.
func (r *Registry) Disk(name string) (Disk, error) {
	disk, ok := r.disks[name]
	if !ok {
		return nil, fmt.Errorf("storage: disk %q is not configured", name)
	}
	return disk, nil
}

// Names lists the configured disk names.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.disks))
	for name := range r.disks {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
