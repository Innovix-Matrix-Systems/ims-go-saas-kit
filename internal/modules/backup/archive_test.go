package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCreateArchiveContents(t *testing.T) {
	dir := t.TempDir()
	dumpPath := filepath.Join(dir, "source.dump")
	if err := os.WriteFile(dumpPath, []byte("custom-dump"), 0o600); err != nil {
		t.Fatalf("write dump: %v", err)
	}
	filesRoot := filepath.Join(t.TempDir(), "public")
	if err := os.MkdirAll(filepath.Join(filesRoot, "avatars"), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filesRoot, "avatars", "a.png"), []byte("png"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(filesRoot, "node_modules"), 0o755); err != nil {
		t.Fatalf("mkdir excluded: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filesRoot, "node_modules", "skip.js"), []byte("skip"), 0o644); err != nil {
		t.Fatalf("write excluded: %v", err)
	}

	result, err := createArchive(context.Background(), dir, ArchiveInput{
		Name:          "ims",
		Environment:   "test",
		AppVersion:    "1.2.3",
		Database:      "ims_test",
		PgDumpVersion: "17.1",
		Scope:         ScopeDatabaseFiles,
		Filename:      "ims-test-2026-10-21-120000.tar.gz",
		DumpPath:      dumpPath,
		CreatedAt:     time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
		IncludeFiles:  true,
		FilesPaths:    []string{filesRoot},
	})
	if err != nil {
		t.Fatalf("createArchive() error = %v", err)
	}
	if result.Size == 0 || result.SHA256 == "" {
		t.Fatalf("archive result = %+v", result)
	}
	if _, err := os.Stat(result.Path); err != nil {
		t.Fatalf("archive missing: %v", err)
	}

	entries := readArchiveEntries(t, result.Path)
	if entries["manifest.json"] == "" {
		t.Fatal("manifest.json missing")
	}
	if entries["database.dump"] != "custom-dump" {
		t.Fatalf("database.dump = %q", entries["database.dump"])
	}
	if entries["files/public/avatars/a.png"] != "png" {
		t.Fatalf("files entry = %q", entries["files/public/avatars/a.png"])
	}
	if _, ok := entries["files/public/node_modules/skip.js"]; ok {
		t.Fatal("node_modules must be excluded from the archive")
	}

	// The same filename cannot be archived twice.
	if _, err := createArchive(context.Background(), dir, ArchiveInput{
		Filename: "ims-test-2026-10-21-120000.tar.gz",
		DumpPath: dumpPath,
	}); err != ErrArchiveExists {
		t.Fatalf("second createArchive() error = %v, want ErrArchiveExists", err)
	}
}

func readArchiveEntries(t *testing.T, path string) map[string]string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer reader.Close()
	tarReader := tar.NewReader(reader)
	entries := map[string]string{}
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		contents, err := io.ReadAll(tarReader)
		if err != nil {
			t.Fatalf("read %s: %v", header.Name, err)
		}
		entries[header.Name] = string(contents)
	}
	return entries
}
