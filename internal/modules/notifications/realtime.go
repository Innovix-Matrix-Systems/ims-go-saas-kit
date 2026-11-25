package notifications

import (
	"context"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/pubsub"
)

// RealtimeKind is the live-update event the app answers by refetching the
// notification feed and unread count (docs/guides/realtime.md).
const RealtimeKind = "notifications"

// maxListedUsers is the most users one event names; a larger audience is
// addressed as its whole tenant (a NOTIFY payload is under 8 KB, and a
// member who was not addressed just refetches and finds nothing new).
const maxListedUsers = 500

// WithPublisher tells open streams when a feed changes. Without one (or with
// pubsub.Discard) clients find changes by polling.
func (s *Service) WithPublisher(publisher pubsub.Publisher) *Service {
	s.publisher = publisher
	return s
}

// publishToUsers nudges the listed users of a tenant (0: platform accounts).
func (s *Service) publishToUsers(ctx context.Context, tenantID int64, userIDs []int64) {
	if len(userIDs) == 0 {
		return
	}
	if len(userIDs) > maxListedUsers {
		s.publish(ctx, pubsub.ToTenant(RealtimeKind, tenantID))
		return
	}
	s.publish(ctx, pubsub.ToUsers(RealtimeKind, userIDs...))
}

// publishToTenant nudges everyone a tenant's notifications can reach (0:
// platform accounts, for notifications without a tenant).
func (s *Service) publishToTenant(ctx context.Context, tenantID int64) {
	s.publish(ctx, pubsub.ToTenant(RealtimeKind, tenantID))
}

// publish never fails the change it reports: polling still finds it.
func (s *Service) publish(ctx context.Context, event pubsub.Event) {
	if s.publisher == nil {
		return
	}
	if err := s.publisher.Publish(ctx, event); err != nil {
		s.logWarn(ctx, "notification live update not published", "error", err)
	}
}
