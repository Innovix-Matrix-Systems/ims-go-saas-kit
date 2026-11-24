package announcements

import (
	"context"
	"errors"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/pubsub"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/tracing"
)

// RealtimeKind is the live-update event the app answers by refetching the
// live announcements (docs/guides/realtime.md).
const RealtimeKind = "announcements"

// WithPublisher tells open streams when what an organization sees changes.
// Without one (or with pubsub.Discard) clients find changes by polling.
func (s *Service) WithPublisher(publisher pubsub.Publisher) *Service {
	s.publisher = publisher
	return s
}

// PublishTransitions tells the audiences of announcements that started or
// ended in (since, until] to refetch. The scheduler calls it every minute;
// it returns how many announcements changed.
func (s *Service) PublishTransitions(ctx context.Context, since, until time.Time) (_ int, err error) {
	ctx, end := tracing.Begin(ctx, "announcements.publish_transitions")
	defer end(&err)
	if s.publisher == nil {
		return 0, nil
	}
	changed, err := s.repo.StartedOrEnded(ctx, since, until)
	if err != nil {
		return 0, err
	}
	for _, announcement := range changed {
		event, err := s.audienceEvent(ctx, announcement)
		if err != nil {
			return 0, err
		}
		s.publish(ctx, event)
	}
	tracing.Annotate(ctx, attribute.Int("announcements", len(changed)))
	return len(changed), nil
}

// liveAudience is who sees the announcement right now: its organizations
// while it is live, nil otherwise (or without a publisher). Read it before a
// change for the old audience and after for the new one.
func (s *Service) liveAudience(ctx context.Context, announcement *Announcement) *pubsub.Event {
	if s.publisher == nil || announcement == nil || announcement.State(s.now()) != "live" {
		return nil
	}
	event, err := s.audienceEvent(ctx, *announcement)
	if err != nil {
		s.logWarn(ctx, "announcement live update not published", "announcement_id", announcement.ID, "error", err)
		return nil
	}
	return &event
}

// publishAudiences sends one event to the union of the audiences given (a
// staff change reaches whoever saw it before and whoever sees it now).
func (s *Service) publishAudiences(ctx context.Context, audiences ...*pubsub.Event) {
	event := pubsub.Event{Kind: RealtimeKind}
	reached := false
	for _, audience := range audiences {
		if audience == nil {
			continue
		}
		reached = true
		event.AllTenants = event.AllTenants || audience.AllTenants
		for _, id := range audience.TenantIDs {
			if !slices.Contains(event.TenantIDs, id) {
				event.TenantIDs = append(event.TenantIDs, id)
			}
		}
	}
	if !reached {
		return
	}
	if event.AllTenants {
		event.TenantIDs = nil
	}
	s.publish(ctx, event)
}

// audienceEvent addresses an announcement's organizations.
func (s *Service) audienceEvent(ctx context.Context, announcement Announcement) (pubsub.Event, error) {
	if announcement.Audience == AudienceAll {
		return pubsub.Event{Kind: RealtimeKind, AllTenants: true}, nil
	}
	tenantIDs, err := s.repo.AudienceTenantIDs(ctx, announcement)
	if err != nil {
		return pubsub.Event{}, err
	}
	return pubsub.Event{Kind: RealtimeKind, TenantIDs: tenantIDs}, nil
}

// publish never fails the change it reports: polling still finds it. An
// audience too long to list goes to every organization instead.
func (s *Service) publish(ctx context.Context, event pubsub.Event) {
	err := s.publisher.Publish(ctx, event)
	if errors.Is(err, pubsub.ErrTooLarge) {
		err = s.publisher.Publish(ctx, pubsub.Event{Kind: event.Kind, AllTenants: true})
	}
	if err != nil {
		s.logWarn(ctx, "announcement live update not published", "error", err)
	}
}

func (s *Service) logWarn(ctx context.Context, message string, args ...any) {
	if s.logger != nil {
		s.logger.WarnContext(ctx, message, args...)
	}
}
