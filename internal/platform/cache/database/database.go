// Package database implements a cache driver backed by the shared database
// (the "cache" table).
package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache"
)

// Store is a database-backed cache store implementing cache.Store.
type Store struct {
	queries *dbgen.Queries
	prefix  string
}

var _ cache.Store = (*Store)(nil)

// New creates the database cache store.
func New(queries *dbgen.Queries, prefix string) *Store {
	return &Store{queries: queries, prefix: prefix}
}

// Get implements cache.Store.
func (s *Store) Get(ctx context.Context, key string, dest any) (bool, error) {
	row, err := s.queries.CacheGet(ctx, s.prefix+key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("cache: database get %q: %w", key, err)
	}
	if row.Expiration.Valid && !time.Now().Before(row.Expiration.Time) {
		_ = s.Delete(ctx, key)
		return false, nil
	}
	if err := json.Unmarshal([]byte(row.Value), dest); err != nil {
		return false, fmt.Errorf("cache: decode %q: %w", key, err)
	}
	return true, nil
}

// Set implements cache.Store. A ttl <= 0 stores the value without expiry.
func (s *Store) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	text, err := encodeValue(value)
	if err != nil {
		return fmt.Errorf("cache: encode %q: %w", key, err)
	}
	var expiration pgtype.Timestamptz
	if ttl > 0 {
		expiration = pgtype.Timestamptz{Time: time.Now().Add(ttl), Valid: true}
	}
	if err := s.queries.CachePut(ctx, dbgen.CachePutParams{
		Key:        s.prefix + key,
		Value:      text,
		Expiration: expiration,
	}); err != nil {
		return fmt.Errorf("cache: database set %q: %w", key, err)
	}
	return nil
}

// SetNX implements cache.Store. It stores the value only when the key is absent
// or expired, reporting whether this caller created it.
func (s *Store) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	text, err := encodeValue(value)
	if err != nil {
		return false, fmt.Errorf("cache: encode %q: %w", key, err)
	}
	var expiration pgtype.Timestamptz
	if ttl > 0 {
		expiration = pgtype.Timestamptz{Time: time.Now().Add(ttl), Valid: true}
	}
	affected, err := s.queries.CachePutIfAbsent(ctx, dbgen.CachePutIfAbsentParams{
		Key:        s.prefix + key,
		Value:      text,
		Expiration: expiration,
	})
	if err != nil {
		return false, fmt.Errorf("cache: database setnx %q: %w", key, err)
	}
	return affected == 1, nil
}

// Delete implements cache.Store.
func (s *Store) Delete(ctx context.Context, key string) error {
	if _, err := s.queries.CacheDelete(ctx, s.prefix+key); err != nil {
		return fmt.Errorf("cache: database delete %q: %w", key, err)
	}
	return nil
}

// Exists implements cache.Store.
func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	row, err := s.queries.CacheGet(ctx, s.prefix+key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("cache: database exists %q: %w", key, err)
	}
	if row.Expiration.Valid && !time.Now().Before(row.Expiration.Time) {
		_ = s.Delete(ctx, key)
		return false, nil
	}
	return true, nil
}

// Clear implements cache.Store and removes keys under the store prefix only.
func (s *Store) Clear(ctx context.Context) error {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s.prefix)
	if _, err := s.queries.CacheClearPrefix(ctx, escaped+"%"); err != nil {
		return fmt.Errorf("cache: database clear: %w", err)
	}
	return nil
}

// Increment implements cache.Store. Existing TTLs are preserved.
func (s *Store) Increment(ctx context.Context, key string, delta int64) (int64, error) {
	value, err := s.queries.CacheIncrement(ctx, dbgen.CacheIncrementParams{
		Key:   s.prefix + key,
		Delta: strconv.FormatInt(delta, 10),
	})
	if err != nil {
		return 0, fmt.Errorf("cache: database increment %q: %w", key, err)
	}
	return value, nil
}

// TTL implements cache.Store.
func (s *Store) TTL(ctx context.Context, key string) (time.Duration, error) {
	row, err := s.queries.CacheGet(ctx, s.prefix+key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cache.TTLMissing, nil
		}
		return 0, fmt.Errorf("cache: database ttl %q: %w", key, err)
	}
	if !row.Expiration.Valid {
		return cache.TTLNoExpiry, nil
	}
	remaining := time.Until(row.Expiration.Time)
	if remaining <= 0 {
		_ = s.Delete(ctx, key)
		return cache.TTLMissing, nil
	}
	return remaining, nil
}

// Ping implements cache.Store.
func (s *Store) Ping(ctx context.Context) error {
	_, err := s.queries.CacheGet(ctx, s.prefix+"__ping__")
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("cache: database ping: %w", err)
	}
	return nil
}

func encodeValue(value any) (string, error) {
	switch typed := value.(type) {
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case int:
		return strconv.Itoa(typed), nil
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
}
