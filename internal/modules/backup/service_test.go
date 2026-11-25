package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/storage"
)

type fakeDumper struct {
	fail bool
}

func (d fakeDumper) Version(ctx context.Context) (string, error) {
	if d.fail {
		return "", errors.New("pg_dump not found")
	}
	return "17.1", nil
}

func (d fakeDumper) Dump(ctx context.Context, dsn, output string) (DumpInfo, error) {
	if d.fail {
		return DumpInfo{}, errors.New("pg_dump failed")
	}
	if err := os.WriteFile(output, []byte("dump-bytes"), 0o600); err != nil {
		return DumpInfo{}, err
	}
	return DumpInfo{Version: "17.1", Database: "ims_test"}, nil
}

type fakeDisk struct {
	objects map[string][]byte
	putErr  error
	deleted []string
}

func (d *fakeDisk) Put(ctx context.Context, path string, contents io.Reader, size int64) error {
	if d.putErr != nil {
		return d.putErr
	}
	data, err := io.ReadAll(contents)
	if err != nil {
		return err
	}
	d.objects[path] = data
	return nil
}

func (d *fakeDisk) Get(ctx context.Context, path string) (io.ReadCloser, error) {
	data, ok := d.objects[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (d *fakeDisk) Delete(ctx context.Context, path string) error {
	d.deleted = append(d.deleted, path)
	delete(d.objects, path)
	return nil
}

func (d *fakeDisk) Exists(ctx context.Context, path string) (bool, error) {
	_, ok := d.objects[path]
	return ok, nil
}

func (d *fakeDisk) Size(ctx context.Context, path string) (int64, error) {
	data, ok := d.objects[path]
	if !ok {
		return 0, os.ErrNotExist
	}
	return int64(len(data)), nil
}

func (d *fakeDisk) URL(ctx context.Context, path string) (string, error) {
	return "", nil
}

func (d *fakeDisk) TemporaryURL(context.Context, string, time.Duration) (string, error) {
	return "", storage.ErrNoTemporaryURL
}

func (d *fakeDisk) Public() bool { return false }

type fakeRegistry map[string]storage.Disk

func (r fakeRegistry) Disk(name string) (storage.Disk, error) {
	disk, ok := r[name]
	if !ok {
		return nil, errors.New("disk not configured")
	}
	return disk, nil
}

type fakeRepo struct {
	runs   map[int64]*Run
	nextID int64
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{runs: map[int64]*Run{}}
}

func (r *fakeRepo) Create(ctx context.Context, input CreateRunInput) (*Run, error) {
	r.nextID++
	run := &Run{
		ID:           r.nextID,
		UUID:         "00000000-0000-0000-0000-000000000000",
		Status:       StatusPending,
		Trigger:      input.Trigger,
		Filename:     input.Filename,
		Scope:        input.Scope,
		DatabaseName: input.DatabaseName,
		CreatedAt:    time.Now().UTC(),
	}
	r.runs[run.ID] = run
	return run, nil
}

func (r *fakeRepo) Get(ctx context.Context, id int64) (*Run, error) {
	run, ok := r.runs[id]
	if !ok {
		return nil, errors.New("run not found")
	}
	copy := *run
	return &copy, nil
}

func (r *fakeRepo) List(ctx context.Context, filter ListFilter) ([]Run, error) {
	return nil, nil
}

func (r *fakeRepo) Stats(ctx context.Context) (Stats, error) {
	var stats Stats
	for _, run := range r.runs {
		if run.Status == StatusDeleted {
			continue
		}
		stats.TotalRuns++
		stats.TotalSize += run.SizeBytes
	}
	return stats, nil
}

func (r *fakeRepo) SetRunning(ctx context.Context, id int64) (*Run, error) {
	run, ok := r.runs[id]
	if !ok {
		return nil, errors.New("run not found")
	}
	run.Status = StatusRunning
	copy := *run
	return &copy, nil
}

func (r *fakeRepo) Complete(ctx context.Context, id int64, input CompleteInput) (*Run, error) {
	run, ok := r.runs[id]
	if !ok {
		return nil, errors.New("run not found")
	}
	run.Status = input.Status
	run.Filename = input.Filename
	run.SizeBytes = input.SizeBytes
	run.ChecksumSHA256 = input.ChecksumSHA256
	run.PgDumpVersion = input.PgDumpVersion
	run.DatabaseName = input.DatabaseName
	run.Destinations = input.Destinations
	run.Error = input.ErrorMessage
	finished := time.Now().UTC()
	run.FinishedAt = &finished
	copy := *run
	return &copy, nil
}

func (r *fakeRepo) Fail(ctx context.Context, id int64, message string, destinations []DestinationResult) (*Run, error) {
	run, ok := r.runs[id]
	if !ok {
		return nil, errors.New("run not found")
	}
	run.Status = StatusFailed
	run.Error = message
	run.Destinations = destinations
	finished := time.Now().UTC()
	run.FinishedAt = &finished
	copy := *run
	return &copy, nil
}

func (r *fakeRepo) SoftDelete(ctx context.Context, id int64) (bool, error) {
	run, ok := r.runs[id]
	if !ok {
		return false, nil
	}
	run.Status = StatusDeleted
	return true, nil
}

func (r *fakeRepo) ForCleanup(ctx context.Context) ([]CleanupRun, error) {
	runs := make([]CleanupRun, 0, len(r.runs))
	for _, run := range r.runs {
		if run.Status == StatusDeleted {
			continue
		}
		runs = append(runs, CleanupRun{
			ID:           run.ID,
			Filename:     run.Filename,
			CreatedAt:    run.CreatedAt,
			SizeBytes:    run.SizeBytes,
			Destinations: run.Destinations,
		})
	}
	return runs, nil
}

func (r *fakeRepo) LatestSuccessful(ctx context.Context) (*Run, error) {
	return nil, errors.New("no successful backup")
}

func newTestService(t *testing.T, registry DiskRegistry, dumper Dumper) (*Service, *fakeRepo, string) {
	t.Helper()
	dir := t.TempDir()
	repo := newFakeRepo()
	service := NewService(Options{
		Config: config.Backup{
			Enabled:      true,
			Name:         "ims",
			Environment:  "test",
			Dir:          dir,
			Destinations: []string{DestinationLocal, DestinationS3},
			S3Prefix:     "backups",
			PgDumpPath:   "pg_dump",
			Timeout:      time.Minute,
		},
		Repository: repo,
		Disks:      registry,
		DSN:        "postgres://postgres:postgres@127.0.0.1:5432/ims_test?sslmode=disable",
		AppVersion: "test",
	})
	service.dumper = dumper
	return service, repo, dir
}

func TestServiceRunPartialAndDelete(t *testing.T) {
	disk := &fakeDisk{objects: map[string][]byte{}, putErr: errors.New("s3 unavailable")}
	service, _, dir := newTestService(t, fakeRegistry{"s3": disk}, fakeDumper{})

	run, err := service.RunNow(context.Background(), TriggerCLI, 0, RunOptions{})
	if err != nil {
		t.Fatalf("RunNow() error = %v", err)
	}
	if run.Status != StatusPartial {
		t.Fatalf("run status = %q, want partial", run.Status)
	}
	if len(run.Destinations) != 2 {
		t.Fatalf("destinations = %+v", run.Destinations)
	}
	if run.Destinations[0].Destination != DestinationLocal || run.Destinations[0].Status != StatusCompleted {
		t.Fatalf("local destination = %+v", run.Destinations[0])
	}
	if run.Destinations[1].Destination != DestinationS3 || run.Destinations[1].Status != StatusFailed {
		t.Fatalf("s3 destination = %+v", run.Destinations[1])
	}
	if _, err := os.Stat(filepath.Join(dir, run.Filename)); err != nil {
		t.Fatalf("local archive missing: %v", err)
	}

	if err := service.Delete(context.Background(), run.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, run.Filename)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("local archive still present: %v", err)
	}
}

func TestServiceRunCompletedAndDownload(t *testing.T) {
	disk := &fakeDisk{objects: map[string][]byte{}}
	service, _, _ := newTestService(t, fakeRegistry{"s3": disk}, fakeDumper{})

	run, err := service.RunNow(context.Background(), TriggerCLI, 0, RunOptions{})
	if err != nil {
		t.Fatalf("RunNow() error = %v", err)
	}
	if run.Status != StatusCompleted {
		t.Fatalf("run status = %q, want completed", run.Status)
	}
	if len(disk.objects) != 1 {
		t.Fatalf("s3 objects = %d, want 1", len(disk.objects))
	}
	download, err := service.Open(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer download.Reader.Close()
	data, err := io.ReadAll(download.Reader)
	if err != nil || len(data) == 0 {
		t.Fatalf("download = %d bytes, %v", len(data), err)
	}
}

func TestServiceRunFailsWithoutPgDump(t *testing.T) {
	service, repo, _ := newTestService(t, fakeRegistry{}, fakeDumper{fail: true})
	run, err := service.RunNow(context.Background(), TriggerCLI, 0, RunOptions{})
	if err == nil {
		t.Fatal("RunNow() error = nil, want failure")
	}
	if run == nil || run.Status != StatusFailed {
		t.Fatalf("run = %+v, want failed", run)
	}
	if repo.runs[run.ID].Error == "" {
		t.Fatal("failure reason not recorded")
	}
}

func TestServiceCleanRespectsDryRun(t *testing.T) {
	service, repo, dir := newTestService(t, fakeRegistry{}, fakeDumper{})
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	service.cfg.KeepAllDays = 7
	service.cfg.KeepDailyDays = 0
	service.cfg.KeepWeeklyWeeks = 0
	service.cfg.KeepMonthlyMonths = 0
	service.cfg.KeepYearlyYears = 0

	for _, age := range []time.Duration{time.Hour, 30 * 24 * time.Hour} {
		run, err := repo.Create(context.Background(), CreateRunInput{
			Trigger:  TriggerCLI,
			Filename: "backup-" + age.String() + ".tar.gz",
		})
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		repo.runs[run.ID].CreatedAt = now.Add(-age)
		if err := os.WriteFile(filepath.Join(dir, run.Filename), []byte("archive"), 0o600); err != nil {
			t.Fatalf("write archive: %v", err)
		}
	}

	preview, err := service.Clean(context.Background(), true)
	if err != nil {
		t.Fatalf("Clean(dry run) error = %v", err)
	}
	if preview.Candidates != 1 || preview.Deleted != 0 {
		t.Fatalf("dry run = %+v, want 1 candidate and no deletions", preview)
	}
	for _, run := range repo.runs {
		if run.Status == StatusDeleted {
			t.Fatal("dry run deleted a run")
		}
	}

	result, err := service.Clean(context.Background(), false)
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	if result.Deleted != 1 {
		t.Fatalf("clean result = %+v, want 1 deleted", result)
	}
}
