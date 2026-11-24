// Package privacy runs account erasure and personal-data export
// (docs/design/account-deletion-and-export.md). The data itself is described
// by each module's privacy.Contributor (internal/privacy); this module keeps
// the ledger (privacy_requests), orders the work and cleans up after it.
package privacy

import (
	"encoding/json"
	"time"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	contracts "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/privacy"
)

// Request types.
const (
	TypeErasure = "erasure"
	TypeExport  = "export"
)

// Request statuses.
const (
	StatusScheduled  = "scheduled"
	StatusProcessing = "processing"
	StatusCompleted  = "completed"
	StatusCancelled  = "cancelled"
	StatusFailed     = "failed"
)

// Who asked.
const (
	SourceSelf      = "self"
	SourceAdmin     = "admin"
	SourceCLI       = "cli"
	SourceRetention = "retention"
)

// Activity actions. Every entry is id-only: it never repeats the person's
// details.
const (
	ActionUserErased   = "user.erased"
	ActionUserExported = "user.privacy_exported"
)

// Request is one row of the ledger.
type Request struct {
	ID           int64
	TenantID     *int64
	UserID       int64
	Type         string
	Status       string
	Source       string
	RequestedBy  *int64
	ScheduledFor *time.Time
	// ProcessingJobID is the export's data_processing_jobs row.
	ProcessingJobID *int64
	Report          Report
	Error           *string
	CompletedAt     *time.Time
	CreatedAt       time.Time
}

// Report is what an erasure did, per contributor: counts, never values.
type Report map[string]contracts.Report

// Totals sums the report.
func (r Report) Totals() contracts.Report {
	var total contracts.Report
	for _, part := range r {
		total.Add(part)
	}
	return total
}

func mapRequest(row dbgen.PrivacyRequest) Request {
	request := Request{
		ID: row.ID, TenantID: row.TenantID, UserID: row.UserID, Type: row.Type, Status: row.Status,
		Source: row.Source, RequestedBy: row.RequestedBy, Error: row.Error, ProcessingJobID: row.ProcessingJobID,
		ScheduledFor: contracts.Time(row.ScheduledFor), CompletedAt: contracts.Time(row.CompletedAt),
	}
	if row.CreatedAt.Valid {
		request.CreatedAt = row.CreatedAt.Time
	}
	if len(row.Report) > 0 {
		_ = json.Unmarshal(row.Report, &request.Report)
	}
	return request
}

// Manifest is the export's manifest.json.
type Manifest struct {
	SchemaVersion int       `json:"schema_version"`
	GeneratedAt   time.Time `json:"generated_at"`
	UserID        int64     `json:"user_id"`
	Contributors  []string  `json:"contributors"`
	Files         []string  `json:"files"`
}

// manifestSchemaVersion changes when the export layout does.
const manifestSchemaVersion = 1
