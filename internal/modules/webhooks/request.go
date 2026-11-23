package webhooks

import "time"

// CreateRequest is the endpoint create payload.
type CreateRequest struct {
	URL         string   `json:"url" validate:"required,max=2000"`
	Description string   `json:"description" validate:"max=255"`
	Events      []string `json:"events" validate:"required,min=1,max=100,dive,max=255"`
}

// UpdateRequest changes an endpoint; absent fields keep their value.
type UpdateRequest struct {
	URL         *string  `json:"url" validate:"omitempty,max=2000"`
	Description *string  `json:"description" validate:"omitempty,max=255"`
	Events      []string `json:"events" validate:"omitempty,min=1,max=100,dive,max=255"`
	IsActive    *bool    `json:"is_active"`
}

// Response is an endpoint (never its secret).
type Response struct {
	ID                  int64     `json:"id"`
	URL                 string    `json:"url"`
	Description         string    `json:"description"`
	Events              []string  `json:"events"`
	IsActive            bool      `json:"is_active"`
	ConsecutiveFailures int32     `json:"consecutive_failures"`
	DisabledReason      *string   `json:"disabled_reason"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// SecretResponse is an endpoint with its new signing secret (create and
// rotate only).
type SecretResponse struct {
	Response
	Secret string `json:"secret"`
}

// NewResponse maps an endpoint.
func NewResponse(endpoint Endpoint) Response {
	return Response{
		ID: endpoint.ID, URL: endpoint.URL, Description: endpoint.Description, Events: endpoint.Events,
		IsActive: endpoint.IsActive, ConsecutiveFailures: endpoint.ConsecutiveFailures,
		DisabledReason: endpoint.DisabledReason, CreatedAt: endpoint.CreatedAt, UpdatedAt: endpoint.UpdatedAt,
	}
}
