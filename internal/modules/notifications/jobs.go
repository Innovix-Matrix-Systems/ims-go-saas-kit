package notifications

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/queue"
)

// Job kinds registered by the notifications module.
const (
	KindFanOut          = "notification.fanout"
	KindMarkAllRead     = "notification.mark-all-read"
	KindDecrementUnread = "notification.decrement-unread"
)

type fanOutPayload struct {
	NotificationID int64 `json:"notification_id"`
}

// markAllReadPayload names the branch notices the feed showed: one
// branch, every branch, or (both empty) the member's own branch.
type markAllReadPayload struct {
	UserID      int64 `json:"user_id"`
	BranchID    int64 `json:"branch_id,omitempty"`
	AllBranches bool  `json:"all_branches,omitempty"`
}

type decrementUnreadPayload struct {
	TenantID int64   `json:"tenant_id"`
	UserIDs  []int64 `json:"user_ids"`
}

// RegisterJobs adds the notification job definitions to the registry.
func (s *Service) RegisterJobs(registry *queue.Registry) error {
	definitions := []queue.Definition{
		{
			Kind:        KindFanOut,
			Description: "Fans a notification out to its target users (chunked, idempotent).",
			Queue:       queue.QueueDefault,
			Timeout:     5 * time.Minute,
			SystemJob:   true,
			Validate:    validatePayload[fanOutPayload],
			Handle: func(ctx context.Context, payload []byte) error {
				var input fanOutPayload
				if err := json.Unmarshal(payload, &input); err != nil {
					return err
				}
				return s.ProcessFanOut(ctx, input.NotificationID)
			},
		},
		{
			Kind:        KindMarkAllRead,
			Description: "Marks every addressable notification read for one user.",
			Queue:       queue.QueueDefault,
			Timeout:     5 * time.Minute,
			SystemJob:   true,
			Validate:    validatePayload[markAllReadPayload],
			Handle: func(ctx context.Context, payload []byte) error {
				var input markAllReadPayload
				if err := json.Unmarshal(payload, &input); err != nil {
					return err
				}
				return s.ProcessMarkAllRead(ctx, input.UserID, input.BranchID, input.AllBranches)
			},
		},
		{
			Kind:        KindDecrementUnread,
			Description: "Repairs unread counters after a notification is deleted.",
			Queue:       queue.QueueDefault,
			Timeout:     time.Minute,
			SystemJob:   true,
			Validate:    validatePayload[decrementUnreadPayload],
			Handle: func(ctx context.Context, payload []byte) error {
				var input decrementUnreadPayload
				if err := json.Unmarshal(payload, &input); err != nil {
					return err
				}
				return s.ProcessCounterDecrement(ctx, input.TenantID, input.UserIDs)
			},
		},
	}
	for _, definition := range definitions {
		if err := registry.Register(definition); err != nil {
			return err
		}
	}
	return nil
}

func validatePayload[T any](payload []byte) error {
	var value T
	if err := json.Unmarshal(payload, &value); err != nil {
		return apperr.Wrap(apperr.InvalidInput, "Invalid job payload.", err)
	}
	return nil
}
