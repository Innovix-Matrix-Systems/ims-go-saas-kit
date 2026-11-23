package telemetry

import (
	"context"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
)

// CacheStore decorates cache.Store with telemetry capture.
type CacheStore struct {
	inner     cache.Store
	collector *Collector
}

// WrapCache returns a capturing cache.Store when cache capture is enabled;
// otherwise the inner store is returned as-is.
func WrapCache(inner cache.Store, collector *Collector) cache.Store {
	if inner == nil || collector == nil || !collector.capture.Enabled || !collector.capture.Capture.Cache {
		return inner
	}
	return &CacheStore{inner: inner, collector: collector}
}

func (s *CacheStore) event(ctx context.Context, operation, key string, hit *bool, started time.Time) {
	s.collector.CacheEvent(CacheEventEntry{
		RequestID:  httpx.RequestIDFromContext(ctx),
		Operation:  operation,
		Key:        key,
		Hit:        hit,
		DurationMS: float64(time.Since(started).Microseconds()) / 1000.0,
		CreatedAt:  time.Now(),
	})
}

// Get implements cache.Store.
func (s *CacheStore) Get(ctx context.Context, key string, dest any) (bool, error) {
	started := time.Now()
	found, err := s.inner.Get(ctx, key, dest)
	s.event(ctx, "get", key, &found, started)
	return found, err
}

// Set implements cache.Store.
func (s *CacheStore) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	started := time.Now()
	err := s.inner.Set(ctx, key, value, ttl)
	s.event(ctx, "set", key, nil, started)
	return err
}

// SetNX implements cache.Store.
func (s *CacheStore) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	started := time.Now()
	set, err := s.inner.SetNX(ctx, key, value, ttl)
	s.event(ctx, "setnx", key, nil, started)
	return set, err
}

// Delete implements cache.Store.
func (s *CacheStore) Delete(ctx context.Context, key string) error {
	started := time.Now()
	err := s.inner.Delete(ctx, key)
	s.event(ctx, "delete", key, nil, started)
	return err
}

// Exists implements cache.Store.
func (s *CacheStore) Exists(ctx context.Context, key string) (bool, error) {
	started := time.Now()
	exists, err := s.inner.Exists(ctx, key)
	s.event(ctx, "exists", key, &exists, started)
	return exists, err
}

// Clear implements cache.Store.
func (s *CacheStore) Clear(ctx context.Context) error {
	started := time.Now()
	err := s.inner.Clear(ctx)
	s.event(ctx, "clear", "", nil, started)
	return err
}

// Increment implements cache.Store.
func (s *CacheStore) Increment(ctx context.Context, key string, delta int64) (int64, error) {
	started := time.Now()
	value, err := s.inner.Increment(ctx, key, delta)
	s.event(ctx, "increment", key, nil, started)
	return value, err
}

// TTL implements cache.Store.
func (s *CacheStore) TTL(ctx context.Context, key string) (time.Duration, error) {
	started := time.Now()
	ttl, err := s.inner.TTL(ctx, key)
	s.event(ctx, "ttl", key, nil, started)
	return ttl, err
}

// Ping implements cache.Store.
func (s *CacheStore) Ping(ctx context.Context) error {
	return s.inner.Ping(ctx)
}
