package redis

import (
	"os"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/cache/cachetest"
)

// TestContract runs against a real Redis server when REDIS_TEST_ADDR is set
// (e.g. "127.0.0.1:6379"); otherwise it skips, like other integration suites.
func TestContract(t *testing.T) {
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR is not set; skipping redis contract test")
	}

	store, err := New(Options{Addr: addr, DB: 15, Prefix: "contract-"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cachetest.RunContract(t, func(t *testing.T) cache.Store {
		if err := store.Clear(t.Context()); err != nil {
			t.Fatalf("Clear() error = %v", err)
		}
		return store
	})
}
