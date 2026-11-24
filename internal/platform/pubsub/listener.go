package pubsub

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// Listener holds one dedicated Postgres connection that LISTENs on Channel
// and dispatches what arrives to the hub: one connection per server process,
// whatever the number of streams (the notifier pattern River uses).
type Listener struct {
	dsn    string
	hub    *Hub
	logger *slog.Logger

	minBackoff, maxBackoff time.Duration
}

// NewListener creates a listener for the hub. The DSN must reach Postgres
// directly or through a session-mode pooler.
func NewListener(dsn string, hub *Hub, logger *slog.Logger) *Listener {
	if logger == nil {
		logger = slog.Default()
	}
	return &Listener{dsn: dsn, hub: hub, logger: logger, minBackoff: time.Second, maxBackoff: 30 * time.Second}
}

// Run listens until ctx is cancelled, reconnecting with backoff, then closes
// the hub. After every reconnect it sends a resync event: notifications sent
// while it was away are lost.
func (l *Listener) Run(ctx context.Context) {
	defer l.hub.Close()
	backoff := l.minBackoff
	connected := false
	for ctx.Err() == nil {
		err := l.listen(ctx, func() {
			if connected {
				l.hub.Dispatch(Event{Kind: KindResync})
			}
			connected = true
			backoff = l.minBackoff
		})
		if ctx.Err() != nil {
			return
		}
		l.logger.WarnContext(ctx, "realtime listener disconnected; reconnecting", "error", err, "retry_in", backoff.String())
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, l.maxBackoff)
	}
}

// listen runs one connection's life; ready is called once LISTEN succeeded.
func (l *Listener) listen(ctx context.Context, ready func()) error {
	conn, err := pgx.Connect(ctx, l.dsn)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()
	if _, err := conn.Exec(ctx, "LISTEN "+pgx.Identifier{Channel}.Sanitize()); err != nil {
		return err
	}
	ready()
	for {
		notification, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		event, err := decode(notification.Payload)
		if err != nil {
			l.logger.WarnContext(ctx, "realtime event ignored: unreadable payload", "error", err)
			continue
		}
		l.hub.Dispatch(event)
	}
}
