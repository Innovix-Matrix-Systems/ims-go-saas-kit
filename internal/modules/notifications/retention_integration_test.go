package notifications

import (
	"context"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database/dbtest"
)

// TestPurgeExpiredNotifications covers the notifications.purge-expired
// retention task: only notifications expired for more than a week go.
func TestPurgeExpiredNotifications(t *testing.T) {
	queries, pool := dbtest.Queries(t)
	dbtest.Clean(t, pool, "notification_deliveries", "notification_reads", "notification_targets", "notifications")
	ctx := context.Background()

	insert := func(title, expires string) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`INSERT INTO notifications (event_type, title, expires_at) VALUES ('test', $1, `+expires+`)`, title,
		); err != nil {
			t.Fatalf("insert %s: %v", title, err)
		}
	}
	insert("long expired", "now() - interval '10 days'")
	insert("just expired", "now() - interval '1 day'")
	insert("never expires", "NULL")

	purged, err := queries.PurgeExpiredNotifications(ctx)
	if err != nil {
		t.Fatalf("PurgeExpiredNotifications() error = %v", err)
	}
	if purged != 1 {
		t.Fatalf("purged = %d, want 1", purged)
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notifications`).Scan(&left); err != nil {
		t.Fatalf("count: %v", err)
	}
	if left != 2 {
		t.Fatalf("remaining = %d, want 2", left)
	}
}
