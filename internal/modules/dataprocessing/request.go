package dataprocessing

import (
	"encoding/json"
	"time"
)

// Response is the public processing-job representation; disk/path internals
// are omitted).
type Response struct {
	ID                 int64   `json:"id"`
	TenantID           *int64  `json:"tenant_id"`
	BranchID           *int64  `json:"branch_id"`
	JobID              *string `json:"job_id"`
	Type               string  `json:"type"`
	Status             string  `json:"status"`
	EntityType         *string `json:"entity_type"`
	Filters            any     `json:"filters"`
	FileName           *string `json:"file_name"`
	FileSize           *int64  `json:"file_size"`
	FileMime           *string `json:"file_mime"`
	TotalItems         int32   `json:"total_items"`
	ProcessedItems     int32   `json:"processed_items"`
	SuccessCount       int32   `json:"success_count"`
	ErrorCount         int32   `json:"error_count"`
	Errors             any     `json:"errors"`
	ErrorMessage       *string `json:"error_message"`
	ProgressPercentage int     `json:"progress_percentage"`
	UserID             *int64  `json:"user_id"`
	// UserName is who started the job.
	UserName    *string    `json:"user_name"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	// ExpiresAt is when the finished job and its file are deleted.
	ExpiresAt *time.Time `json:"expires_at"`
}

// NewResponse maps a job to its public representation.
func NewResponse(job Job) Response {
	return Response{
		ID:                 job.ID,
		TenantID:           job.TenantID,
		BranchID:           job.BranchID,
		JobID:              job.QueueJobID,
		Type:               job.Type,
		Status:             job.Status,
		EntityType:         job.EntityType,
		Filters:            rawJSON(job.Filters),
		FileName:           job.FileName,
		FileSize:           job.FileSize,
		FileMime:           job.FileMime,
		TotalItems:         job.TotalItems,
		ProcessedItems:     job.ProcessedItems,
		SuccessCount:       job.SuccessCount,
		ErrorCount:         job.ErrorCount,
		Errors:             rawJSON(job.Errors),
		ErrorMessage:       job.ErrorMessage,
		ProgressPercentage: job.ProgressPercentage(),
		UserID:             job.UserID,
		StartedAt:          job.StartedAt,
		CompletedAt:        job.CompletedAt,
		CreatedAt:          job.CreatedAt,
		UpdatedAt:          job.UpdatedAt,
	}
}

// CleanupSettingsRequest updates the per-scope retention window.
type CleanupSettingsRequest struct {
	Days int `json:"days" validate:"required,min=1"`
}

// CleanupSettingsResponse reports the effective retention window.
type CleanupSettingsResponse struct {
	Days int `json:"days"`
}

func rawJSON(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}
