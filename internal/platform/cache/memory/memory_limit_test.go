package memory

import (
	"context"
	"fmt"
	"testing"
)

func TestStoreStaysBounded(t *testing.T) {
	store := NewWithLimit("t-", 50)
	defer store.Close()
	ctx := context.Background()
	for i := range 500 {
		if err := store.Set(ctx, fmt.Sprintf("k%d", i), i, 0); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if _, err := store.Increment(ctx, fmt.Sprintf("c%d", i), 1); err != nil {
			t.Fatalf("Increment: %v", err)
		}
	}
	store.mu.Lock()
	size := len(store.items)
	store.mu.Unlock()
	if size > 50 {
		t.Fatalf("store holds %d entries, want <= 50", size)
	}
	var last int
	if found, err := store.Get(ctx, "k499", &last); err != nil || !found || last != 499 {
		t.Fatalf("latest key lost: found=%v value=%d err=%v", found, last, err)
	}
}
