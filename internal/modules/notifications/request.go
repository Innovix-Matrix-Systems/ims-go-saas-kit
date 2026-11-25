package notifications

import (
	"encoding/json"
	"time"
)

// TargetRequest is one addressability rule in create/update payloads.
type TargetRequest struct {
	Type string `json:"type" validate:"required,oneof=USER ROLE BRANCH TENANT GLOBAL"`
	ID   *int64 `json:"id" validate:"omitempty,gt=0"`
}

// InsertUpdateRequest is the admin notification create/update payload (PATCH
// is a full replacement).
type InsertUpdateRequest struct {
	EventType string          `json:"event_type" validate:"required,max=255"`
	Priority  string          `json:"priority" validate:"required,oneof=INFO WARNING CRITICAL"`
	Title     string          `json:"title" validate:"required,max=255"`
	Body      *string         `json:"body"`
	Data      map[string]any  `json:"data"`
	GroupKey  *string         `json:"group_key" validate:"omitempty,max=255"`
	ExpiresAt *time.Time      `json:"expires_at"`
	TenantID  *int64          `json:"tenant_id" validate:"omitempty,gt=0"`
	Targets   []TargetRequest `json:"targets" validate:"required,min=1,dive"`
	// Channels (create only): the feed is always on; add mail, sms or push
	// to also deliver it (the event type decides which are allowed).
	Channels []string `json:"channels" validate:"omitempty,max=4,dive,oneof=database mail sms push"`
}

// PreferencesRequest changes notification preferences.
type PreferencesRequest struct {
	Preferences []PreferenceRequest `json:"preferences" validate:"required,min=1,max=100,dive"`
}

// PreferenceRequest is one event type × channel choice.
type PreferenceRequest struct {
	EventType string `json:"event_type" validate:"required,max=255"`
	Channel   string `json:"channel" validate:"required,oneof=mail sms push"`
	Enabled   bool   `json:"enabled"`
}

// UnsubscribeRequest carries the token of a one-click unsubscribe link.
type UnsubscribeRequest struct {
	Token string `json:"token" validate:"required,max=2000"`
}

// TargetResponse is the public target representation.
type TargetResponse struct {
	ID         int64  `json:"id"`
	TargetType string `json:"target_type"`
	TargetID   *int64 `json:"target_id"`
}

// Response is the admin notification representation.
type Response struct {
	ID        int64            `json:"id"`
	TenantID  *int64           `json:"tenant_id"`
	EventType string           `json:"event_type"`
	Priority  string           `json:"priority"`
	Title     string           `json:"title"`
	Body      *string          `json:"body"`
	Data      any              `json:"data"`
	GroupKey  *string          `json:"group_key"`
	CreatedBy *int64           `json:"created_by"`
	UpdatedBy *int64           `json:"updated_by"`
	ExpiresAt *time.Time       `json:"expires_at"`
	Channels  []string         `json:"channels"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
	Targets   []TargetResponse `json:"targets"`
}

// FeedItemResponse is one feed item.
type FeedItemResponse struct {
	ID        int64     `json:"id"`
	EventType string    `json:"event_type"`
	Priority  string    `json:"priority"`
	Title     string    `json:"title"`
	Body      *string   `json:"body"`
	Data      any       `json:"data"`
	GroupKey  *string   `json:"group_key"`
	CreatedAt time.Time `json:"created_at"`
	IsRead    bool      `json:"is_read"`
}

// FeedMeta is the cursor feed meta object.
type FeedMeta struct {
	Path        string  `json:"path"`
	PerPage     int     `json:"per_page"`
	NextCursor  *string `json:"next_cursor"`
	PrevCursor  *string `json:"prev_cursor"`
	UnreadCount int     `json:"unread_count"`
}

// FeedLinks is the cursor feed links object (cursor pagination has no
// first/last links).
type FeedLinks struct {
	First *string `json:"first"`
	Last  *string `json:"last"`
	Prev  *string `json:"prev"`
	Next  *string `json:"next"`
}

// NewResponse maps a notification and its targets to the admin representation.
func NewResponse(notification Notification, targets []Target) Response {
	targetResponses := make([]TargetResponse, 0, len(targets))
	for _, target := range targets {
		targetResponses = append(targetResponses, TargetResponse{
			ID:         target.ID,
			TargetType: target.TargetType,
			TargetID:   target.TargetID,
		})
	}
	return Response{
		ID:        notification.ID,
		TenantID:  notification.TenantID,
		EventType: notification.EventType,
		Priority:  notification.Priority,
		Title:     notification.Title,
		Body:      notification.Body,
		Data:      dataAny(notification.Data),
		GroupKey:  notification.GroupKey,
		CreatedBy: notification.CreatedBy,
		UpdatedBy: notification.UpdatedBy,
		ExpiresAt: notification.ExpiresAt,
		Channels:  notification.Channels,
		CreatedAt: notification.CreatedAt,
		UpdatedAt: notification.UpdatedAt,
		Targets:   targetResponses,
	}
}

// NewFeedResponse maps a feed row to its public representation, the title and
// body in the reader's language when the notification carries a message key.
func NewFeedResponse(item FeedItem, locale string) FeedItemResponse {
	title, body := localize(locale, item.Data, item.Title, item.Body)
	return FeedItemResponse{
		ID:        item.ID,
		EventType: item.EventType,
		Priority:  item.Priority,
		Title:     title,
		Body:      body,
		Data:      dataAny(item.Data),
		GroupKey:  item.GroupKey,
		CreatedAt: item.CreatedAt,
		IsRead:    item.IsRead,
	}
}

func dataAny(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}
