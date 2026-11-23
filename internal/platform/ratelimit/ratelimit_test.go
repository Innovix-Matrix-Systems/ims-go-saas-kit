package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestMemoryLimiterWindow(t *testing.T) {
	limiter := NewMemoryLimiter(2, time.Minute)
	defer limiter.Close()
	ctx := context.Background()

	if allowed, _ := limiter.Allow(ctx, "k"); !allowed {
		t.Fatal("first request denied")
	}
	if allowed, _ := limiter.Allow(ctx, "k"); !allowed {
		t.Fatal("second request denied")
	}
	allowed, retryAfter := limiter.Allow(ctx, "k")
	if allowed {
		t.Fatal("third request allowed, want denied")
	}
	if retryAfter <= 0 {
		t.Errorf("retryAfter = %v, want positive", retryAfter)
	}
	if allowed, _ := limiter.Allow(ctx, "other"); !allowed {
		t.Fatal("independent key denied")
	}
}
