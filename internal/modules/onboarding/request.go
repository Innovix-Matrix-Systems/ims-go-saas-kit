package onboarding

import "time"

// SignUpRequest is POST /api/v1/signup.
type SignUpRequest struct {
	Name             string  `json:"name" validate:"required,min=2,max=255"`
	Email            string  `json:"email" validate:"required,email,max=255"`
	Password         string  `json:"password" validate:"required,min=8,max=72"`
	OrganizationName string  `json:"organization_name" validate:"required,min=2,max=100"`
	Phone            *string `json:"phone" validate:"omitempty,max=50"`
	Locale           string  `json:"locale" validate:"omitempty,locale"`
	AcceptTerms      bool    `json:"accept_terms"`
}

// SignUpResponse is the same for every accepted request, whether or not the
// address already had an account.
type SignUpResponse struct {
	Message string `json:"message"`
}

// CompleteRequest is POST /api/v1/onboarding/complete.
type CompleteRequest struct {
	SkippedSteps []string `json:"skipped_steps" validate:"omitempty,max=10,dive,max=50"`
}

// ProgressResponse is the organization's first-run setup.
type ProgressResponse struct {
	Completed    bool       `json:"completed"`
	CompletedAt  *time.Time `json:"completed_at"`
	SkippedSteps []string   `json:"skipped_steps"`
	Source       string     `json:"source"`
	IsOwner      bool       `json:"is_owner"`
}

// NewProgressResponse maps progress for the caller.
func NewProgressResponse(progress Progress, callerID int64) ProgressResponse {
	return ProgressResponse{
		Completed:    progress.CompletedAt != nil,
		CompletedAt:  progress.CompletedAt,
		SkippedSteps: progress.SkippedSteps,
		Source:       progress.Source,
		IsOwner:      progress.OwnerUserID != nil && *progress.OwnerUserID == callerID,
	}
}
