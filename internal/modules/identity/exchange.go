package identity

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/mail"
	"path/filepath"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/access"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/dataprocessing"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/storage"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// Exchange job kinds and limits.
const (
	KindUserImport = "user.import"
	KindUserExport = "user.export"

	maxImportBytes   = 10 << 20
	importProgressAt = 100
	exportPageSize   = 500
)

// ExportRequest holds the user export filters; the snapshot is
// stored on the job row and replayed by the worker.
type ExportRequest struct {
	Search     string `json:"search"`
	IsActive   *bool  `json:"is_active"`
	RoleName   string `json:"role_name"`
	OrderBy    string `json:"order_by"`
	OrderDir   string `json:"order_direction"`
	AllTenants bool   `json:"all_tenants"`
	TenantID   int64  `json:"tenant_id"`
}

// ExchangeService runs the background user import/export flows.
type ExchangeService struct {
	users      *Service
	repo       Repository
	roles      access.RoleRepository
	processing *dataprocessing.Service
	disks      *storage.Registry
	queue      queue.Engine
	cfg        *config.Config
	logger     *slog.Logger
}

// NewExchangeService creates the user import/export service.
func NewExchangeService(users *Service, repo Repository, roles access.RoleRepository, processing *dataprocessing.Service, disks *storage.Registry, engine queue.Engine, cfg *config.Config, logger *slog.Logger) *ExchangeService {
	return &ExchangeService{users: users, repo: repo, roles: roles, processing: processing, disks: disks, queue: engine, cfg: cfg, logger: logger}
}

// StartImport stores the uploaded spreadsheet and queues the import job.
func (s *ExchangeService) StartImport(ctx context.Context, scope tenancy.Scope, actorID int64, header *multipart.FileHeader) (*dataprocessing.Job, error) {
	extension, err := validateImportHeader(header)
	if err != nil {
		return nil, err
	}

	job, err := s.processing.Create(ctx, scope, dataprocessing.TypeImport, entityTypePtr("user"), 0, nil, actorID)
	if err != nil {
		return nil, err
	}

	disk, err := s.disks.Disk(s.cfg.Processing.Disk)
	if err != nil {
		_ = s.processing.MarkFailed(ctx, job.ID, "The processing storage is not available.")
		return nil, apperr.Wrap(apperr.ServiceUnavailable, "The processing storage is not available.", err)
	}
	path := s.processingPath("imports", *job.QueueJobID+"."+extension)
	file, err := header.Open()
	if err != nil {
		_ = s.processing.MarkFailed(ctx, job.ID, "The uploaded file could not be read.")
		return nil, apperr.Wrap(apperr.InternalServerError, "The uploaded file could not be read.", err)
	}
	defer file.Close()
	if err := disk.Put(ctx, path, file, header.Size); err != nil {
		_ = s.processing.MarkFailed(ctx, job.ID, "The uploaded file could not be stored.")
		return nil, apperr.Wrap(apperr.ServiceUnavailable, "The uploaded file could not be stored.", err)
	}
	if err := s.processing.AttachFile(ctx, job.ID, dataprocessing.FileInput{
		Name: filepath.Base(header.Filename),
		Disk: s.cfg.Processing.Disk,
		Path: path,
		Size: header.Size,
		Mime: header.Header.Get("Content-Type"),
	}); err != nil {
		return nil, err
	}
	if err := s.queue.Enqueue(ctx, KindUserImport, userExchangePayload{JobID: job.ID}, queue.EnqueueOptions{}); err != nil {
		_ = s.processing.MarkFailed(ctx, job.ID, "The import job could not be queued.")
		return nil, apperr.Wrap(apperr.ServiceUnavailable, "The import job could not be queued.", err)
	}
	return job, nil
}

// StartExport snapshots the filters and queues the export job.
func (s *ExchangeService) StartExport(ctx context.Context, scope tenancy.Scope, actorID int64, filter ExportRequest) (*dataprocessing.Job, error) {
	applyScopeToExportFilter(&filter, scope)
	encoded, err := json.Marshal(filter)
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "The export filters could not be encoded.", err)
	}
	job, err := s.processing.Create(ctx, scope, dataprocessing.TypeExport, entityTypePtr("user"), 0, encoded, actorID)
	if err != nil {
		return nil, err
	}
	if err := s.queue.Enqueue(ctx, KindUserExport, userExchangePayload{JobID: job.ID}, queue.EnqueueOptions{}); err != nil {
		_ = s.processing.MarkFailed(ctx, job.ID, "The export job could not be queued.")
		return nil, apperr.Wrap(apperr.ServiceUnavailable, "The export job could not be queued.", err)
	}
	return job, nil
}

// ImportTemplate returns the CSV import template (heading + sample rows).
func (s *ExchangeService) ImportTemplate() string {
	return "name,email,phone,password,status,roles\n" +
		"\"John Doe\",\"john@example.com\",\"+8801700000000\",\"password123\",\"Active\",\"Admin,User\"\n" +
		"\"Jane Smith\",\"jane@example.com\",\"+8801800000000\",\"password123\",\"Active\",\"User\"\n" +
		"\"Bob Johnson\",\"bob@example.com\",\"\",\"password123\",\"Inactive\",\"User\"\n"
}

// ProcessImport executes the import job: parse the stored spreadsheet, create
// users row by row, and record per-row errors.
func (s *ExchangeService) ProcessImport(ctx context.Context, jobRowID int64) error {
	job, err := s.processing.Get(ctx, tenancy.Scope{Internal: true}, jobRowID)
	if err != nil {
		return err
	}
	if job.FilePath == nil || *job.FilePath == "" {
		return s.failJob(ctx, job, "Import file not found.")
	}
	if err := s.processing.MarkProcessing(ctx, job.ID); err != nil {
		return err
	}

	disk, err := s.diskFor(job.FileDisk)
	if err != nil {
		return s.failJob(ctx, job, "The processing storage is not available.")
	}
	reader, err := disk.Get(ctx, *job.FilePath)
	if err != nil {
		return s.failJob(ctx, job, fmt.Sprintf("Import file not found: %s", *job.FilePath))
	}

	rows, err := readSpreadsheet(reader, strings.TrimPrefix(filepath.Ext(*job.FilePath), "."))
	// Close before reading results so the source file can be deleted on
	// Windows (open handles block deletion there).
	_ = reader.Close()
	if err != nil {
		return s.failJob(ctx, job, "The import file could not be parsed: "+err.Error())
	}
	if len(rows) == 0 {
		if err := s.processing.UpdateProgress(ctx, job.ID, 0, 0, 0, nil); err != nil {
			return err
		}
		return s.processing.MarkCompleted(ctx, job.ID)
	}

	scope := jobScope(job)
	actorID := int64(0)
	if job.UserID != nil {
		actorID = *job.UserID
	}
	total := int32(len(rows) - 1)
	result, err := s.importRows(ctx, scope, actorID, job.BranchID, rows, func(processed, success int32, rowErrors []map[string]any) error {
		return s.processing.UpdateProgress(ctx, job.ID, total, processed, success, rowErrors)
	})
	if err != nil {
		return err
	}
	// Persist the final counters: progress callbacks are throttled
	// (importProgressAt), so small imports would otherwise finish with zeroed
	// totals and no per-row errors.
	if err := s.processing.UpdateProgress(ctx, job.ID, total, int32(result.Processed), int32(result.Success), result.Errors); err != nil {
		return err
	}
	// The source file is not kept after a successful run.
	_ = disk.Delete(ctx, *job.FilePath)
	return s.processing.MarkCompleted(ctx, job.ID)
}

// ImportResult summarises a synchronous import run.
type ImportResult struct {
	Total     int              `json:"total"`
	Processed int              `json:"processed"`
	Success   int              `json:"success"`
	Failed    int              `json:"failed"`
	Errors    []map[string]any `json:"errors"`
}

// importRows creates users row by row, optionally reporting progress. It is
// shared by the background job and the synchronous internal endpoint.
func (s *ExchangeService) importRows(ctx context.Context, scope tenancy.Scope, actorID int64, branchID *int64, rows [][]string, onProgress func(processed, success int32, rowErrors []map[string]any) error) (ImportResult, error) {
	result := ImportResult{Errors: make([]map[string]any, 0)}
	if len(rows) == 0 {
		return result, nil
	}
	headings := normalizeHeadings(rows[0])
	result.Total = len(rows) - 1
	for index, row := range rows[1:] {
		record := rowRecord(headings, row)
		result.Processed++
		if rowIsEmpty(record) {
			continue
		}
		if err := s.importRow(ctx, scope, actorID, branchID, record); err != nil {
			result.Errors = append(result.Errors, map[string]any{
				"row":    index + 2,
				"errors": rowErrorsFor(err),
			})
		} else {
			result.Success++
		}
		if onProgress != nil && result.Processed%importProgressAt == 0 {
			if err := onProgress(int32(result.Processed), int32(result.Success), result.Errors); err != nil {
				return result, err
			}
		}
	}
	result.Failed = len(result.Errors)
	return result, nil
}

// ImportUsers runs an import synchronously from an uploaded spreadsheet.
func (s *ExchangeService) ImportUsers(ctx context.Context, scope tenancy.Scope, actorID int64, header *multipart.FileHeader) (ImportResult, error) {
	extension, err := validateImportHeader(header)
	if err != nil {
		return ImportResult{}, err
	}
	file, err := header.Open()
	if err != nil {
		return ImportResult{}, apperr.Wrap(apperr.InternalServerError, "The uploaded file could not be read.", err)
	}
	defer file.Close()
	rows, err := readSpreadsheet(file, extension)
	if err != nil {
		return ImportResult{}, apperr.Validation(map[string][]string{"file": {"The import file could not be parsed."}})
	}
	return s.importRows(ctx, scope, actorID, nil, rows, nil)
}

// ExportUsers runs an export synchronously and returns the xlsx bytes.
func (s *ExchangeService) ExportUsers(ctx context.Context, scope tenancy.Scope, filter ExportRequest) ([]byte, error) {
	applyScopeToExportFilter(&filter, scope)
	return s.buildUserExport(ctx, filter, nil)
}

// ProcessExport executes the export job: stream the filtered users into an
// xlsx artifact and attach it to the job.
func (s *ExchangeService) ProcessExport(ctx context.Context, jobRowID int64) error {
	job, err := s.processing.Get(ctx, tenancy.Scope{Internal: true}, jobRowID)
	if err != nil {
		return err
	}
	if err := s.processing.MarkProcessing(ctx, job.ID); err != nil {
		return err
	}

	filter := ExportRequest{OrderBy: "created_at", OrderDir: "desc"}
	if len(job.Filters) > 0 {
		if err := json.Unmarshal(job.Filters, &filter); err != nil {
			return s.failJob(ctx, job, "The export filters are invalid.")
		}
	}
	applyScopeToExportFilter(&filter, jobScope(job))

	data, err := s.buildUserExport(ctx, filter, func(processed int32) error {
		return s.processing.UpdateProgress(ctx, job.ID, processed, processed, processed, nil)
	})
	if err != nil {
		return s.failJob(ctx, job, "The export file could not be generated.")
	}
	disk, err := s.diskFor(nil)
	if err != nil {
		return s.failJob(ctx, job, "The processing storage is not available.")
	}
	fileName := fmt.Sprintf("user_export_%s.xlsx", *job.QueueJobID)
	path := s.processingPath("export", fileName)
	if err := disk.Put(ctx, path, bytes.NewReader(data), int64(len(data))); err != nil {
		return s.failJob(ctx, job, "The export file could not be stored.")
	}
	if err := s.processing.AttachFile(ctx, job.ID, dataprocessing.FileInput{
		Name: fileName,
		Disk: s.cfg.Processing.Disk,
		Path: path,
		Size: int64(len(data)),
		Mime: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	}); err != nil {
		return err
	}
	return s.processing.MarkCompleted(ctx, job.ID)
}

// applyScopeToExportFilter pins the export to the effective scope.
func applyScopeToExportFilter(filter *ExportRequest, scope tenancy.Scope) {
	if filter.OrderBy == "" {
		filter.OrderBy = "created_at"
	}
	if filter.OrderDir == "" {
		filter.OrderDir = "desc"
	}
	if scope.Internal {
		filter.AllTenants = true
		filter.TenantID = 0
		return
	}
	filter.AllTenants = false
	filter.TenantID = scope.EffectiveTenantID()
}

// buildUserExport produces the xlsx artifact, optionally reporting progress
// after each page. Shared by the background job and the synchronous endpoint.
func (s *ExchangeService) buildUserExport(ctx context.Context, filter ExportRequest, onPage func(processed int32) error) ([]byte, error) {
	workbook := excelize.NewFile()
	sheet := workbook.GetSheetName(0)
	headings := []any{"ID", "Name", "Email", "Phone", "Status", "Roles", "Branch", "Created At"}
	if err := workbook.SetSheetRow(sheet, "A1", &headings); err != nil {
		return nil, err
	}

	exported := 0
	rowIndex := 2
	for {
		rows, err := s.repo.ListForExport(ctx, ExportFilter{
			AllTenants: filter.AllTenants,
			TenantID:   filter.TenantID,
			Search:     filter.Search,
			IsActive:   filter.IsActive,
			RoleName:   filter.RoleName,
			OrderBy:    filter.OrderBy,
			OrderDir:   filter.OrderDir,
			Limit:      exportPageSize,
			Offset:     int32(exported),
		})
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			values := []any{
				row.ID,
				safeCell(fallbackText(row.Name)),
				safeCell(fallbackText(row.Email)),
				safeCell(fallbackText(pointerText(row.Phone))),
				statusText(row.IsActive),
				safeCell(fallbackText(row.RoleNames)),
				safeCell(fallbackText(row.BranchName)),
				row.CreatedAt.Format("02-01-2006 15:04:05"),
			}
			cell := fmt.Sprintf("A%d", rowIndex)
			if err := workbook.SetSheetRow(sheet, cell, &values); err != nil {
				return nil, err
			}
			rowIndex++
		}
		exported += len(rows)
		if onPage != nil {
			if err := onPage(int32(exported)); err != nil {
				return nil, err
			}
		}
		if len(rows) < exportPageSize {
			break
		}
	}

	buffer := &bytes.Buffer{}
	if err := workbook.Write(buffer); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// RegisterJobs adds the user import/export job definitions to the registry.
func (s *ExchangeService) RegisterJobs(registry *queue.Registry) error {
	definitions := []queue.Definition{
		{
			Kind:        KindUserImport,
			Description: "Imports users from an uploaded spreadsheet (per-row errors tracked).",
			Queue:       queue.QueueHeavy,
			MaxRetries:  1,
			Timeout:     30 * time.Minute,
			SystemJob:   true,
			Validate:    validateExchangePayload,
			Handle: func(ctx context.Context, payload []byte) error {
				var input userExchangePayload
				if err := json.Unmarshal(payload, &input); err != nil {
					return err
				}
				return s.ProcessImport(ctx, input.JobID)
			},
		},
		{
			Kind:        KindUserExport,
			Description: "Exports users matching the job's filters into an xlsx artifact.",
			Queue:       queue.QueueHeavy,
			MaxRetries:  1,
			Timeout:     30 * time.Minute,
			SystemJob:   true,
			Validate:    validateExchangePayload,
			Handle: func(ctx context.Context, payload []byte) error {
				var input userExchangePayload
				if err := json.Unmarshal(payload, &input); err != nil {
					return err
				}
				return s.ProcessExport(ctx, input.JobID)
			},
		},
	}
	for _, definition := range definitions {
		if err := registry.Register(definition); err != nil {
			return err
		}
	}
	// A failed import or export can be retried from the jobs screen: the
	// same job row runs again (an import reads its uploaded file again).
	for jobType, kind := range map[string]string{dataprocessing.TypeImport: KindUserImport, dataprocessing.TypeExport: KindUserExport} {
		kind := kind
		dataprocessing.RegisterRerun(dataprocessing.RerunKey(jobType, "user"), func(ctx context.Context, job *dataprocessing.Job) error {
			return s.queue.Enqueue(tenancy.WithScope(ctx, jobScope(job)), kind, userExchangePayload{JobID: job.ID}, queue.EnqueueOptions{})
		})
	}
	return nil
}

type userExchangePayload struct {
	JobID int64 `json:"job_id"`
}

func validateExchangePayload(payload []byte) error {
	var value userExchangePayload
	if err := json.Unmarshal(payload, &value); err != nil {
		return apperr.Wrap(apperr.InvalidInput, "Invalid job payload.", err)
	}
	return nil
}

// importRow validates one spreadsheet row and creates the user.
func (s *ExchangeService) importRow(ctx context.Context, scope tenancy.Scope, actorID int64, branchID *int64, record map[string]string) error {
	name := strings.TrimSpace(record["name"])
	email := strings.TrimSpace(record["email"])
	password := strings.TrimSpace(record["password"])
	if password == "" {
		// Never share one well-known default across imported accounts: an
		// imported user without a password gets an unguessable one and sets
		// their own through the password-reset flow.
		random, err := randomPassword()
		if err != nil {
			return err
		}
		password = random
	}
	phone := strings.TrimSpace(record["phone"])
	status := strings.ToLower(strings.TrimSpace(record["status"]))
	roleNames := splitNames(record["roles"])

	fields := map[string][]string{}
	if name == "" {
		fields["name"] = append(fields["name"], "The name field is required.")
	} else if len([]rune(name)) < 2 {
		fields["name"] = append(fields["name"], "The name field must be at least 2 characters.")
	}
	if email == "" {
		fields["email"] = append(fields["email"], "The email field is required.")
	} else if _, err := mail.ParseAddress(email); err != nil || len(email) > 255 {
		fields["email"] = append(fields["email"], "The email field must be a valid email address.")
	}
	if err := authn.ValidateNewPassword(password); err != nil {
		var policy *apperr.Error
		if errors.As(err, &policy) {
			fields["password"] = append(fields["password"], policy.Fields["password"]...)
		}
	}
	if len(fields) > 0 {
		return apperr.Validation(fields)
	}

	var roleIDs []int64
	if len(roleNames) > 0 {
		tenantForRoles := int64(0)
		if !scope.Internal {
			tenantForRoles = scope.EffectiveTenantID()
		}
		idsByName, err := s.roles.IDsByNames(ctx, tenantForRoles, roleNames)
		if err != nil {
			return err
		}
		missing := make([]string, 0)
		for _, roleName := range roleNames {
			id, ok := idsByName[roleName]
			if !ok {
				missing = append(missing, roleName)
				continue
			}
			roleIDs = append(roleIDs, id)
		}
		if len(missing) > 0 {
			return apperr.Validation(map[string][]string{
				"roles": {"The selected roles is invalid."},
			})
		}
	}

	isActive := true
	if status != "" {
		isActive = status == "active"
	}
	var phonePtr *string
	if phone != "" {
		phonePtr = &phone
	}
	_, err := s.users.Create(ctx, scope, actorID, CreateParams{
		BranchID: branchID,
		Name:     name,
		Email:    email,
		Password: password,
		Phone:    phonePtr,
		IsActive: &isActive,
		Roles:    roleIDs,
	})
	return err
}

func (s *ExchangeService) failJob(ctx context.Context, job *dataprocessing.Job, message string) error {
	if s.logger != nil {
		s.logger.ErrorContext(ctx, "processing job failed", "job_id", derefString(job.QueueJobID), "message", message)
	}
	if err := s.processing.MarkFailed(ctx, job.ID, message); err != nil {
		return err
	}
	return apperr.New(apperr.InternalServerError, message)
}

func (s *ExchangeService) diskFor(jobDisk *string) (storage.Disk, error) {
	name := s.cfg.Processing.Disk
	if jobDisk != nil && *jobDisk != "" {
		name = *jobDisk
	}
	return s.disks.Disk(name)
}

func (s *ExchangeService) processingPath(kind, name string) string {
	base := strings.Trim(s.cfg.Processing.BasePath, "/")
	if base == "" {
		base = "jobs"
	}
	return base + "/" + kind + "/" + name
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

// readSpreadsheet parses xlsx or csv content into rows.
func readSpreadsheet(reader io.Reader, extension string) ([][]string, error) {
	switch strings.ToLower(extension) {
	case "csv":
		csvReader := csv.NewReader(reader)
		csvReader.FieldsPerRecord = -1
		csvReader.TrimLeadingSpace = true
		return csvReader.ReadAll()
	case "xlsx":
		// Bound decompression: a tiny crafted xlsx must not expand into
		// gigabytes in a worker (M76).
		workbook, err := excelize.OpenReader(reader, excelize.Options{
			UnzipSizeLimit:    64 << 20,
			UnzipXMLSizeLimit: 16 << 20,
		})
		if err != nil {
			return nil, err
		}
		defer workbook.Close()
		sheets := workbook.GetSheetList()
		if len(sheets) == 0 {
			return nil, errors.New("the workbook contains no sheets")
		}
		return workbook.GetRows(sheets[0], excelize.Options{RawCellValue: true})
	default:
		return nil, fmt.Errorf("unsupported spreadsheet format %q", extension)
	}
}

// normalizeHeadings slugs the heading row (lowercase, spaces/dashes → _).
func normalizeHeadings(headings []string) []string {
	normalized := make([]string, 0, len(headings))
	for _, heading := range headings {
		slug := strings.ToLower(strings.TrimSpace(heading))
		slug = strings.ReplaceAll(slug, " ", "_")
		slug = strings.ReplaceAll(slug, "-", "_")
		normalized = append(normalized, slug)
	}
	return normalized
}

func rowRecord(headings []string, row []string) map[string]string {
	record := make(map[string]string, len(headings))
	for index, heading := range headings {
		if heading == "" || index >= len(row) {
			continue
		}
		record[heading] = row[index]
	}
	return record
}

func rowIsEmpty(record map[string]string) bool {
	for _, value := range record {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func rowErrorsFor(err error) map[string]any {
	var appErr *apperr.Error
	if errors.As(err, &appErr) && len(appErr.Fields) > 0 {
		converted := make(map[string]any, len(appErr.Fields))
		for field, messages := range appErr.Fields {
			converted[field] = messages
		}
		return converted
	}
	return map[string]any{"general": err.Error()}
}

func validateImportHeader(header *multipart.FileHeader) (string, error) {
	if header == nil || header.Filename == "" {
		return "", apperr.Validation(map[string][]string{"file": {"The file field is required."}})
	}
	if header.Size > maxImportBytes {
		return "", apperr.Validation(map[string][]string{"file": {"The file field must not be greater than 10240 kilobytes."}})
	}
	extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(header.Filename), "."))
	switch extension {
	case "xlsx", "csv":
		return extension, nil
	case "xls":
		return "", apperr.Validation(map[string][]string{"file": {"Legacy .xls files are not supported; convert the file to .xlsx or .csv."}})
	default:
		return "", apperr.Validation(map[string][]string{"file": {"The file field must be a file of type: xlsx,csv."}})
	}
}

func splitNames(raw string) []string {
	parts := strings.Split(raw, ",")
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func entityTypePtr(value string) *string {
	return &value
}

func fallbackText(value string) string {
	if strings.TrimSpace(value) == "" {
		return "N/A"
	}
	return value
}

func statusText(active bool) string {
	if active {
		return "Active"
	}
	return "Inactive"
}

func pointerText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// randomPassword returns a 24-character URL-safe random password.
func randomPassword() (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", apperr.Wrap(apperr.InternalServerError, "generate password", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// safeCell neutralises spreadsheet formula injection (M77): user-controlled
// text starting with = + - @ (or a tab/CR) is prefixed with a quote so Excel
// and LibreOffice show it as text instead of evaluating it.
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
