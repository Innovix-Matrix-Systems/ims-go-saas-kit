// Package redis implements a Redis cache driver over go-redis.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/extra/redisotel/v9"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache"
)

// Options configures the Redis cache driver.
type Options struct {
	Addr     string
	Password string
	DB       int
	Prefix   string
}

// Store is a Redis cache store implementing cache.Store.
type Store struct {
	client *goredis.Client
	prefix string
}

var _ cache.Store = (*Store)(nil)

// New creates the Redis cache store. Every command is a span (redisotel),
// a no-op while tracing is off.
func New(opts Options) (*Store, error) {
	client := goredis.NewClient(&goredis.Options{
		Addr:     opts.Addr,
		Password: opts.Password,
		DB:       opts.DB,
	})
	if err := redisotel.InstrumentTracing(client); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("cache: redis tracing: %w", err)
	}
	return &Store{client: client, prefix: opts.Prefix}, nil
}

// Close closes the underlying client.
func (s *Store) Close() error {
	return s.client.Close()
}

// Get implements cache.Store.
func (s *Store) Get(ctx context.Context, key string, dest any) (bool, error) {
	data, err := s.client.Get(ctx, s.prefix+key).Bytes()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return false, nil
		}
		return false, fmt.Errorf("cache: redis get %q: %w", key, err)
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return false, fmt.Errorf("cache: decode %q: %w", key, err)
	}
	return true, nil
}

// Set implements cache.Store. A ttl <= 0 stores the value without expiry.
func (s *Store) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cache: encode %q: %w", key, err)
	}
	if ttl < 0 {
		ttl = 0
	}
	if err := s.client.Set(ctx, s.prefix+key, data, ttl).Err(); err != nil {
		return fmt.Errorf("cache: redis set %q: %w", key, err)
	}
	return nil
}

// SetNX implements cache.Store.
func (s *Store) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return false, fmt.Errorf("cache: encode %q: %w", key, err)
	}
	if ttl < 0 {
		ttl = 0
	}
	created, err := s.client.SetNX(ctx, s.prefix+key, data, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("cache: redis setnx %q: %w", key, err)
	}
	return created, nil
}

// Delete implements cache.Store.
func (s *Store) Delete(ctx context.Context, key string) error {
	if err := s.client.Del(ctx, s.prefix+key).Err(); err != nil {
		return fmt.Errorf("cache: redis delete %q: %w", key, err)
	}
	return nil
}

// Exists implements cache.Store.
func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	count, err := s.client.Exists(ctx, s.prefix+key).Result()
	if err != nil {
		return false, fmt.Errorf("cache: redis exists %q: %w", key, err)
	}
	return count > 0, nil
}

// Clear implements cache.Store and deletes every key under the prefix using
// SCAN (never FLUSHDB, which could belong to another application).
func (s *Store) Clear(ctx context.Context) error {
	var cursor uint64
	for {
		keys, next, err := s.client.Scan(ctx, cursor, s.prefix+"*", 500).Result()
		if err != nil {
			return fmt.Errorf("cache: redis scan: %w", err)
		}
		if len(keys) > 0 {
			if err := s.client.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("cache: redis clear: %w", err)
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

// Increment implements cache.Store. Existing TTLs are preserved (INCRBY).
func (s *Store) Increment(ctx context.Context, key string, delta int64) (int64, error) {
	value, err := s.client.IncrBy(ctx, s.prefix+key, delta).Result()
	if err != nil {
		return 0, fmt.Errorf("cache: redis increment %q: %w", key, err)
	}
	return value, nil
}

// TTL implements cache.Store.
func (s *Store) TTL(ctx context.Context, key string) (time.Duration, error) {
	ttl, err := s.client.PTTL(ctx, s.prefix+key).Result()
	if err != nil {
		return 0, fmt.Errorf("cache: redis ttl %q: %w", key, err)
	}
	switch ttl {
	case -2 * time.Nanosecond:
		return cache.TTLMissing, nil
	case -1 * time.Nanosecond:
		return cache.TTLNoExpiry, nil
	default:
		return ttl, nil
	}
}

// Ping implements cache.Store.
func (s *Store) Ping(ctx context.Context) error {
	if err := s.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("cache: redis ping: %w", err)
	}
	return nil
}
