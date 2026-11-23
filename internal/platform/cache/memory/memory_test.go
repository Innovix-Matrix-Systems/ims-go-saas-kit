package memory

import (
	"context"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache"
)

func TestStoreBasics(t *testing.T) {
	ctx := context.Background()
	store := New("test-")
	defer store.Close()

	var value string
	found, err := store.Get(ctx, "missing", &value)
	if err != nil || found {
		t.Fatalf("Get(missing) = %v, %v; want false, nil", found, err)
	}

	if err := store.Set(ctx, "greeting", "hello", time.Minute); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	found, err = store.Get(ctx, "greeting", &value)
	if err != nil || !found || value != "hello" {
		t.Fatalf("Get() = %v, %v, %q", found, err, value)
	}

	exists, err := store.Exists(ctx, "greeting")
	if err != nil || !exists {
		t.Fatalf("Exists() = %v, %v", exists, err)
	}

	ttl, err := store.TTL(ctx, "greeting")
	if err != nil || ttl <= 0 || ttl > time.Minute {
		t.Fatalf("TTL() = %v, %v", ttl, err)
	}

	if err := store.Delete(ctx, "greeting"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if exists, _ := store.Exists(ctx, "greeting"); exists {
		t.Fatal("Exists() after delete = true")
	}
}

func TestStoreTTLExpiry(t *testing.T) {
	ctx := context.Background()
	store := New("test-")
	defer store.Close()

	if err := store.Set(ctx, "short", "value", time.Millisecond); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	var value string
	found, err := store.Get(ctx, "short", &value)
	if err != nil || found {
		t.Fatalf("Get(expired) = %v, %v; want miss", found, err)
	}
	if ttl, _ := store.TTL(ctx, "short"); ttl != cache.TTLMissing {
		t.Fatalf("TTL(expired) = %v, want TTLMissing", ttl)
	}
}

func TestStoreIncrementPreservesTTL(t *testing.T) {
	ctx := context.Background()
	store := New("test-")
	defer store.Close()

	count, err := store.Increment(ctx, "counter", 1)
	if err != nil || count != 1 {
		t.Fatalf("Increment() = %d, %v", count, err)
	}
	if err := store.Set(ctx, "counter", count, time.Minute); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	count, err = store.Increment(ctx, "counter", 2)
	if err != nil || count != 3 {
		t.Fatalf("Increment() = %d, %v", count, err)
	}
	ttl, err := store.TTL(ctx, "counter")
	if err != nil || ttl <= 0 {
		t.Fatalf("TTL after increment = %v, %v; want preserved", ttl, err)
	}

	var decoded int64
	found, err := store.Get(ctx, "counter", &decoded)
	if err != nil || !found || decoded != 3 {
		t.Fatalf("Get(counter) = %v, %v, %d", found, err, decoded)
	}
}

func TestStoreClearAndPrefixIsolation(t *testing.T) {
	ctx := context.Background()
	first := New("a-")
	second := New("b-")
	defer first.Close()
	defer second.Close()

	_ = first.Set(ctx, "key", "first", time.Minute)
	_ = second.Set(ctx, "key", "second", time.Minute)

	if err := first.Clear(ctx); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	var value string
	if found, _ := first.Get(ctx, "key", &value); found {
		t.Fatal("first store still has value after Clear")
	}
	if found, _ := second.Get(ctx, "key", &value); !found || value != "second" {
		t.Fatalf("second store value = %q, found = %v", value, found)
	}
}

func TestRemember(t *testing.T) {
	ctx := context.Background()
	store := New("test-")
	defer store.Close()

	calls := 0
	compute := func(ctx context.Context) (string, error) {
		calls++
		return "computed", nil
	}

	value, err := cache.Remember(ctx, store, "remember", time.Minute, compute)
	if err != nil || value != "computed" || calls != 1 {
		t.Fatalf("Remember first = %q, %v, calls %d", value, err, calls)
	}
	value, err = cache.Remember(ctx, store, "remember", time.Minute, compute)
	if err != nil || value != "computed" || calls != 1 {
		t.Fatalf("Remember cached = %q, %v, calls %d", value, err, calls)
	}
}
