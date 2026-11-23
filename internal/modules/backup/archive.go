package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrArchiveExists = errors.New("backup: archive already exists")

type Manifest struct {
	Name          string    `json:"name"`
	Environment   string    `json:"environment"`
	AppVersion    string    `json:"app_version"`
	Database      string    `json:"database"`
	PgDumpVersion string    `json:"pg_dump_version"`
	CreatedAt     time.Time `json:"created_at"`
	Scope         string    `json:"scope"`
}

type ArchiveInput struct {
	Name          string
	Environment   string
	AppVersion    string
	Database      string
	PgDumpVersion string
	Scope         string
	Filename      string
	DumpPath      string
	CreatedAt     time.Time
	IncludeFiles  bool
	FilesPaths    []string
}

type ArchiveResult struct {
	Path     string
	Filename string
	Size     int64
	SHA256   string
}

func createArchive(ctx context.Context, dir string, input ArchiveInput) (*ArchiveResult, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("backup: create backup directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("backup: secure backup directory: %w", err)
	}
	tmpDir := filepath.Join(dir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return nil, fmt.Errorf("backup: create temporary directory: %w", err)
	}
	if err := os.Chmod(tmpDir, 0o700); err != nil {
		return nil, fmt.Errorf("backup: secure temporary directory: %w", err)
	}
	tempFile, err := os.CreateTemp(tmpDir, "archive-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("backup: create temporary archive: %w", err)
	}
	tempPath := tempFile.Name()
	cleanup := func() {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
	}

	hash := sha256.New()
	gzipWriter := gzip.NewWriter(io.MultiWriter(tempFile, hash))
	tarWriter := tar.NewWriter(gzipWriter)
	ok := false
	defer func() {
		if !ok {
			cleanup()
		}
	}()

	manifest, err := json.Marshal(Manifest{
		Name:          input.Name,
		Environment:   input.Environment,
		AppVersion:    input.AppVersion,
		Database:      input.Database,
		PgDumpVersion: input.PgDumpVersion,
		CreatedAt:     input.CreatedAt.UTC(),
		Scope:         input.Scope,
	})
	if err != nil {
		return nil, fmt.Errorf("backup: encode manifest: %w", err)
	}
	if err := writeTarBytes(tarWriter, "manifest.json", manifest, input.CreatedAt); err != nil {
		return nil, err
	}
	if err := addFileToArchive(tarWriter, "database.dump", input.DumpPath); err != nil {
		return nil, err
	}
	if input.IncludeFiles {
		if err := addFilesToArchive(ctx, tarWriter, input.FilesPaths, dir); err != nil {
			return nil, err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return nil, fmt.Errorf("backup: close archive: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, fmt.Errorf("backup: close archive compression: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return nil, fmt.Errorf("backup: close archive file: %w", err)
	}

	finalPath := filepath.Join(dir, input.Filename)
	if _, err := os.Stat(finalPath); err == nil {
		return nil, ErrArchiveExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("backup: inspect archive target: %w", err)
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return nil, fmt.Errorf("backup: store archive: %w", err)
	}
	info, err := os.Stat(finalPath)
	if err != nil {
		return nil, fmt.Errorf("backup: stat archive: %w", err)
	}
	ok = true
	return &ArchiveResult{
		Path:     finalPath,
		Filename: input.Filename,
		Size:     info.Size(),
		SHA256:   hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func writeTarBytes(writer *tar.Writer, name string, contents []byte, modTime time.Time) error {
	header := &tar.Header{
		Name:     name,
		Mode:     0o600,
		Size:     int64(len(contents)),
		ModTime:  modTime,
		Typeflag: tar.TypeReg,
	}
	if err := writer.WriteHeader(header); err != nil {
		return fmt.Errorf("backup: write %s header: %w", name, err)
	}
	if _, err := writer.Write(contents); err != nil {
		return fmt.Errorf("backup: write %s: %w", name, err)
	}
	return nil
}

func addFileToArchive(writer *tar.Writer, name, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("backup: stat %s: %w", path, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("backup: open %s: %w", path, err)
	}
	defer file.Close()

	header := &tar.Header{
		Name:     name,
		Mode:     0o600,
		Size:     info.Size(),
		ModTime:  info.ModTime(),
		Typeflag: tar.TypeReg,
	}
	if err := writer.WriteHeader(header); err != nil {
		return fmt.Errorf("backup: write %s header: %w", name, err)
	}
	if _, err := io.Copy(writer, file); err != nil {
		return fmt.Errorf("backup: write %s: %w", name, err)
	}
	return nil
}

func addFilesToArchive(ctx context.Context, writer *tar.Writer, paths []string, backupDir string) error {
	cleanBackupDir := filepath.Clean(backupDir)
	for _, raw := range paths {
		root := filepath.Clean(raw)
		if root == "" || root == "." {
			return fmt.Errorf("backup: BACKUP_FILES_PATHS entry %q is invalid", raw)
		}
		info, err := os.Stat(root)
		if err != nil {
			return fmt.Errorf("backup: file path %q: %w", root, err)
		}
		if !info.IsDir() {
			if !shouldSkipBackupPath(root, info.Name(), cleanBackupDir) {
				if err := addFileToArchive(writer, filepath.ToSlash(filepath.Join("files", info.Name())), root); err != nil {
					return err
				}
			}
			continue
		}
		err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if path == root {
				return nil
			}
			if entry.IsDir() {
				if shouldSkipBackupPath(path, entry.Name(), cleanBackupDir) {
					return filepath.SkipDir
				}
				return nil
			}
			if !entry.Type().IsRegular() || shouldSkipBackupPath(path, entry.Name(), cleanBackupDir) {
				return nil
			}
			return addFileToArchive(writer, archiveNameForFile(root, path), path)
		})
		if err != nil {
			return fmt.Errorf("backup: archive files from %q: %w", root, err)
		}
	}
	return nil
}

func archiveNameForFile(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = filepath.Base(path)
	}
	rootName := filepath.Base(root)
	if rootName == "." || rootName == string(filepath.Separator) {
		return filepath.ToSlash(filepath.Join("files", rel))
	}
	return filepath.ToSlash(filepath.Join("files", rootName, rel))
}

func shouldSkipBackupPath(path, name, backupDir string) bool {
	switch name {
	case ".git", "node_modules", "tmp":
		return true
	}
	clean := filepath.Clean(path)
	if clean == backupDir || strings.HasPrefix(clean, backupDir+string(filepath.Separator)) {
		return true
	}
	return false
}
