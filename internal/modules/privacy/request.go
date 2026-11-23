package privacy

import "time"

// PasswordRequest confirms the caller's password before a deletion request
// or an export.
type PasswordRequest struct {
	Password string `json:"password" validate:"required,max=72"`
}

// EraseRequest is an admin's erasure of another user. Immediate skips the
// grace period.
type EraseRequest struct {
	Immediate bool `json:"immediate"`
}

// RequestResponse is one privacy request as the API shows it.
type RequestResponse struct {
	ID           int64      `json:"id"`
	UserID       int64      `json:"user_id"`
	Type         string     `json:"type"`
	Status       string     `json:"status"`
	Source       string     `json:"source"`
	ScheduledFor *time.Time `json:"scheduled_for"`
	CompletedAt  *time.Time `json:"completed_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

// NewRequestResponse maps a ledger row.
func NewRequestResponse(request Request) RequestResponse {
	return RequestResponse{
		ID: request.ID, UserID: request.UserID, Type: request.Type, Status: request.Status, Source: request.Source,
		ScheduledFor: request.ScheduledFor, CompletedAt: request.CompletedAt, CreatedAt: request.CreatedAt,
	}
}

// ExportJobResponse points at the queued export: poll
// /data-processing/job/{job_id}, then download it.
type ExportJobResponse struct {
	JobID   string `json:"job_id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}
