package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache/memory"
)

func TestCacheLimiterWindow(t *testing.T) {
	ctx := context.Background()
	store := memory.New("rl-")
	defer store.Close()

	limiter := NewCacheLimiter(store, 2, time.Minute)
	defer limiter.Close()

	if allowed, _ := limiter.Allow(ctx, "key"); !allowed {
		t.Fatal("first request denied")
	}
	if allowed, _ := limiter.Allow(ctx, "key"); !allowed {
		t.Fatal("second request denied")
	}
	allowed, retryAfter := limiter.Allow(ctx, "key")
	if allowed {
		t.Fatal("third request allowed, want denied")
	}
	if retryAfter <= 0 {
		t.Fatalf("retryAfter = %v, want positive", retryAfter)
	}
	if allowed, _ := limiter.Allow(ctx, "other"); !allowed {
		t.Fatal("independent key denied")
	}
}
