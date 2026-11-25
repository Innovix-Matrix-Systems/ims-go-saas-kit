// Package cachetest provides the shared cache contract suite that every
// driver must satisfy.
package cachetest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/cache"
)

type contractProfile struct {
	Name string
	Age  int `json:"age"`
}

// RunContract exercises the cache.Store contract against a driver.
func RunContract(t *testing.T, newStore func(t *testing.T) cache.Store) {
	t.Helper()
	ctx := context.Background()

	t.Run("SetGetString", func(t *testing.T) {
		store := newStore(t)
		if err := store.Set(ctx, "greeting", "hello", time.Minute); err != nil {
			t.Fatalf("Set() error = %v", err)
		}
		var value string
		found, err := store.Get(ctx, "greeting", &value)
		if err != nil || !found || value != "hello" {
			t.Fatalf("Get() = %v, %v, %q", found, err, value)
		}
	})

	t.Run("SetGetStruct", func(t *testing.T) {
		store := newStore(t)
		profile := contractProfile{Name: "acme", Age: 42}
		if err := store.Set(ctx, "profile", profile, time.Minute); err != nil {
			t.Fatalf("Set() error = %v", err)
		}
		var decoded contractProfile
		found, err := store.Get(ctx, "profile", &decoded)
		if err != nil || !found || decoded != profile {
			t.Fatalf("Get() = %v, %v, %+v", found, err, decoded)
		}
	})

	t.Run("Miss", func(t *testing.T) {
		store := newStore(t)
		var value string
		found, err := store.Get(ctx, "absent", &value)
		if err != nil || found {
			t.Fatalf("Get(miss) = %v, %v; want false, nil", found, err)
		}
		if exists, err := store.Exists(ctx, "absent"); err != nil || exists {
			t.Fatalf("Exists(miss) = %v, %v", exists, err)
		}
		if ttl, err := store.TTL(ctx, "absent"); err != nil || ttl != cache.TTLMissing {
			t.Fatalf("TTL(miss) = %v, %v; want TTLMissing", ttl, err)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		store := newStore(t)
		if err := store.Set(ctx, "temp", "value", time.Minute); err != nil {
			t.Fatalf("Set() error = %v", err)
		}
		if err := store.Delete(ctx, "temp"); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		if exists, _ := store.Exists(ctx, "temp"); exists {
			t.Fatal("Exists() after delete = true")
		}
	})

	t.Run("TTLExpiry", func(t *testing.T) {
		store := newStore(t)
		if err := store.Set(ctx, "short", "value", 20*time.Millisecond); err != nil {
			t.Fatalf("Set() error = %v", err)
		}
		time.Sleep(60 * time.Millisecond)
		var value string
		if found, _ := store.Get(ctx, "short", &value); found {
			t.Fatal("Get(expired) still found")
		}
	})

	t.Run("TTLNoExpiry", func(t *testing.T) {
		store := newStore(t)
		if err := store.Set(ctx, "forever", "value", 0); err != nil {
			t.Fatalf("Set() error = %v", err)
		}
		ttl, err := store.TTL(ctx, "forever")
		if err != nil || ttl != cache.TTLNoExpiry {
			t.Fatalf("TTL() = %v, %v; want TTLNoExpiry", ttl, err)
		}
	})

	t.Run("Increment", func(t *testing.T) {
		store := newStore(t)
		first, err := store.Increment(ctx, "counter", 1)
		if err != nil || first != 1 {
			t.Fatalf("Increment() = %d, %v", first, err)
		}
		second, err := store.Increment(ctx, "counter", 2)
		if err != nil || second != 3 {
			t.Fatalf("Increment() = %d, %v", second, err)
		}
		var decoded int64
		found, err := store.Get(ctx, "counter", &decoded)
		if err != nil || !found || decoded != 3 {
			t.Fatalf("Get(counter) = %v, %v, %d", found, err, decoded)
		}
	})

	t.Run("IncrementPreservesTTL", func(t *testing.T) {
		store := newStore(t)
		if _, err := store.Increment(ctx, "window", 1); err != nil {
			t.Fatalf("Increment() error = %v", err)
		}
		if err := store.Set(ctx, "window", int64(1), time.Minute); err != nil {
			t.Fatalf("Set() error = %v", err)
		}
		if _, err := store.Increment(ctx, "window", 1); err != nil {
			t.Fatalf("Increment() error = %v", err)
		}
		ttl, err := store.TTL(ctx, "window")
		if err != nil || ttl <= 0 {
			t.Fatalf("TTL() after increment = %v, %v; want positive", ttl, err)
		}
	})

	t.Run("Clear", func(t *testing.T) {
		store := newStore(t)
		for i := 0; i < 3; i++ {
			key := fmt.Sprintf("clear-%d", i)
			if err := store.Set(ctx, key, "value", time.Minute); err != nil {
				t.Fatalf("Set() error = %v", err)
			}
		}
		if err := store.Clear(ctx); err != nil {
			t.Fatalf("Clear() error = %v", err)
		}
		for i := 0; i < 3; i++ {
			key := fmt.Sprintf("clear-%d", i)
			if exists, _ := store.Exists(ctx, key); exists {
				t.Fatalf("Exists(%q) after Clear = true", key)
			}
		}
	})

	t.Run("SetNX", func(t *testing.T) {
		store := newStore(t)
		created, err := store.SetNX(ctx, "once", "first", time.Minute)
		if err != nil || !created {
			t.Fatalf("SetNX(first) = %v, %v; want true, nil", created, err)
		}
		created, err = store.SetNX(ctx, "once", "second", time.Minute)
		if err != nil || created {
			t.Fatalf("SetNX(second) = %v, %v; want false, nil", created, err)
		}
		var value string
		found, err := store.Get(ctx, "once", &value)
		if err != nil || !found || value != "first" {
			t.Fatalf("Get(once) = %v, %v, %q; want first", found, err, value)
		}
	})

	t.Run("Remember", func(t *testing.T) {
		store := newStore(t)
		calls := 0
		compute := func(ctx context.Context) (string, error) {
			calls++
			return "computed", nil
		}
		value, err := cache.Remember(ctx, store, "remember", time.Minute, compute)
		if err != nil || value != "computed" || calls != 1 {
			t.Fatalf("Remember(first) = %q, %v, calls=%d", value, err, calls)
		}
		value, err = cache.Remember(ctx, store, "remember", time.Minute, compute)
		if err != nil || value != "computed" || calls != 1 {
			t.Fatalf("Remember(cached) = %q, %v, calls=%d", value, err, calls)
		}
	})

	t.Run("Ping", func(t *testing.T) {
		store := newStore(t)
		if err := store.Ping(ctx); err != nil {
			t.Fatalf("Ping() error = %v", err)
		}
	})
}
