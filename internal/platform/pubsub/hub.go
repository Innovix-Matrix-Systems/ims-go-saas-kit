package pubsub

import (
	"errors"
	"sync"
)

// ErrTooManyStreams is returned by Subscribe when a user already holds the
// configured number of streams on this process.
var ErrTooManyStreams = errors.New("pubsub: too many streams for this user")

// ErrClosed is returned by Subscribe after Close.
var ErrClosed = errors.New("pubsub: hub closed")

// subscriptionBuffer is how many kinds a slow stream may have pending. When
// it is full the newest event is dropped: the client is already behind and
// refetches everything the pending events name.
const subscriptionBuffer = 8

// Hub hands events to the streams open on this server process.
type Hub struct {
	limit int

	mu      sync.Mutex
	subs    map[*Subscription]struct{}
	perUser map[int64]int
	closed  bool
}

// NewHub creates a hub allowing limit streams per user (at least one).
func NewHub(limit int) *Hub {
	if limit < 1 {
		limit = 1
	}
	return &Hub{limit: limit, subs: map[*Subscription]struct{}{}, perUser: map[int64]int{}}
}

// Subscription is one open stream. The kind of each matching event arrives
// on Events, which is closed when the subscription ends.
type Subscription struct {
	UserID   int64
	TenantID int64
	events   chan string
}

// Events delivers the kind of each matching event.
func (s *Subscription) Events() <-chan string { return s.events }

// Subscribe opens a subscription for a user (tenantID 0: a platform account).
func (h *Hub) Subscribe(userID, tenantID int64) (*Subscription, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, ErrClosed
	}
	if h.perUser[userID] >= h.limit {
		return nil, ErrTooManyStreams
	}
	sub := &Subscription{UserID: userID, TenantID: tenantID, events: make(chan string, subscriptionBuffer)}
	h.subs[sub] = struct{}{}
	h.perUser[userID]++
	return sub, nil
}

// Unsubscribe removes a subscription; safe to call more than once and after
// Close.
func (h *Hub) Unsubscribe(sub *Subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[sub]; !ok {
		return
	}
	delete(h.subs, sub)
	if h.perUser[sub.UserID]--; h.perUser[sub.UserID] <= 0 {
		delete(h.perUser, sub.UserID)
	}
	close(sub.events)
}

// Dispatch delivers an event to every matching subscription without
// blocking.
func (h *Hub) Dispatch(event Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		if !event.Matches(sub.UserID, sub.TenantID) {
			continue
		}
		select {
		case sub.events <- event.Kind:
		default:
		}
	}
}

// Len reports the open subscriptions.
func (h *Hub) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// Close ends every subscription (their Events channels close, so streams
// tell clients to reconnect and return) and refuses new ones. Called on
// shutdown so open streams do not hold up the server's drain.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for sub := range h.subs {
		close(sub.events)
		delete(h.subs, sub)
	}
	clear(h.perUser)
}
