// Package pubsub carries "something changed" signals from any process (the
// API, a queue worker, the scheduler) to the server processes holding live
// streams, over Postgres LISTEN/NOTIFY (docs/guides/realtime.md).
//
// Events are nudges, not data: they say which kind of thing changed and for
// whom, and the client refetches through the normal API, where permissions,
// tenancy and read state apply. A lost or duplicated event therefore costs at
// most one extra request; one sent to too many people costs a refetch that
// finds nothing new.
package pubsub

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
)

// Channel is the Postgres notification channel every server process LISTENs on.
const Channel = "ims_realtime"

// KindResync tells every subscriber to refetch everything: sent by the
// listener after it reconnected, since Postgres keeps no notifications for a
// listener that was away.
const KindResync = "resync"

// maxPayload stays under the 8000-byte NOTIFY payload limit.
const maxPayload = 7900

// ErrTooLarge is returned for an event whose encoding exceeds the NOTIFY
// payload limit; address a whole tenant instead of listing its users.
var ErrTooLarge = errors.New("pubsub: event payload too large")

// Event is one change signal. It reaches a subscriber when any audience field
// matches: one of UserIDs, one of TenantIDs, every tenant member (AllTenants)
// or every tenantless platform account (Platform).
type Event struct {
	Kind       string  `json:"k"`
	UserIDs    []int64 `json:"u,omitempty"`
	TenantIDs  []int64 `json:"t,omitempty"`
	AllTenants bool    `json:"a,omitempty"`
	Platform   bool    `json:"p,omitempty"`
}

// ToUsers addresses an event to the listed users.
func ToUsers(kind string, userIDs ...int64) Event {
	return Event{Kind: kind, UserIDs: userIDs}
}

// ToTenant addresses an event to every member of a tenant; tenantID 0 means
// the tenantless platform accounts.
func ToTenant(kind string, tenantID int64) Event {
	if tenantID == 0 {
		return Event{Kind: kind, Platform: true}
	}
	return Event{Kind: kind, TenantIDs: []int64{tenantID}}
}

// Matches reports whether a subscriber (tenantID 0: a platform account)
// should hear the event.
func (e Event) Matches(userID, tenantID int64) bool {
	switch {
	case e.Kind == KindResync:
		return true
	case slices.Contains(e.UserIDs, userID):
		return true
	case tenantID == 0:
		return e.Platform
	default:
		return e.AllTenants || slices.Contains(e.TenantIDs, tenantID)
	}
}

// Publisher sends events to every listening server process.
type Publisher interface {
	Publish(ctx context.Context, event Event) error
}

// Discard is a Publisher that sends nothing (realtime off, tests).
var Discard Publisher = discard{}

type discard struct{}

func (discard) Publish(context.Context, Event) error { return nil }

// Postgres publishes with pg_notify. Built on a transaction's queries, the
// event goes out only if that transaction commits.
type Postgres struct {
	queries *dbgen.Queries
}

// NewPostgres creates the NOTIFY publisher.
func NewPostgres(queries *dbgen.Queries) *Postgres {
	return &Postgres{queries: queries}
}

// Publish implements Publisher.
func (p *Postgres) Publish(ctx context.Context, event Event) error {
	payload, err := encode(event)
	if err != nil {
		return err
	}
	return p.queries.PublishRealtimeEvent(ctx, payload)
}

func encode(event Event) (string, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return "", err
	}
	if len(payload) > maxPayload {
		return "", ErrTooLarge
	}
	return string(payload), nil
}

func decode(payload string) (Event, error) {
	var event Event
	err := json.Unmarshal([]byte(payload), &event)
	return event, err
}
