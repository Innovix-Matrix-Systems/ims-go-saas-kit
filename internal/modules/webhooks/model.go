// Package webhooks delivers events to customers' systems (phase 26.8): a
// tenant subscribes URLs to events, and every matching entry of the
// activity log is POSTed to them, signed (Standard Webhooks), retried with
// backoff and logged. An endpoint that keeps failing is disabled.
package webhooks

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
)

// Delivery statuses.
const (
	StatusPending   = "pending"   // queued or waiting for a retry
	StatusSucceeded = "succeeded" // the endpoint answered 2xx
	StatusAbandoned = "abandoned" // every attempt failed
)

// EventTest is the event POST /webhook/{id}/test sends.
const EventTest = "webhook.test"

// events is the catalog of deliverable events: activity actions a tenant may
// subscribe to. Sign-ins, impersonation and dashboard actions are never in
// it. Register yours with RegisterEvent in the module that records them.
var events = map[string]string{
	"user.created":   "A user was created.",
	"branch.updated": "A branch was changed.",
	EventTest:        "A test event sent from the API.",
}

// RegisterEvent makes an activity action deliverable as a webhook event.
func RegisterEvent(action, description string) { events[action] = description }

// Event is one catalog entry.
type Event struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// Events lists the deliverable events, sorted.
func Events() []Event {
	out := make([]Event, 0, len(events))
	for name, description := range events {
		out = append(out, Event{Type: name, Description: description})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

func deliverable(action string) bool {
	_, ok := events[action]
	return ok
}

// Endpoint is a subscribed URL. Secret and PreviousSecret are sealed.
type Endpoint struct {
	ID                      int64
	TenantID                int64
	URL                     string
	Description             string
	Events                  []string
	Secret                  string
	PreviousSecret          *string
	PreviousSecretExpiresAt *time.Time
	IsActive                bool
	ConsecutiveFailures     int32
	DisabledReason          *string
	CreatedBy               *int64
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

// Delivery is one event sent to one endpoint, with its last attempt.
type Delivery struct {
	ID             int64           `json:"id"`
	EndpointID     int64           `json:"endpoint_id"`
	EventID        string          `json:"event_id"`
	EventType      string          `json:"event_type"`
	Payload        json.RawMessage `json:"payload"`
	Status         string          `json:"status"`
	Attempts       int32           `json:"attempts"`
	ResponseStatus *int32          `json:"response_status"`
	ResponseBody   *string         `json:"response_body"`
	DurationMs     *int32          `json:"duration_ms"`
	Error          *string         `json:"error"`
	NextAttemptAt  *time.Time      `json:"next_attempt_at"`
	CreatedAt      time.Time       `json:"created_at"`
	DeliveredAt    *time.Time      `json:"delivered_at"`
	tenantID       int64
}

// Payload is the JSON body of every webhook.
type Payload struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	TenantID  int64           `json:"tenant_id"`
	Data      json.RawMessage `json:"data"`
}

func mapEndpoint(row dbgen.WebhookEndpoint) Endpoint {
	return Endpoint{
		ID:                      row.ID,
		TenantID:                row.TenantID,
		URL:                     row.Url,
		Description:             row.Description,
		Events:                  row.Events,
		Secret:                  row.Secret,
		PreviousSecret:          row.PreviousSecret,
		PreviousSecretExpiresAt: timePtr(row.PreviousSecretExpiresAt),
		IsActive:                row.IsActive,
		ConsecutiveFailures:     row.ConsecutiveFailures,
		DisabledReason:          row.DisabledReason,
		CreatedBy:               row.CreatedBy,
		CreatedAt:               row.CreatedAt.Time,
		UpdatedAt:               row.UpdatedAt.Time,
	}
}

func mapDelivery(row dbgen.WebhookDelivery) Delivery {
	return Delivery{
		ID:             row.ID,
		EndpointID:     row.EndpointID,
		EventID:        row.EventID,
		EventType:      row.EventType,
		Payload:        row.Payload,
		Status:         row.Status,
		Attempts:       row.Attempts,
		ResponseStatus: row.ResponseStatus,
		ResponseBody:   row.ResponseBody,
		DurationMs:     row.DurationMs,
		Error:          row.Error,
		NextAttemptAt:  timePtr(row.NextAttemptAt),
		CreatedAt:      row.CreatedAt.Time,
		DeliveredAt:    timePtr(row.DeliveredAt),
		tenantID:       row.TenantID,
	}
}

func timePtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}

func timestamptz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}
