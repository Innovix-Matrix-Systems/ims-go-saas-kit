// Package memory provides a concurrency-safe in-process cache driver with
// per-entry TTLs. It is the default driver for local development and tests.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache"
)

type item struct {
	data      []byte
	number    *int64
	expiresAt time.Time
}

func (i item) expired(now time.Time) bool {
	return !i.expiresAt.IsZero() && !now.Before(i.expiresAt)
}

// DefaultMaxEntries bounds a store created with New.
const DefaultMaxEntries = 100000

// Store is an in-process cache store implementing cache.Store. It holds at
// most maxEntries keys: when full, expired entries are swept first, then an
// arbitrary tenth of the entries is evicted (a cache may always forget).
type Store struct {
	prefix     string
	maxEntries int

	mu    sync.Mutex
	items map[string]item

	now  func() time.Time
	stop chan struct{}
	once sync.Once
}

var _ cache.Store = (*Store)(nil)

// New creates a memory store bounded to DefaultMaxEntries. The prefix
// namespaces every key.
func New(prefix string) *Store {
	return NewWithLimit(prefix, DefaultMaxEntries)
}

// NewWithLimit creates a memory store holding at most maxEntries keys
// (maxEntries <= 0 uses DefaultMaxEntries).
func NewWithLimit(prefix string, maxEntries int) *Store {
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}
	store := &Store{
		prefix:     prefix,
		maxEntries: maxEntries,
		items:      map[string]item{},
		now:        time.Now,
		stop:       make(chan struct{}),
	}
	go store.janitor()
	return store
}

// makeRoom must be called with s.mu held before inserting fullKey.
func (s *Store) makeRoom(fullKey string) {
	if _, exists := s.items[fullKey]; exists || len(s.items) < s.maxEntries {
		return
	}
	now := s.now()
	for key, stored := range s.items {
		if stored.expired(now) {
			delete(s.items, key)
		}
	}
	if len(s.items) < s.maxEntries {
		return
	}
	evict := s.maxEntries/10 + 1
	for key := range s.items {
		if evict == 0 {
			break
		}
		delete(s.items, key)
		evict--
	}
}

// Close stops the background janitor.
func (s *Store) Close() {
	s.once.Do(func() { close(s.stop) })
}

// Get implements cache.Store.
func (s *Store) Get(ctx context.Context, key string, dest any) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fullKey := s.prefix + key
	stored, ok := s.items[fullKey]
	if !ok {
		return false, nil
	}
	if stored.expired(s.now()) {
		delete(s.items, fullKey)
		return false, nil
	}
	if stored.number != nil {
		data, err := json.Marshal(*stored.number)
		if err != nil {
			return false, fmt.Errorf("cache: encode %q: %w", key, err)
		}
		if err := json.Unmarshal(data, dest); err != nil {
			return false, fmt.Errorf("cache: decode %q: %w", key, err)
		}
		return true, nil
	}
	if err := json.Unmarshal(stored.data, dest); err != nil {
		return false, fmt.Errorf("cache: decode %q: %w", key, err)
	}
	return true, nil
}

// Set implements cache.Store. A ttl <= 0 stores the value without expiry.
func (s *Store) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	entry, err := encodeItem(key, value)
	if err != nil {
		return err
	}
	if ttl > 0 {
		entry.expiresAt = s.now().Add(ttl)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.makeRoom(s.prefix + key)
	s.items[s.prefix+key] = entry
	return nil
}

// SetNX implements cache.Store. It stores the value only when the key is absent
// or expired, reporting whether this caller created it.
func (s *Store) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	entry, err := encodeItem(key, value)
	if err != nil {
		return false, err
	}
	if ttl > 0 {
		entry.expiresAt = s.now().Add(ttl)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	fullKey := s.prefix + key
	if stored, ok := s.items[fullKey]; ok && !stored.expired(s.now()) {
		return false, nil
	}
	s.makeRoom(fullKey)
	s.items[fullKey] = entry
	return true, nil
}

func encodeItem(key string, value any) (item, error) {
	entry := item{}
	switch typed := value.(type) {
	case int64:
		entry.number = &typed
	case int:
		number := int64(typed)
		entry.number = &number
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return item{}, fmt.Errorf("cache: encode %q: %w", key, err)
		}
		entry.data = data
	}
	return entry, nil
}

// Delete implements cache.Store.
func (s *Store) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, s.prefix+key)
	return nil
}

// Exists implements cache.Store.
func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fullKey := s.prefix + key
	stored, ok := s.items[fullKey]
	if !ok {
		return false, nil
	}
	if stored.expired(s.now()) {
		delete(s.items, fullKey)
		return false, nil
	}
	return true, nil
}

// Clear implements cache.Store.
func (s *Store) Clear(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = map[string]item{}
	return nil
}

// Increment implements cache.Store. Existing TTLs are preserved; a new key is
// created without expiry (callers set a TTL explicitly when needed).
func (s *Store) Increment(ctx context.Context, key string, delta int64) (int64, error) {
	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()

	fullKey := s.prefix + key
	stored, ok := s.items[fullKey]
	if ok && stored.expired(now) {
		delete(s.items, fullKey)
		ok = false
	}
	if !ok {
		value := delta
		s.makeRoom(fullKey)
		s.items[fullKey] = item{number: &value}
		return value, nil
	}
	if stored.number == nil {
		return 0, fmt.Errorf("cache: key %q does not store an integer", key)
	}
	*stored.number += delta
	s.items[fullKey] = stored
	return *stored.number, nil
}

// TTL implements cache.Store.
func (s *Store) TTL(ctx context.Context, key string) (time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, ok := s.items[s.prefix+key]
	if !ok {
		return cache.TTLMissing, nil
	}
	now := s.now()
	if stored.expired(now) {
		delete(s.items, s.prefix+key)
		return cache.TTLMissing, nil
	}
	if stored.expiresAt.IsZero() {
		return cache.TTLNoExpiry, nil
	}
	return stored.expiresAt.Sub(now), nil
}

// Ping implements cache.Store.
func (s *Store) Ping(ctx context.Context) error {
	return nil
}

func (s *Store) janitor() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			now := s.now()
			s.mu.Lock()
			for key, stored := range s.items {
				if stored.expired(now) {
					delete(s.items, key)
				}
			}
			s.mu.Unlock()
		}
	}
}
