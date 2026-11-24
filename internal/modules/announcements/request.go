package announcements

import "time"

// Request is what staff send to create or update an announcement.
type Request struct {
	Title       string     `json:"title" validate:"required,max=255"`
	Body        *string    `json:"body" validate:"omitempty,max=5000"`
	Kind        string     `json:"kind" validate:"required,oneof=info promotion warning critical"`
	CTALabel    *string    `json:"cta_label" validate:"omitempty,max=60"`
	CTAURL      *string    `json:"cta_url" validate:"omitempty,max=2048"`
	TenantIDs   []int64    `json:"tenant_ids" validate:"omitempty,max=1000,dive,gt=0"`
	StartsAt    *time.Time `json:"starts_at"`
	EndsAt      *time.Time `json:"ends_at"`
	Dismissible *bool      `json:"dismissible"`
	Status      string     `json:"status" validate:"omitempty,oneof=draft published archived"`
	Channels    []string   `json:"channels" validate:"omitempty,max=4,dive,oneof=database mail sms push"`
}

func (r Request) input() Input {
	dismissible := true
	if r.Dismissible != nil {
		dismissible = *r.Dismissible
	}
	return Input{
		Title: r.Title, Body: r.Body, Kind: r.Kind, CTALabel: r.CTALabel, CTAURL: r.CTAURL, TenantIDs: r.TenantIDs,
		StartsAt: r.StartsAt, EndsAt: r.EndsAt, Dismissible: dismissible, Status: r.Status, Channels: r.Channels,
	}
}

// EventRequest is a member's view, click or dismissal.
type EventRequest struct {
	Type string `json:"type" validate:"required,oneof=view click dismiss"`
}

// Response is an announcement.
type Response struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title"`
	Body        *string    `json:"body"`
	Kind        string     `json:"kind"`
	CTALabel    *string    `json:"cta_label"`
	CTAURL      *string    `json:"cta_url"`
	Audience    string     `json:"audience"`
	StartsAt    time.Time  `json:"starts_at"`
	EndsAt      *time.Time `json:"ends_at"`
	Dismissible bool       `json:"dismissible"`
	Status      string     `json:"status"`
	State       string     `json:"state"`
	Channels    []string   `json:"channels"`
	DeliveredAt *time.Time `json:"delivered_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func newResponse(a Announcement, now time.Time) Response {
	return Response{
		ID: a.ID, Title: a.Title, Body: a.Body, Kind: a.Kind, CTALabel: a.CTALabel, CTAURL: a.CTAURL, Audience: a.Audience,
		StartsAt: a.StartsAt, EndsAt: a.EndsAt, Dismissible: a.Dismissible, Status: a.Status, State: a.State(now),
		Channels: a.Channels, DeliveredAt: a.DeliveredAt, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
}

// ListedResponse is an announcement in the staff list.
type ListedResponse struct {
	Response
	TenantCount int64  `json:"tenant_count"`
	Totals      Totals `json:"totals"`
}

// DetailResponse is an announcement with its audience and counts.
type DetailResponse struct {
	Response
	Tenants []TenantRef `json:"tenants"`
	Totals  Totals      `json:"totals"`
}

// LiveResponse is what members see: the banner's content.
type LiveResponse struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title"`
	Body        *string    `json:"body"`
	Kind        string     `json:"kind"`
	CTALabel    *string    `json:"cta_label"`
	CTAURL      *string    `json:"cta_url"`
	Dismissible bool       `json:"dismissible"`
	StartsAt    time.Time  `json:"starts_at"`
	EndsAt      *time.Time `json:"ends_at"`
}

func newLiveResponse(a Announcement) LiveResponse {
	return LiveResponse{
		ID: a.ID, Title: a.Title, Body: a.Body, Kind: a.Kind, CTALabel: a.CTALabel, CTAURL: a.CTAURL,
		Dismissible: a.Dismissible, StartsAt: a.StartsAt, EndsAt: a.EndsAt,
	}
}
