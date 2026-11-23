// Package cache defines the driver-independent cache contract and shared
// helpers. Drivers serialize values themselves (JSON for the shipped drivers).
package cache

import (
	"context"
	"errors"
	"time"
)

// ErrMiss reports that an operation required a present key.
var ErrMiss = errors.New("cache: key not found")

// Store is the driver-independent cache contract. Get reports a miss via
// found=false; driver failures are returned as errors rather than swallowed.
// Increment preserves an existing key's TTL. Cache state is never
// authoritative for business data.
type Store interface {
	Get(ctx context.Context, key string, dest any) (found bool, err error)
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	// SetNX stores the value only when the key is absent (or expired), returning
	// whether this caller created it. It is the atomic primitive behind fixed
	// windows and lock-once initialization.
	SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	Clear(ctx context.Context) error
	Increment(ctx context.Context, key string, delta int64) (int64, error)
	TTL(ctx context.Context, key string) (time.Duration, error)
	Ping(ctx context.Context) error
}

// TTL sentinel values mirroring common cache semantics.
const (
	TTLNoExpiry = time.Duration(-1)
	TTLMissing  = time.Duration(-2)
)

// Remember returns the cached value for key, computing and storing it with fn
// on a miss. Cache read/write failures degrade to computing the value: callers
// get fresh data instead of a cache outage, and drivers are responsible for
// surfacing infrastructure errors through their own logging.
func Remember[T any](ctx context.Context, store Store, key string, ttl time.Duration, fn func(ctx context.Context) (T, error)) (T, error) {
	var value T
	if found, err := store.Get(ctx, key, &value); err == nil && found {
		return value, nil
	}

	computed, err := fn(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	_ = store.Set(ctx, key, computed, ttl)
	return computed, nil
}
