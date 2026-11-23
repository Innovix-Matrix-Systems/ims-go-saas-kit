package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// localDisk stores files under a root directory on the server. A public local
// disk has a base URL it is served from (the built-in one: {APP_URL}/storage).
type localDisk struct {
	name      string
	root      string
	urlPrefix string
	public    bool
}

func newLocalDisk(name, root, baseURL string, public bool) *localDisk {
	return &localDisk{name: name, root: root, urlPrefix: strings.TrimRight(baseURL, "/"), public: public}
}

func (d *localDisk) Public() bool { return d.public }

func (d *localDisk) fullPath(path string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash("/" + strings.TrimLeft(path, "/")))
	target := filepath.Join(d.root, clean)
	root := filepath.Clean(d.root)
	if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("storage: path %q escapes disk %q root", path, d.name)
	}
	return target, nil
}

func (d *localDisk) Put(_ context.Context, path string, contents io.Reader, _ int64) error {
	target, err := d.fullPath(path)
	if err != nil {
		return err
	}
	// Private-disk contents are readable by the service user only (L2).
	dirMode, fileMode := os.FileMode(0o755), os.FileMode(0o644)
	if !d.public {
		dirMode, fileMode = 0o700, 0o600
	}
	if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
		return fmt.Errorf("storage: create directory for %q: %w", path, err)
	}
	file, err := os.OpenFile(target, os.O_RDWR|os.O_CREATE|os.O_TRUNC, fileMode)
	if err != nil {
		return fmt.Errorf("storage: create %q: %w", path, err)
	}
	if _, err := io.Copy(file, contents); err != nil {
		file.Close()
		_ = os.Remove(target)
		return fmt.Errorf("storage: write %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("storage: close %q: %w", path, err)
	}
	return nil
}

func (d *localDisk) Get(_ context.Context, path string) (io.ReadCloser, error) {
	target, err := d.fullPath(path)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(target)
	if err != nil {
		return nil, fmt.Errorf("storage: open %q: %w", path, err)
	}
	return file, nil
}

func (d *localDisk) Delete(_ context.Context, path string) error {
	target, err := d.fullPath(path)
	if err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("storage: delete %q: %w", path, err)
	}
	return nil
}

func (d *localDisk) Exists(_ context.Context, path string) (bool, error) {
	target, err := d.fullPath(path)
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("storage: stat %q: %w", path, err)
	}
	return true, nil
}

func (d *localDisk) Size(_ context.Context, path string) (int64, error) {
	target, err := d.fullPath(path)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return 0, fmt.Errorf("storage: stat %q: %w", path, err)
	}
	return info.Size(), nil
}

func (d *localDisk) URL(_ context.Context, path string) (string, error) {
	if !d.public || d.urlPrefix == "" {
		return "", fmt.Errorf("storage: disk %q has no public URL", d.name)
	}
	return d.urlPrefix + "/" + strings.TrimLeft(path, "/"), nil
}

// TemporaryURL is not available on local disks: private local files are
// streamed through the application's authorized download route.
func (d *localDisk) TemporaryURL(context.Context, string, time.Duration) (string, error) {
	return "", ErrNoTemporaryURL
}
