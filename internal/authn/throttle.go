package authn

import (
	"context"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache"
)

// Throttle implements attempt counting with a decay window.
type Throttle struct {
	store cache.Store
	max   int
	decay time.Duration
}

// NewThrottle creates an attempt throttle over the cache store.
func NewThrottle(store cache.Store, max int, decay time.Duration) *Throttle {
	return &Throttle{store: store, max: max, decay: decay}
}

// TooMany reports whether the key exceeded the allowed attempts, plus the
// remaining decay time.
func (t *Throttle) TooMany(ctx context.Context, key string) (bool, time.Duration, error) {
	var count int64
	found, err := t.store.Get(ctx, key, &count)
	if err != nil {
		return false, 0, err
	}
	if !found || count < int64(t.max) {
		return false, 0, nil
	}
	ttl, err := t.store.TTL(ctx, key)
	if err != nil {
		return false, 0, err
	}
	if ttl < 0 {
		ttl = t.decay
	}
	return true, ttl, nil
}

// Hit records one failed attempt. The decay window starts with the first hit.
func (t *Throttle) Hit(ctx context.Context, key string) error {
	count, err := t.store.Increment(ctx, key, 1)
	if err != nil {
		return err
	}
	if count == 1 {
		return t.store.Set(ctx, key, count, t.decay)
	}
	return nil
}

// Clear removes the counter after a successful login.
func (t *Throttle) Clear(ctx context.Context, key string) error {
	return t.store.Delete(ctx, key)
}
