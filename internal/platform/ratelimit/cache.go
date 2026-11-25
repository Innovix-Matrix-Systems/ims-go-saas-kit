package ratelimit

import (
	"context"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/cache"
)

// cacheLimiter is a fixed-window limiter backed by the cache store. It shares
// its counter with every process using the same store (unlike memoryLimiter).
type cacheLimiter struct {
	store  cache.Store
	limit  int
	window time.Duration
}

// NewCacheLimiter creates a cache-backed limiter. Counter increments that fail
// (cache outage) fail open and rely on the cache driver's own observability.
func NewCacheLimiter(store cache.Store, limit int, window time.Duration) Limiter {
	return &cacheLimiter{store: store, limit: limit, window: window}
}

// Allow implements Limiter. The window is initialized atomically (SetNX) so a
// concurrent first request cannot reset the counter or drop the expiry.
func (l *cacheLimiter) Allow(ctx context.Context, key string) (bool, time.Duration) {
	count, err := l.next(ctx, key)
	if err != nil {
		return true, 0
	}
	if int(count) > l.limit {
		retryAfter, err := l.store.TTL(ctx, key)
		if err != nil || retryAfter < 0 {
			retryAfter = l.window
		}
		return false, retryAfter
	}
	return true, 0
}

func (l *cacheLimiter) next(ctx context.Context, key string) (int64, error) {
	created, err := l.store.SetNX(ctx, key, int64(1), l.window)
	if err != nil {
		return 0, err
	}
	if created {
		return 1, nil
	}
	return l.store.Increment(ctx, key, 1)
}

// Close implements Limiter (no resources owned).
func (l *cacheLimiter) Close() {}
