package files

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/storage"
)

// bucketDisk stands in for an S3/R2/MinIO disk with a configurable visibility.
type bucketDisk struct{ public bool }

func (bucketDisk) Put(context.Context, string, io.Reader, int64) error { return nil }
func (bucketDisk) Get(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (bucketDisk) Delete(context.Context, string) error         { return nil }
func (bucketDisk) Exists(context.Context, string) (bool, error) { return true, nil }
func (bucketDisk) Size(context.Context, string) (int64, error)  { return 0, nil }
func (bucketDisk) URL(_ context.Context, path string) (string, error) {
	return "https://cdn.test/" + path, nil
}
func (bucketDisk) TemporaryURL(_ context.Context, path string, _ time.Duration) (string, error) {
	return "https://signed.test/" + path, nil
}
func (d bucketDisk) Public() bool { return d.public }

func TestVisibilityFollowsTheDiskNotItsName(t *testing.T) {
	registry := storage.NewRegistry(map[string]storage.Disk{
		"r2_private": bucketDisk{public: false},
		"r2_public":  bucketDisk{public: true},
	})

	private := NewService(nil, registry, "r2_private", nil, nil).WithPublicDisk("r2_public")
	if got := private.URL(context.Background(), File{Disk: "r2_private", Path: "files/a.pdf"}); got != "" {
		t.Fatalf("private bucket file URL = %q; want none (download route)", got)
	}
	if got := private.avatarDisk(); got != "r2_public" {
		t.Fatalf("avatar disk with a private FILES_DISK = %q; want r2_public", got)
	}

	public := NewService(nil, registry, "r2_public", nil, nil)
	if got := public.URL(context.Background(), File{Disk: "r2_public", Path: "files/a.pdf"}); got != "https://cdn.test/files/a.pdf" {
		t.Fatalf("public bucket file URL = %q", got)
	}
	if got := public.avatarDisk(); got != "r2_public" {
		t.Fatalf("avatar disk with a public FILES_DISK = %q", got)
	}
}
