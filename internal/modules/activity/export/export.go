// Package activityexport exports the activity log as a background
// data-processing job. It lives apart from the activity module, which the
// settings module (and through it data processing) depends on.
package activityexport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/dataprocessing"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/storage"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

// Kind is the queue job that writes the export.
const Kind = "activity.export"

// entity names the data-processing job's entity type.
const entity = "activity"

const pageSize = 100

// Filters is what an export covers: the activity feed's filters, plus
// whether a staff member asked for the platform log (every organization and
// the system log). Stored on the job, so a retry exports the same rows.
type Filters struct {
	System      bool      `json:"system"`
	Tenant      string    `json:"tenant,omitempty"`
	SubjectType string    `json:"subject_type,omitempty"`
	ActorID     int64     `json:"actor_id,omitempty"`
	Action      string    `json:"action,omitempty"`
	From        time.Time `json:"from,omitempty"`
	To          time.Time `json:"to,omitempty"`
	Search      string    `json:"search,omitempty"`
}

// Exporter starts and runs activity exports.
type Exporter struct {
	feed       *activity.Service
	processing *dataprocessing.Service
	disks      *storage.Registry
	cfg        config.Processing
	queue      queue.Engine
	repo       Repository
	logger     *slog.Logger
}

// New creates the exporter.
func New(feed *activity.Service, processing *dataprocessing.Service, disks *storage.Registry, cfg config.Processing,
	engine queue.Engine, repo Repository, logger *slog.Logger) *Exporter {
	return &Exporter{feed: feed, processing: processing, disks: disks, cfg: cfg, queue: engine, repo: repo, logger: logger}
}

// RegisterJobs adds the export job and lets a failed export be retried.
func (e *Exporter) RegisterJobs(registry *queue.Registry) error {
	if err := registry.Register(queue.Definition{
		Kind:        Kind,
		Description: "Exports the activity log matching the job's filters into an xlsx artifact.",
		Queue:       queue.QueueHeavy,
		MaxRetries:  1,
		Timeout:     30 * time.Minute,
		SystemJob:   true,
		Validate: func(payload []byte) error {
			var input jobPayload
			if err := json.Unmarshal(payload, &input); err != nil || input.JobID < 1 {
				return apperr.New(apperr.InvalidInput, "The activity export payload is invalid.")
			}
			return nil
		},
		Handle: func(ctx context.Context, payload []byte) error {
			var input jobPayload
			if err := json.Unmarshal(payload, &input); err != nil {
				return err
			}
			return e.Process(ctx, input.JobID)
		},
	}); err != nil {
		return err
	}
	dataprocessing.RegisterRerun(dataprocessing.RerunKey(dataprocessing.TypeExport, entity), func(ctx context.Context, job *dataprocessing.Job) error {
		return e.queue.Enqueue(tenancy.WithScope(ctx, jobScope(job)), Kind, jobPayload{JobID: job.ID}, queue.EnqueueOptions{})
	})
	return nil
}

type jobPayload struct {
	JobID int64 `json:"job_id"`
}

// Start records the export job and queues it. A staff export (System,
// outside any organization) belongs to no organization.
func (e *Exporter) Start(ctx context.Context, scope tenancy.Scope, actorID int64, filters Filters) (_ *dataprocessing.Job, err error) {
	ctx, end := tracing.Begin(ctx, "activity.start_export", attribute.Int64("tenant.id", scope.EffectiveTenantID()))
	defer end(&err)
	// Fail early on a bad organization filter rather than in the worker.
	if _, err := e.feed.Feed(ctx, scope, activity.Reader{System: filters.System}, toFeedFilter(filters, "", 1)); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(filters)
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "The export filters could not be encoded.", err)
	}
	entityType := entity
	job, err := e.processing.Create(ctx, scope, dataprocessing.TypeExport, &entityType, 0, encoded, actorID)
	if err != nil {
		return nil, err
	}
	if err := e.queue.Enqueue(ctx, Kind, jobPayload{JobID: job.ID}, queue.EnqueueOptions{}); err != nil {
		_ = e.processing.MarkFailed(ctx, job.ID, "The export job could not be queued.")
		return nil, apperr.Wrap(apperr.ServiceUnavailable, "The export job could not be queued.", err)
	}
	return job, nil
}

func toFeedFilter(filters Filters, cursor string, perPage int) activity.Filter {
	return activity.Filter{
		SubjectType: filters.SubjectType, ActorID: filters.ActorID, Tenant: filters.Tenant, Action: filters.Action,
		From: filters.From, To: filters.To, Search: filters.Search, Cursor: cursor, PerPage: perPage,
	}
}

// Process writes every matching entry into an xlsx file and attaches it.
func (e *Exporter) Process(ctx context.Context, jobRowID int64) (err error) {
	ctx, end := tracing.Begin(ctx, "activity.process_export", attribute.Int64("job.id", jobRowID))
	defer end(&err)
	job, err := e.processing.Get(ctx, tenancy.Scope{Internal: true}, jobRowID)
	if err != nil {
		return err
	}
	if err := e.processing.MarkProcessing(ctx, job.ID); err != nil {
		return err
	}
	var filters Filters
	if len(job.Filters) > 0 {
		if err := json.Unmarshal(job.Filters, &filters); err != nil {
			return e.fail(ctx, job, "The export filters are invalid.")
		}
	}
	data, err := e.build(ctx, job, filters)
	if err != nil {
		return e.fail(ctx, job, "The export file could not be generated.")
	}
	disk, err := e.disks.Disk(e.cfg.Disk)
	if err != nil {
		return e.fail(ctx, job, "The processing storage is not available.")
	}
	name := fmt.Sprintf("activity_export_%s.xlsx", deref(job.QueueJobID))
	path := e.path(name)
	if err := disk.Put(ctx, path, bytes.NewReader(data), int64(len(data))); err != nil {
		return e.fail(ctx, job, "The export file could not be stored.")
	}
	if err := e.processing.AttachFile(ctx, job.ID, dataprocessing.FileInput{
		Name: name, Disk: e.cfg.Disk, Path: path, Size: int64(len(data)),
		Mime: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	}); err != nil {
		return err
	}
	return e.processing.MarkCompleted(ctx, job.ID)
}

func (e *Exporter) build(ctx context.Context, job *dataprocessing.Job, filters Filters) ([]byte, error) {
	scope := jobScope(job)
	if filters.System && job.TenantID == nil {
		scope = tenancy.Scope{}
	}
	reader := activity.Reader{System: filters.System}

	workbook := excelize.NewFile()
	sheet := workbook.GetSheetName(0)
	headings := []any{"When", "Who", "Acted by staff", "Action", "Subject", "IP", "Details"}
	if filters.System {
		headings = append([]any{"Organization"}, headings...)
	}
	if err := workbook.SetSheetRow(sheet, "A1", &headings); err != nil {
		return nil, err
	}
	names := map[int64]string{}
	row := 2
	written := int32(0)
	cursor := ""
	for {
		page, err := e.feed.Feed(ctx, scope, reader, toFeedFilter(filters, cursor, pageSize))
		if err != nil {
			return nil, err
		}
		if filters.System {
			e.loadNames(ctx, page.Items, names)
		}
		for _, item := range page.Items {
			values := []any{
				item.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
				safeCell(deref(item.ActorName)),
				staffText(item),
				safeCell(item.Action),
				safeCell(subject(item)),
				safeCell(deref(item.IP)),
				safeCell(string(item.Changes)),
			}
			if filters.System {
				organization := "System"
				if item.TenantID != nil {
					organization = names[*item.TenantID]
				}
				values = append([]any{safeCell(organization)}, values...)
			}
			if err := workbook.SetSheetRow(sheet, fmt.Sprintf("A%d", row), &values); err != nil {
				return nil, err
			}
			row++
		}
		written += int32(len(page.Items))
		total := int32(page.Total)
		if err := e.processing.UpdateProgress(ctx, job.ID, total, written, written, nil); err != nil {
			return nil, err
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	buffer := &bytes.Buffer{}
	if err := workbook.Write(buffer); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// loadNames fills in the organization names a page refers to.
func (e *Exporter) loadNames(ctx context.Context, items []activity.Item, names map[int64]string) {
	missing := []int64{}
	for _, item := range items {
		if item.TenantID != nil {
			if _, ok := names[*item.TenantID]; !ok {
				names[*item.TenantID] = "#" + strconv.FormatInt(*item.TenantID, 10)
				missing = append(missing, *item.TenantID)
			}
		}
	}
	if len(missing) == 0 {
		return
	}
	found, err := e.repo.TenantNames(ctx, missing)
	if err != nil {
		return
	}
	for id, name := range found {
		names[id] = name
	}
}

func (e *Exporter) fail(ctx context.Context, job *dataprocessing.Job, message string) error {
	if e.logger != nil {
		e.logger.ErrorContext(ctx, "activity export failed", "job_id", deref(job.QueueJobID), "message", message)
	}
	if err := e.processing.MarkFailed(ctx, job.ID, message); err != nil {
		return err
	}
	return apperr.New(apperr.InternalServerError, message)
}

func (e *Exporter) path(name string) string {
	base := strings.Trim(e.cfg.BasePath, "/")
	if base == "" {
		base = "jobs"
	}
	return base + "/export/" + name
}

func jobScope(job *dataprocessing.Job) tenancy.Scope {
	if job.TenantID == nil {
		return tenancy.Scope{Internal: true}
	}
	scope := tenancy.Scope{TenantID: *job.TenantID}
	if job.BranchID != nil {
		scope.BranchID = *job.BranchID
	}
	return scope
}

func subject(item activity.Item) string {
	if item.SubjectID == nil {
		return item.SubjectType
	}
	return fmt.Sprintf("%s #%d", item.SubjectType, *item.SubjectID)
}

func staffText(item activity.Item) string {
	if item.ImpersonatorName != nil {
		return safeCell(*item.ImpersonatorName)
	}
	if item.ImpersonatorID != nil {
		return "yes"
	}
	return ""
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// safeCell neutralises spreadsheet formula injection: text starting with
// = + - @ (or a tab/CR) is prefixed with a quote.
func safeCell(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	}
	return value
}
