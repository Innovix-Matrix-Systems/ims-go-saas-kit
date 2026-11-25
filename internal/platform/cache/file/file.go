// Package file implements a filesystem cache driver. Entries are stored as
// JSON envelopes named by the SHA-256 of the (prefixed) key. The driver
// coordinates increments within the process; concurrent writers from multiple
// processes sharing one directory are not coordinated (documented limitation).
package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/cache"
)

type envelope struct {
	Value     json.RawMessage `json:"v"`
	ExpiresAt int64           `json:"e,omitempty"`
}

// Store is a filesystem cache store implementing cache.Store.
type Store struct {
	dir    string
	prefix string

	mu sync.Mutex
}

var _ cache.Store = (*Store)(nil)

// New creates a file cache rooted at dir; prefix namespaces keys.
func New(dir, prefix string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache directory: %w", err)
	}
	return &Store{dir: dir, prefix: prefix}, nil
}

// Get implements cache.Store.
func (s *Store) Get(ctx context.Context, key string, dest any) (bool, error) {
	stored, ok, err := s.read(key)
	if err != nil || !ok {
		return false, err
	}
	if stored.ExpiresAt != 0 && time.Now().UnixNano() >= stored.ExpiresAt {
		_ = s.Delete(ctx, key)
		return false, nil
	}
	if err := json.Unmarshal(stored.Value, dest); err != nil {
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
	entry := envelope{Value: data}
	if ttl > 0 {
		entry.ExpiresAt = time.Now().Add(ttl).UnixNano()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write(key, entry)
}

// SetNX implements cache.Store. It stores the value only when the key is absent
// or expired, reporting whether this caller created it.
func (s *Store) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, ok, err := s.read(key)
	if err != nil {
		return false, err
	}
	if ok && (stored.ExpiresAt == 0 || time.Now().UnixNano() < stored.ExpiresAt) {
		return false, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return false, fmt.Errorf("cache: encode %q: %w", key, err)
	}
	entry := envelope{Value: data}
	if ttl > 0 {
		entry.ExpiresAt = time.Now().Add(ttl).UnixNano()
	}
	if err := s.write(key, entry); err != nil {
		return false, err
	}
	return true, nil
}

// Delete implements cache.Store.
func (s *Store) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.path(key))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cache: delete %q: %w", key, err)
	}
	return nil
}

// Exists implements cache.Store.
func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	stored, ok, err := s.read(key)
	if err != nil || !ok {
		return false, err
	}
	if stored.ExpiresAt != 0 && time.Now().UnixNano() >= stored.ExpiresAt {
		_ = s.Delete(ctx, key)
		return false, nil
	}
	return true, nil
}

// Clear implements cache.Store and removes every entry in the directory.
func (s *Store) Clear(ctx context.Context) error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("cache: read directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("cache: clear %q: %w", entry.Name(), err)
		}
	}
	return nil
}

// Increment implements cache.Store. Existing TTLs are preserved.
func (s *Store) Increment(ctx context.Context, key string, delta int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, ok, err := s.read(key)
	if err != nil {
		return 0, err
	}
	entry := envelope{}
	if ok {
		if stored.ExpiresAt != 0 && time.Now().UnixNano() >= stored.ExpiresAt {
			ok = false
		} else {
			entry = *stored
		}
	}

	var current int64
	if ok {
		if err := json.Unmarshal(entry.Value, &current); err != nil {
			return 0, fmt.Errorf("cache: key %q does not store an integer", key)
		}
	}
	current += delta

	data, err := json.Marshal(current)
	if err != nil {
		return 0, fmt.Errorf("cache: encode %q: %w", key, err)
	}
	entry.Value = data
	if err := s.write(key, entry); err != nil {
		return 0, err
	}
	return current, nil
}

// TTL implements cache.Store.
func (s *Store) TTL(ctx context.Context, key string) (time.Duration, error) {
	stored, ok, err := s.read(key)
	if err != nil {
		return 0, err
	}
	if !ok {
		return cache.TTLMissing, nil
	}
	if stored.ExpiresAt == 0 {
		return cache.TTLNoExpiry, nil
	}
	remaining := time.Until(time.Unix(0, stored.ExpiresAt))
	if remaining <= 0 {
		_ = s.Delete(ctx, key)
		return cache.TTLMissing, nil
	}
	return remaining, nil
}

// Ping implements cache.Store.
func (s *Store) Ping(ctx context.Context) error {
	info, err := os.Stat(s.dir)
	if err != nil {
		return fmt.Errorf("cache directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("cache path %q is not a directory", s.dir)
	}
	return nil
}

func (s *Store) read(key string) (*envelope, bool, error) {
	data, err := os.ReadFile(s.path(key))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("cache: read %q: %w", key, err)
	}
	var entry envelope
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false, fmt.Errorf("cache: decode entry %q: %w", key, err)
	}
	return &entry, true, nil
}

func (s *Store) write(key string, entry envelope) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("cache: encode entry %q: %w", key, err)
	}
	tmp, err := os.CreateTemp(s.dir, "tmp-*.json")
	if err != nil {
		return fmt.Errorf("cache: write %q: %w", key, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("cache: write %q: %w", key, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("cache: write %q: %w", key, err)
	}
	if err := os.Rename(tmpName, s.path(key)); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("cache: commit %q: %w", key, err)
	}
	return nil
}

func (s *Store) path(key string) string {
	sum := sha256.Sum256([]byte(s.prefix + key))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])+".json")
}
