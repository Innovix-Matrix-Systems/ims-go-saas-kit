package pubsub

import (
	"errors"
	"testing"
)

func TestEventMatches(t *testing.T) {
	tests := []struct {
		name     string
		event    Event
		userID   int64
		tenantID int64
		want     bool
	}{
		{"listed user", ToUsers("k", 7, 9), 9, 3, true},
		{"unlisted user", ToUsers("k", 7), 9, 3, false},
		{"tenant member", ToTenant("k", 3), 9, 3, true},
		{"other tenant", ToTenant("k", 3), 9, 4, false},
		{"platform event reaches platform accounts", ToTenant("k", 0), 1, 0, true},
		{"platform event skips tenant members", ToTenant("k", 0), 9, 3, false},
		{"tenant event skips platform accounts", ToTenant("k", 3), 1, 0, false},
		{"every tenant", Event{Kind: "k", AllTenants: true}, 9, 4, true},
		{"every tenant skips platform accounts", Event{Kind: "k", AllTenants: true}, 1, 0, false},
		{"listed platform user", ToUsers("k", 1), 1, 0, true},
		{"resync reaches everyone", Event{Kind: KindResync}, 1, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.event.Matches(tt.userID, tt.tenantID); got != tt.want {
				t.Fatalf("Matches(%d, %d) = %v, want %v", tt.userID, tt.tenantID, got, tt.want)
			}
		})
	}
}

func TestEncodeRoundTripAndLimit(t *testing.T) {
	event := Event{Kind: "notifications", UserIDs: []int64{1, 2}, TenantIDs: []int64{3}}
	payload, err := encode(event)
	if err != nil {
		t.Fatalf("encode() error = %v", err)
	}
	got, err := decode(payload)
	if err != nil {
		t.Fatalf("decode() error = %v", err)
	}
	if got.Kind != event.Kind || len(got.UserIDs) != 2 || got.TenantIDs[0] != 3 {
		t.Fatalf("decode(encode()) = %+v, want %+v", got, event)
	}

	many := make([]int64, 2000)
	for i := range many {
		many[i] = int64(100000 + i)
	}
	if _, err := encode(ToUsers("notifications", many...)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("encode(2000 users) error = %v, want ErrTooLarge", err)
	}
}

func TestHubDispatchesToMatchingSubscriptions(t *testing.T) {
	hub := NewHub(5)
	member, _ := hub.Subscribe(9, 3)
	other, _ := hub.Subscribe(10, 4)

	hub.Dispatch(ToTenant("announcements", 3))

	if kind := <-member.Events(); kind != "announcements" {
		t.Fatalf("member got %q, want announcements", kind)
	}
	select {
	case kind := <-other.Events():
		t.Fatalf("other tenant got %q", kind)
	default:
	}
}

func TestHubLimitsStreamsPerUser(t *testing.T) {
	hub := NewHub(2)
	first, _ := hub.Subscribe(9, 3)
	if _, err := hub.Subscribe(9, 3); err != nil {
		t.Fatalf("second stream: %v", err)
	}
	if _, err := hub.Subscribe(9, 3); !errors.Is(err, ErrTooManyStreams) {
		t.Fatalf("third stream error = %v, want ErrTooManyStreams", err)
	}
	hub.Unsubscribe(first)
	hub.Unsubscribe(first) // twice is harmless
	if _, err := hub.Subscribe(9, 3); err != nil {
		t.Fatalf("stream after unsubscribe: %v", err)
	}
}

func TestHubDropsWhenSubscriptionIsFull(t *testing.T) {
	hub := NewHub(1)
	sub, _ := hub.Subscribe(9, 3)
	for range subscriptionBuffer + 5 {
		hub.Dispatch(ToUsers("notifications", 9)) // must never block
	}
	if got := len(sub.Events()); got != subscriptionBuffer {
		t.Fatalf("pending = %d, want %d", got, subscriptionBuffer)
	}
}

func TestHubCloseEndsSubscriptions(t *testing.T) {
	hub := NewHub(1)
	sub, _ := hub.Subscribe(9, 3)
	hub.Close()
	if _, open := <-sub.Events(); open {
		t.Fatal("Events() still open after Close")
	}
	hub.Unsubscribe(sub) // after Close is harmless
	if _, err := hub.Subscribe(9, 3); !errors.Is(err, ErrClosed) {
		t.Fatalf("Subscribe after Close error = %v, want ErrClosed", err)
	}
	if hub.Len() != 0 {
		t.Fatalf("Len() = %d after Close", hub.Len())
	}
}
