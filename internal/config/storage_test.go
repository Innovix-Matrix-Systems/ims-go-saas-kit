package config

import (
	"strings"
	"testing"
)

func TestNamedStorageDisks(t *testing.T) {
	t.Setenv("APP_ENV_FILE", "")
	t.Setenv("STORAGE_DISKS", "r2,minio,archive")
	// Cloudflare R2: region defaults to "auto" when an endpoint is set.
	t.Setenv("STORAGE_R2_ENDPOINT", "https://acc.r2.cloudflarestorage.com")
	t.Setenv("STORAGE_R2_BUCKET", "media")
	t.Setenv("STORAGE_R2_ACCESS_KEY", "key")
	t.Setenv("STORAGE_R2_SECRET_KEY", "secret")
	t.Setenv("STORAGE_R2_PUBLIC_URL", "https://cdn.example.com/")
	t.Setenv("STORAGE_R2_ROOT", "/uploads/")
	// MinIO, private.
	t.Setenv("STORAGE_MINIO_ENDPOINT", "http://minio:9000")
	t.Setenv("STORAGE_MINIO_BUCKET", "files")
	t.Setenv("STORAGE_MINIO_ACCESS_KEY", "key")
	t.Setenv("STORAGE_MINIO_SECRET_KEY", "secret")
	t.Setenv("STORAGE_MINIO_PATH_STYLE", "true")
	// A second local disk.
	t.Setenv("STORAGE_ARCHIVE_DRIVER", "local")
	t.Setenv("STORAGE_ARCHIVE_ROOT", "archive")
	t.Setenv("FILES_DISK", "minio")
	t.Setenv("FILES_PUBLIC_DISK", "r2")

	cfg, err := Load("")
	if err == nil {
		err = cfg.Validate()
	}
	if err != nil {
		t.Fatalf("Load()+Validate() error = %v", err)
	}
	r2, ok := cfg.Storage.Disk("r2")
	if !ok || r2.Driver != DriverS3 || !r2.Public || r2.S3.Region != "auto" || r2.Root != "uploads" ||
		r2.S3.PublicURL != "https://cdn.example.com" {
		t.Fatalf("r2 disk = %+v", r2)
	}
	minio, ok := cfg.Storage.Disk("minio")
	if !ok || minio.Public || !minio.S3.PathStyle {
		t.Fatalf("minio disk = %+v", minio)
	}
	archive, ok := cfg.Storage.Disk("archive")
	if !ok || archive.Driver != DriverLocal || archive.Public || !strings.HasSuffix(archive.Root, "archive") {
		t.Fatalf("archive disk = %+v", archive)
	}
	if got := strings.Join(cfg.Storage.DiskNames(), ","); got != "private,public,r2,minio,archive" {
		t.Fatalf("disk names = %s", got)
	}
}

func TestStorageDiskValidation(t *testing.T) {
	cases := map[string]map[string]string{
		"unknown files disk":        {"FILES_DISK": "nope"},
		"private avatar disk":       {"FILES_PUBLIC_DISK": "private"},
		"incomplete bucket":         {"STORAGE_DISKS": "r2", "STORAGE_R2_BUCKET": "x"},
		"bad driver":                {"STORAGE_DISKS": "x", "STORAGE_X_DRIVER": "ftp"},
		"duplicate built-in":        {"STORAGE_DISKS": "public"},
		"bad visibility":            {"S3_VISIBILITY": "world"},
		"public local without URL":  {"STORAGE_DISKS": "a", "STORAGE_A_DRIVER": "local", "STORAGE_A_ROOT": "a", "STORAGE_A_VISIBILITY": "public"},
		"s3 files disk without env": {"FILES_DISK": "s3"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("APP_ENV_FILE", "")
			for key, value := range env {
				t.Setenv(key, value)
			}
			cfg, err := Load("")
			if err == nil {
				err = cfg.Validate()
			}
			if err == nil {
				t.Fatal("Load()+Validate() = nil error")
			}
		})
	}
}
