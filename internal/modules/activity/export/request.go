package activityexport

import "time"

// Request is the POST /activity/export payload: the feed's filters.
type Request struct {
	TenantID    string     `json:"tenant_id"`
	SubjectType string     `json:"subject_type" validate:"omitempty,max=100"`
	ActorID     int64      `json:"actor_id" validate:"omitempty,gt=0"`
	Action      string     `json:"action" validate:"omitempty,max=150"`
	From        *time.Time `json:"from"`
	To          *time.Time `json:"to"`
	Search      string     `json:"search" validate:"omitempty,max=255"`
}
