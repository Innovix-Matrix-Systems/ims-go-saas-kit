package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
)

func TestLocalDiskContract(t *testing.T) {
	disk := newLocalDisk(DiskPublic, t.TempDir(), "https://api.test/storage", true)
	ctx := context.Background()
	const path = "files/abc123/photo.png"

	if exists, err := disk.Exists(ctx, path); err != nil || exists {
		t.Fatalf("Exists() before put = %v, %v", exists, err)
	}
	if err := disk.Put(ctx, path, strings.NewReader("binary"), 6); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if exists, err := disk.Exists(ctx, path); err != nil || !exists {
		t.Fatalf("Exists() after put = %v, %v", exists, err)
	}
	if size, err := disk.Size(ctx, path); err != nil || size != 6 {
		t.Fatalf("Size() = %d, %v", size, err)
	}
	reader, err := disk.Get(ctx, path)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	contents, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || string(contents) != "binary" {
		t.Fatalf("Get() contents = %q, %v", contents, err)
	}
	url, err := disk.URL(ctx, path)
	if err != nil || url != "https://api.test/storage/"+path {
		t.Fatalf("URL() = %q, %v", url, err)
	}
	if err := disk.Delete(ctx, path); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if exists, _ := disk.Exists(ctx, path); exists {
		t.Fatal("file still exists after delete")
	}
	// Deleting a missing file is idempotent.
	if err := disk.Delete(ctx, path); err != nil {
		t.Fatalf("Delete() missing error = %v", err)
	}
}

func TestLocalDiskNeutralizesTraversal(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	disk := newLocalDisk(DiskPublic, root, "", true)
	ctx := context.Background()

	// The path is cleaned onto the disk root; nothing escapes.
	if err := disk.Put(ctx, "../escape.txt", strings.NewReader("x"), 1); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if exists, _ := disk.Exists(ctx, "escape.txt"); !exists {
		t.Error("cleaned path not stored inside the disk root")
	}
	if _, err := os.Stat(filepath.Join(base, "escape.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file escaped the disk root: %v", err)
	}
}

func TestPrivateDiskHasNoPublicURL(t *testing.T) {
	disk := newLocalDisk(DiskPrivate, t.TempDir(), "", false)
	if _, err := disk.URL(context.Background(), "jobs/export/x.xlsx"); err == nil {
		t.Fatal("URL() on a private disk = nil error")
	}
}

func TestS3DiskKeysUseRoot(t *testing.T) {
	disk, err := newS3Disk(config.S3{Region: "auto", Bucket: "b", AccessKey: "k", SecretKey: "s",
		Endpoint: "https://acc.r2.cloudflarestorage.com", Root: "/uploads/", PublicURL: "https://cdn.test/"}, true)
	if err != nil {
		t.Fatalf("newS3Disk: %v", err)
	}
	if got := disk.key("/files/a.png"); got != "uploads/files/a.png" {
		t.Fatalf("key = %q", got)
	}
	url, err := disk.URL(context.Background(), "files/a.png")
	if err != nil || url != "https://cdn.test/uploads/files/a.png" {
		t.Fatalf("URL = %q, %v", url, err)
	}
	// No public URL: links are presigned and expire.
	private, _ := newS3Disk(config.S3{Region: "auto", Bucket: "b", AccessKey: "k", SecretKey: "s",
		Endpoint: "http://minio:9000", PathStyle: true}, false)
	signed, err := private.TemporaryURL(context.Background(), "files/a.png", time.Minute)
	if err != nil || !strings.HasPrefix(signed, "http://minio:9000/b/files/a.png?") || !strings.Contains(signed, "X-Amz-Expires=60") {
		t.Fatalf("TemporaryURL = %q, %v", signed, err)
	}
	if private.Public() {
		t.Fatal("private bucket reported public")
	}
}

func TestRegistryBuildsConfiguredDisks(t *testing.T) {
	cfg := &config.Config{App: config.App{URL: "https://api.test"}}
	cfg.Storage = config.Storage{Disks: []config.Disk{
		{Name: "private", Driver: config.DriverLocal, Root: t.TempDir()},
		{Name: "media", Driver: config.DriverS3, Public: true,
			S3: config.S3{Region: "auto", Bucket: "b", AccessKey: "k", SecretKey: "s", PublicURL: "https://cdn.test"}},
	}}
	registry, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got := strings.Join(registry.Names(), ","); got != "media,private" {
		t.Fatalf("names = %s", got)
	}
	media, _ := registry.Disk("media")
	private, _ := registry.Disk("private")
	if !media.Public() || private.Public() {
		t.Fatal("visibility not taken from config")
	}
	if _, err := private.TemporaryURL(context.Background(), "x", time.Minute); !errors.Is(err, ErrNoTemporaryURL) {
		t.Fatalf("local TemporaryURL error = %v", err)
	}
}
