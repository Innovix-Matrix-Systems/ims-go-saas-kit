package maintenance

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Checker is the per-process cached reader of the maintenance state. It
// re-reads the store at most once per interval; while one refresh runs, other
// callers keep using the last known state, so a slow database never queues
// requests behind it. A failed read keeps the last known state (logged).
type Checker struct {
	store    Store
	interval time.Duration
	logger   *slog.Logger
	now      func() time.Time

	mu        sync.RWMutex
	state     State
	fetchedAt time.Time
	loaded    bool

	refresh sync.Mutex
}

// NewChecker creates a cached checker over store.
func NewChecker(store Store, interval time.Duration, logger *slog.Logger) *Checker {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Checker{store: store, interval: interval, logger: logger, now: time.Now}
}

// Current returns the (at most interval-old) maintenance state.
func (c *Checker) Current(ctx context.Context) State {
	c.mu.RLock()
	state, fresh, loaded := c.state, c.now().Sub(c.fetchedAt) < c.interval, c.loaded
	c.mu.RUnlock()
	if loaded && fresh {
		return state
	}
	if loaded {
		// Someone else is refreshing: serve the last known state.
		if !c.refresh.TryLock() {
			return state
		}
	} else {
		c.refresh.Lock()
	}
	defer c.refresh.Unlock()

	c.mu.RLock()
	state, fresh, loaded = c.state, c.now().Sub(c.fetchedAt) < c.interval, c.loaded
	c.mu.RUnlock()
	if loaded && fresh {
		return state
	}
	next, err := c.store.Get(ctx)
	if err != nil {
		c.logger.Warn("maintenance state could not be read; keeping the last known state", "error", err, "down", state.Down)
		c.mu.Lock()
		// Retry after another interval, not on every request.
		c.fetchedAt, c.loaded = c.now(), true
		c.mu.Unlock()
		return state
	}
	c.set(next)
	return next
}

// IsDown reports whether the app is in maintenance mode.
func (c *Checker) IsDown(ctx context.Context) bool { return c.Current(ctx).Down }

func (c *Checker) set(state State) {
	c.mu.Lock()
	c.state, c.fetchedAt, c.loaded = state, c.now(), true
	c.mu.Unlock()
}
