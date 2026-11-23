package notifications

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache"
	cachememory "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache/memory"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database/dbtest"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

func setupNotificationDB(t *testing.T) (context.Context, Repository, *pgxpool.Pool, cache.Store) {
	t.Helper()
	queries, pool := dbtest.Queries(t)
	dbtest.Clean(t, pool,
		"notification_deliveries", "notification_reads", "notification_targets", "notifications",
		"personal_access_tokens", "role_has_permissions", "model_has_roles", "users", "roles", "permissions",
		"branches", "tenants",
	)
	ctx := context.Background()

	statements := []string{
		`INSERT INTO tenants (id, name, code) VALUES (1, 'Tenant One', 'TEN1')`,
		`INSERT INTO branches (id, tenant_id, name) VALUES (1, 1, 'Main Branch')`,
		`INSERT INTO users (id, tenant_id, branch_id, name, email, password) VALUES
			(1, 1, 1, 'User One', 'u1@example.com', 'x'),
			(2, 1, 1, 'User Two', 'u2@example.com', 'x'),
			(3, 1, NULL, 'User Three', 'u3@example.com', 'x')`,
		`INSERT INTO roles (id, tenant_id, name) VALUES (1, 1, 'Tenant Role')`,
		`INSERT INTO model_has_roles (role_id, model_type, model_id) VALUES (1, 'user', 2)`,
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("seed: %v\n%s", err, statement)
		}
	}

	tx := database.NewTxManager(&database.Conn{Name: "test", Driver: "pgsql", Pool: pool})
	store := cachememory.New("notifications-int-")
	t.Cleanup(store.Close)
	return ctx, NewRepository(queries, tx), pool, store
}

func createNotification(t *testing.T, service *Service, ctx context.Context, targets ...TargetInput) *Notification {
	t.Helper()
	notification, _, err := service.Create(ctx, tenancy.Scope{TenantID: 1}, 0, CreateInput{
		EventType: EventRenewalReminder,
		Priority:  PriorityInfo,
		Title:     "Integration notification",
		Targets:   targets,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return notification
}

func TestFanOutIsIdempotentAcrossRetries(t *testing.T) {
	ctx, repo, pool, store := setupNotificationDB(t)
	service := NewService(repo, store, nil, nil, nil)

	notification := createNotification(t, service, ctx, TargetInput{Type: TargetTenant})

	if err := service.ProcessFanOut(ctx, notification.ID); err != nil {
		t.Fatalf("ProcessFanOut() error = %v", err)
	}
	// A retried fan-out must not duplicate deliveries or counters.
	if err := service.ProcessFanOut(ctx, notification.ID); err != nil {
		t.Fatalf("ProcessFanOut() retry error = %v", err)
	}

	var deliveries int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM notification_deliveries WHERE notification_id = $1", notification.ID).Scan(&deliveries); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if deliveries != 3 {
		t.Fatalf("deliveries = %d, want 3", deliveries)
	}

	for _, userID := range []int64{1, 2, 3} {
		if got := service.UnreadCount(ctx, 1, userID); got != 1 {
			t.Errorf("user %d unread = %d, want 1 after retry", userID, got)
		}
	}

	// Cache flush mid-flight: the recompute fallback returns the DB truth.
	if err := store.Delete(ctx, unreadKey(1, 2)); err != nil {
		t.Fatalf("delete counter: %v", err)
	}
	if got := service.UnreadCount(ctx, 1, 2); got != 1 {
		t.Errorf("recomputed unread = %d, want 1", got)
	}
}

func TestVisibilityMatrixAndFeed(t *testing.T) {
	ctx, repo, _, store := setupNotificationDB(t)
	service := NewService(repo, store, nil, nil, nil)

	byUser := createNotification(t, service, ctx, TargetInput{Type: TargetUser, ID: int64Ptr(2)})
	byRole := createNotification(t, service, ctx, TargetInput{Type: TargetRole, ID: int64Ptr(1)})
	byBranch := createNotification(t, service, ctx, TargetInput{Type: TargetBranch, ID: int64Ptr(1)})
	byTenant := createNotification(t, service, ctx, TargetInput{Type: TargetTenant})
	byGlobal := createNotification(t, service, ctx, TargetInput{Type: TargetGlobal})

	cases := []struct {
		name         string
		notification *Notification
		user         int64
		roles        []int64
		branch       int64
		want         bool
	}{
		{"user-target for target user", byUser, 2, nil, 1, true},
		{"user-target for other user", byUser, 1, nil, 1, false},
		{"role-target for assigned user", byRole, 2, []int64{1}, 1, true},
		{"role-target for unassigned user", byRole, 1, nil, 1, false},
		{"branch-target for branch user", byBranch, 1, nil, 1, true},
		{"branch-target for branchless user", byBranch, 3, nil, 0, false},
		{"tenant-target for tenant user", byTenant, 1, nil, 1, true},
		{"global-target for tenant user", byGlobal, 1, nil, 1, false},
	}
	for _, tc := range cases {
		visible, err := repo.VisibleToUser(ctx, tc.notification.ID, Visibility{
			UserID:   tc.user,
			TenantID: 1,
			BranchID: tc.branch,
			RoleIDs:  tc.roles,
		})
		if err != nil {
			t.Fatalf("%s: VisibleToUser() error = %v", tc.name, err)
		}
		if visible != tc.want {
			t.Errorf("%s: visible = %v, want %v", tc.name, visible, tc.want)
		}
	}

	// The feed shows only the notifications addressable to the user.
	page, err := service.Feed(ctx, tenancy.Scope{TenantID: 1}, 2, FeedFilter{Limit: 20})
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	ids := make(map[int64]bool, len(page.Items))
	for _, item := range page.Items {
		ids[item.ID] = true
	}
	if !ids[byUser.ID] || !ids[byRole.ID] || !ids[byBranch.ID] || !ids[byTenant.ID] {
		t.Errorf("feed for user 2 missing addressable notifications: %+v", ids)
	}
	if ids[byGlobal.ID] {
		t.Error("feed for a tenant user must not include GLOBAL-targeted notifications")
	}
}

func TestMarkAllReadAndCounters(t *testing.T) {
	ctx, repo, pool, store := setupNotificationDB(t)
	service := NewService(repo, store, nil, nil, nil)

	first := createNotification(t, service, ctx, TargetInput{Type: TargetUser, ID: int64Ptr(2)})
	second := createNotification(t, service, ctx, TargetInput{Type: TargetTenant})
	if err := service.ProcessFanOut(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessFanOut(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if got := service.UnreadCount(ctx, 1, 2); got != 2 {
		t.Fatalf("unread before mark-all = %d, want 2", got)
	}

	for i := 0; i < 2; i++ {
		if err := service.ProcessMarkAllRead(ctx, 2); err != nil {
			t.Fatalf("ProcessMarkAllRead() error = %v", err)
		}
	}

	var reads int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM notification_reads WHERE user_id = 2").Scan(&reads); err != nil {
		t.Fatal(err)
	}
	// Only the two addressable notifications; the repeat run inserts nothing.
	if reads != 2 {
		t.Fatalf("reads = %d, want 2", reads)
	}
	if got := service.UnreadCount(ctx, 1, 2); got != 0 {
		t.Fatalf("unread after mark-all = %d, want 0", got)
	}
}

func TestDeleteRepairsCounters(t *testing.T) {
	ctx, repo, _, store := setupNotificationDB(t)
	service := NewService(repo, store, nil, nil, nil)

	notification := createNotification(t, service, ctx, TargetInput{Type: TargetTenant})
	if err := service.ProcessFanOut(ctx, notification.ID); err != nil {
		t.Fatal(err)
	}
	if got := service.UnreadCount(ctx, 1, 1); got != 1 {
		t.Fatalf("unread before delete = %d", got)
	}

	if err := service.Delete(ctx, tenancy.Scope{TenantID: 1}, notification.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if got := service.UnreadCount(ctx, 1, 1); got != 0 {
		t.Fatalf("unread after delete = %d, want 0", got)
	}
}

func TestFeedCursorStableUnderInserts(t *testing.T) {
	ctx, repo, pool, store := setupNotificationDB(t)
	service := NewService(repo, store, nil, nil, nil)

	// Five tenant-targeted notifications with explicit, spaced timestamps.
	for i := 1; i <= 5; i++ {
		if _, err := pool.Exec(ctx, `
			INSERT INTO notifications (tenant_id, event_type, priority, title, created_at)
			VALUES (1, 'renewal.reminder', 'INFO', $1, now() - make_interval(mins => $2))`,
			"seed", 10-i); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO notification_targets (tenant_id, notification_id, target_type)
		SELECT 1, id, 'TENANT' FROM notifications`); err != nil {
		t.Fatal(err)
	}

	scope := tenancy.Scope{TenantID: 1}
	page1, err := service.Feed(ctx, scope, 1, FeedFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page1.Items) != 2 || page1.NextCursor == nil {
		t.Fatalf("page1 = %+v", page1)
	}
	cursor := DecodeCursor(*page1.NextCursor)
	page2a, err := service.Feed(ctx, scope, 1, FeedFilter{Limit: 2, Cursor: cursor})
	if err != nil {
		t.Fatal(err)
	}

	// A newer notification arrives between pages.
	if _, err := pool.Exec(ctx, `
		INSERT INTO notifications (tenant_id, event_type, priority, title, created_at)
		VALUES (1, 'renewal.reminder', 'INFO', 'newest', now())`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO notification_targets (tenant_id, notification_id, target_type)
		SELECT 1, id, 'TENANT' FROM notifications WHERE title = 'newest'`); err != nil {
		t.Fatal(err)
	}

	page2b, err := service.Feed(ctx, scope, 1, FeedFilter{Limit: 2, Cursor: cursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2a.Items) != len(page2b.Items) {
		t.Fatalf("page 2 changed after insert: %d vs %d", len(page2a.Items), len(page2b.Items))
	}
	for i := range page2a.Items {
		if page2a.Items[i].ID != page2b.Items[i].ID {
			t.Fatalf("cursor page shifted after insert: %d vs %d", page2a.Items[i].ID, page2b.Items[i].ID)
		}
	}

	// The first page now starts with the newest notification.
	fresh, err := service.Feed(ctx, scope, 1, FeedFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Items[0].Title != "newest" {
		t.Fatalf("first page head = %q, want newest", fresh.Items[0].Title)
	}
}
