package notifications

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache"
	cachememory "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache/memory"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

func newMemoryStore(t *testing.T) cache.Store {
	t.Helper()
	store := cachememory.New("notifications-test-")
	t.Cleanup(func() { _ = store.Clear(context.Background()) })
	return store
}

// fakeRepo implements Repository with per-test function hooks; unset hooks
// fail loudly.
type fakeRepo struct {
	createWithTargets func(ctx context.Context, input CreateInput) (*Notification, []Target, error)
	get               func(ctx context.Context, id int64) (*Notification, error)
	getInTenant       func(ctx context.Context, id int64, allTenants bool, tenantID int64) (*Notification, error)
	updateWithTargets func(ctx context.Context, id int64, allTenants bool, tenantID int64, input UpdateInput) (*Notification, []Target, error)
	deleteFn          func(ctx context.Context, id int64, allTenants bool, tenantID int64) error
	listFn            func(ctx context.Context, filter ListFilter) ([]Notification, error)
	countFn           func(ctx context.Context, filter ListFilter) (int64, error)
	listTargets       func(ctx context.Context, notificationID int64) ([]Target, error)
	resolveUserIDs    func(ctx context.Context, tenantID *int64, targets []Target) ([]int64, error)
	insertDeliveries  func(ctx context.Context, tenantID *int64, notificationID int64, userIDs []int64) ([]int64, error)
	unreadUsers       func(ctx context.Context, notificationID int64) ([]int64, error)
	insertRead        func(ctx context.Context, tenantID, branchID *int64, notificationID, userID int64, readAt time.Time) (int64, error)
	visibleToUser     func(ctx context.Context, notificationID int64, v Visibility) (bool, error)
	markAllRead       func(ctx context.Context, v Visibility, readAt time.Time) (int64, error)
	feed              func(ctx context.Context, params FeedParams, backward bool) ([]FeedItem, error)
	countUnread       func(ctx context.Context, userID int64, tenantID int64, allTenants bool) (int64, error)
	userScope         func(ctx context.Context, userID int64) (int64, int64, error)
	userRoleIDs       func(ctx context.Context, userID int64) ([]int64, error)
	userExists        func(ctx context.Context, id, tenantID int64) (bool, error)
	branchExists      func(ctx context.Context, id, tenantID int64) (bool, error)
	roleExists        func(ctx context.Context, id int64, tenantID *int64) (bool, error)
}

var errNotStubbed = errors.New("repository method not stubbed in test")

func (f *fakeRepo) Default() {
	if f.createWithTargets == nil {
		f.createWithTargets = func(context.Context, CreateInput) (*Notification, []Target, error) { return nil, nil, errNotStubbed }
	}
	if f.get == nil {
		f.get = func(context.Context, int64) (*Notification, error) { return nil, errNotStubbed }
	}
	if f.getInTenant == nil {
		f.getInTenant = func(context.Context, int64, bool, int64) (*Notification, error) { return nil, errNotStubbed }
	}
	if f.updateWithTargets == nil {
		f.updateWithTargets = func(context.Context, int64, bool, int64, UpdateInput) (*Notification, []Target, error) {
			return nil, nil, errNotStubbed
		}
	}
	if f.deleteFn == nil {
		f.deleteFn = func(context.Context, int64, bool, int64) error { return errNotStubbed }
	}
	if f.listFn == nil {
		f.listFn = func(context.Context, ListFilter) ([]Notification, error) { return nil, errNotStubbed }
	}
	if f.countFn == nil {
		f.countFn = func(context.Context, ListFilter) (int64, error) { return 0, errNotStubbed }
	}
	if f.listTargets == nil {
		f.listTargets = func(context.Context, int64) ([]Target, error) { return nil, errNotStubbed }
	}
	if f.resolveUserIDs == nil {
		f.resolveUserIDs = func(context.Context, *int64, []Target) ([]int64, error) { return nil, errNotStubbed }
	}
	if f.insertDeliveries == nil {
		f.insertDeliveries = func(context.Context, *int64, int64, []int64) ([]int64, error) { return nil, errNotStubbed }
	}
	if f.unreadUsers == nil {
		f.unreadUsers = func(context.Context, int64) ([]int64, error) { return nil, errNotStubbed }
	}
	if f.insertRead == nil {
		f.insertRead = func(context.Context, *int64, *int64, int64, int64, time.Time) (int64, error) { return 0, errNotStubbed }
	}
	if f.visibleToUser == nil {
		f.visibleToUser = func(context.Context, int64, Visibility) (bool, error) { return false, errNotStubbed }
	}
	if f.markAllRead == nil {
		f.markAllRead = func(context.Context, Visibility, time.Time) (int64, error) { return 0, errNotStubbed }
	}
	if f.feed == nil {
		f.feed = func(context.Context, FeedParams, bool) ([]FeedItem, error) { return nil, errNotStubbed }
	}
	if f.countUnread == nil {
		f.countUnread = func(context.Context, int64, int64, bool) (int64, error) { return 0, errNotStubbed }
	}
	if f.userScope == nil {
		f.userScope = func(context.Context, int64) (int64, int64, error) { return 0, 1, nil }
	}
	if f.userRoleIDs == nil {
		f.userRoleIDs = func(context.Context, int64) ([]int64, error) { return []int64{}, nil }
	}
	if f.userExists == nil {
		f.userExists = func(context.Context, int64, int64) (bool, error) { return false, errNotStubbed }
	}
	if f.branchExists == nil {
		f.branchExists = func(context.Context, int64, int64) (bool, error) { return false, errNotStubbed }
	}
	if f.roleExists == nil {
		f.roleExists = func(context.Context, int64, *int64) (bool, error) { return false, errNotStubbed }
	}
}

func (f *fakeRepo) CreateWithTargets(ctx context.Context, input CreateInput) (*Notification, []Target, error) {
	return f.createWithTargets(ctx, input)
}

func (f *fakeRepo) Get(ctx context.Context, id int64) (*Notification, error) {
	return f.get(ctx, id)
}

func (f *fakeRepo) GetInTenant(ctx context.Context, id int64, allTenants bool, tenantID int64) (*Notification, error) {
	return f.getInTenant(ctx, id, allTenants, tenantID)
}

func (f *fakeRepo) UpdateWithTargets(ctx context.Context, id int64, allTenants bool, tenantID int64, input UpdateInput) (*Notification, []Target, error) {
	return f.updateWithTargets(ctx, id, allTenants, tenantID, input)
}

func (f *fakeRepo) Delete(ctx context.Context, id int64, allTenants bool, tenantID int64) error {
	return f.deleteFn(ctx, id, allTenants, tenantID)
}

func (f *fakeRepo) List(ctx context.Context, filter ListFilter) ([]Notification, error) {
	return f.listFn(ctx, filter)
}

func (f *fakeRepo) Count(ctx context.Context, filter ListFilter) (int64, error) {
	return f.countFn(ctx, filter)
}

func (f *fakeRepo) ListTargets(ctx context.Context, notificationID int64) ([]Target, error) {
	return f.listTargets(ctx, notificationID)
}

func (f *fakeRepo) ResolveUserIDs(ctx context.Context, tenantID *int64, targets []Target) ([]int64, error) {
	return f.resolveUserIDs(ctx, tenantID, targets)
}

func (f *fakeRepo) InsertDeliveries(ctx context.Context, tenantID *int64, notificationID int64, userIDs []int64) ([]int64, error) {
	return f.insertDeliveries(ctx, tenantID, notificationID, userIDs)
}

func (f *fakeRepo) ListUnreadDeliveryUserIDs(ctx context.Context, notificationID int64) ([]int64, error) {
	return f.unreadUsers(ctx, notificationID)
}

func (f *fakeRepo) InsertRead(ctx context.Context, tenantID, branchID *int64, notificationID, userID int64, readAt time.Time) (int64, error) {
	return f.insertRead(ctx, tenantID, branchID, notificationID, userID, readAt)
}

func (f *fakeRepo) VisibleToUser(ctx context.Context, notificationID int64, v Visibility) (bool, error) {
	return f.visibleToUser(ctx, notificationID, v)
}

func (f *fakeRepo) MarkAllRead(ctx context.Context, v Visibility, readAt time.Time) (int64, error) {
	return f.markAllRead(ctx, v, readAt)
}

func (f *fakeRepo) Feed(ctx context.Context, params FeedParams, backward bool) ([]FeedItem, error) {
	return f.feed(ctx, params, backward)
}

func (f *fakeRepo) CountUnread(ctx context.Context, userID int64, tenantID int64, allTenants bool) (int64, error) {
	return f.countUnread(ctx, userID, tenantID, allTenants)
}

func (f *fakeRepo) UserScope(ctx context.Context, userID int64) (int64, int64, error) {
	return f.userScope(ctx, userID)
}

func (f *fakeRepo) UserRoleIDs(ctx context.Context, userID int64) ([]int64, error) {
	return f.userRoleIDs(ctx, userID)
}

func (f *fakeRepo) UserExistsInTenant(ctx context.Context, id, tenantID int64) (bool, error) {
	return f.userExists(ctx, id, tenantID)
}

func (f *fakeRepo) BranchExistsInTenant(ctx context.Context, id, tenantID int64) (bool, error) {
	return f.branchExists(ctx, id, tenantID)
}

func (f *fakeRepo) RoleExistsForTenant(ctx context.Context, id int64, tenantID *int64) (bool, error) {
	return f.roleExists(ctx, id, tenantID)
}

// Channels and preferences are covered by the integration tests.
func (f *fakeRepo) Recipients(context.Context, []int64) ([]delivery.Recipient, error) {
	return nil, nil
}

func (f *fakeRepo) DisabledChannels(context.Context, string, []int64) (map[int64]map[string]bool, error) {
	return nil, nil
}

func (f *fakeRepo) Preferences(context.Context, int64) (map[string]bool, error) { return nil, nil }

func (f *fakeRepo) SetPreference(context.Context, int64, string, string, bool) error { return nil }

func TestCursorRoundTrip(t *testing.T) {
	created := time.Date(2026, 10, 17, 12, 30, 0, 0, time.UTC)
	encoded := EncodeCursor(created, 42, true)
	cursor := DecodeCursor(encoded)
	if cursor == nil {
		t.Fatal("DecodeCursor() = nil")
	}
	if cursor.ID != 42 || !cursor.PointsToNext {
		t.Fatalf("cursor = %+v", cursor)
	}
	if !cursor.timestamp().Equal(created) {
		t.Fatalf("cursor timestamp = %v, want %v", cursor.timestamp(), created)
	}

	for _, raw := range []string{"", "not-base64!!!", "bm90LWpzb24"} {
		if got := DecodeCursor(raw); got != nil {
			t.Errorf("DecodeCursor(%q) = %+v, want nil (first page)", raw, got)
		}
	}
}

func TestQueueRoutingByPriority(t *testing.T) {
	if got := queueForPriority(PriorityCritical); got != queue.QueueCritical {
		t.Errorf("critical priority routed to %q", got)
	}
	for _, priority := range []string{PriorityInfo, PriorityWarning} {
		if got := queueForPriority(priority); got != queue.QueueDefault {
			t.Errorf("%s priority routed to %q", priority, got)
		}
	}
}

func TestProcessFanOutCountsOnlyNewDeliveries(t *testing.T) {
	store := newMemoryStore(t)
	repo := &fakeRepo{}
	repo.Default()
	tenant := int64(1)
	repo.get = func(context.Context, int64) (*Notification, error) {
		return &Notification{ID: 10, TenantID: &tenant, Priority: PriorityInfo}, nil
	}
	repo.listTargets = func(context.Context, int64) ([]Target, error) {
		return []Target{{NotificationID: 10, TargetType: TargetTenant}}, nil
	}
	repo.resolveUserIDs = func(context.Context, *int64, []Target) ([]int64, error) {
		return []int64{1, 2, 3}, nil
	}
	first := true
	repo.insertDeliveries = func(_ context.Context, _ *int64, _ int64, userIDs []int64) ([]int64, error) {
		if first {
			// Simulate ON CONFLICT DO NOTHING: user 1 already had a delivery.
			first = false
			return []int64{2, 3}, nil
		}
		return nil, nil
	}

	service := NewService(repo, store, nil, nil, nil)
	if err := service.ProcessFanOut(context.Background(), 10); err != nil {
		t.Fatalf("ProcessFanOut() error = %v", err)
	}
	// Retry: existing deliveries conflict, no counter changes.
	if err := service.ProcessFanOut(context.Background(), 10); err != nil {
		t.Fatalf("ProcessFanOut() retry error = %v", err)
	}

	if got := service.UnreadCount(context.Background(), 1, 2); got != 1 {
		t.Errorf("user 2 unread = %d, want 1", got)
	}
	if got := service.UnreadCount(context.Background(), 1, 3); got != 1 {
		t.Errorf("user 3 unread = %d, want 1", got)
	}
	// User 1 never got an increment (no new delivery); the recompute fallback
	// serves the count from the database.
	repo.countUnread = func(context.Context, int64, int64, bool) (int64, error) { return 3, nil }
	if got := service.UnreadCount(context.Background(), 1, 1); got != 3 {
		t.Errorf("user 1 recomputed unread = %d, want 3", got)
	}
}

func TestUnreadCountRecomputesAndCaches(t *testing.T) {
	store := newMemoryStore(t)
	repo := &fakeRepo{}
	repo.Default()
	calls := 0
	repo.countUnread = func(context.Context, int64, int64, bool) (int64, error) {
		calls++
		return 7, nil
	}

	service := NewService(repo, store, nil, nil, nil)
	if got := service.UnreadCount(context.Background(), 1, 5); got != 7 {
		t.Fatalf("UnreadCount() = %d, want 7", got)
	}
	if got := service.UnreadCount(context.Background(), 1, 5); got != 7 {
		t.Fatalf("UnreadCount() cached = %d, want 7", got)
	}
	if calls != 1 {
		t.Fatalf("CountUnread called %d times, want 1 (second call from cache)", calls)
	}
}

func TestFeedPageAssemblyWithCursor(t *testing.T) {
	store := newMemoryStore(t)
	repo := &fakeRepo{}
	repo.Default()
	base := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
	rows := []FeedItem{
		{ID: 5, Title: "five", CreatedAt: base.Add(5 * time.Minute)},
		{ID: 4, Title: "four", CreatedAt: base.Add(4 * time.Minute)},
		{ID: 3, Title: "three", CreatedAt: base.Add(3 * time.Minute)},
	}
	seenBackward := false
	repo.feed = func(_ context.Context, params FeedParams, backward bool) ([]FeedItem, error) {
		if backward {
			seenBackward = true
			return rows, nil
		}
		if params.CursorCreatedAt != nil {
			return rows[1:], nil
		}
		return rows, nil
	}
	repo.countUnread = func(context.Context, int64, int64, bool) (int64, error) { return 2, nil }

	service := NewService(repo, store, nil, nil, nil)
	scope := tenancy.Scope{TenantID: 1}

	page, err := service.Feed(context.Background(), scope, 5, FeedFilter{Limit: 2})
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	if len(page.Items) != 2 || page.Items[0].ID != 5 || page.Items[1].ID != 4 {
		t.Fatalf("page items = %+v", page.Items)
	}
	if page.NextCursor == nil {
		t.Fatal("next cursor missing on a truncated page")
	}
	if page.PrevCursor != nil {
		t.Fatal("prev cursor must be nil on the first page")
	}
	if page.UnreadCount != 2 {
		t.Fatalf("unread count = %d", page.UnreadCount)
	}

	cursor := DecodeCursor(*page.NextCursor)
	if cursor == nil {
		t.Fatal("next cursor did not decode")
	}
	second, err := service.Feed(context.Background(), scope, 5, FeedFilter{Limit: 2, Cursor: cursor})
	if err != nil {
		t.Fatalf("Feed(cursor) error = %v", err)
	}
	if second.PrevCursor == nil {
		t.Fatal("prev cursor missing when paging with a cursor")
	}
	if second.NextCursor != nil {
		t.Fatal("next cursor set on the final page")
	}

	// Backward paging reverses the rows into feed order.
	prev := DecodeCursor(*second.PrevCursor)
	if _, err := service.Feed(context.Background(), scope, 5, FeedFilter{Limit: 2, Cursor: prev}); err != nil {
		t.Fatalf("Feed(prev) error = %v", err)
	}
	if !seenBackward {
		t.Fatal("backward query was not used for the prev cursor")
	}
}

func TestValidateTargetsMatrix(t *testing.T) {
	repo := &fakeRepo{}
	repo.Default()
	repo.userExists = func(_ context.Context, id, _ int64) (bool, error) { return id == 1, nil }
	repo.branchExists = func(_ context.Context, id, _ int64) (bool, error) { return id == 2, nil }
	repo.roleExists = func(_ context.Context, id int64, _ *int64) (bool, error) { return id == 3, nil }
	service := NewService(repo, newMemoryStore(t), nil, nil, nil)

	internal := tenancy.Scope{Internal: true}
	if _, _, err := service.Create(context.Background(), internal, 0, CreateInput{
		EventType: EventRenewalReminder,
		Priority:  PriorityInfo,
		Title:     "x",
		Targets:   []TargetInput{{Type: TargetUser, ID: int64Ptr(1)}},
	}); err == nil {
		t.Fatal("tenant-less non-global target accepted")
	}

	if _, _, err := service.Create(context.Background(), internal, 0, CreateInput{
		EventType: EventRenewalReminder,
		Priority:  PriorityInfo,
		Title:     "x",
		Targets:   []TargetInput{{Type: TargetUser, ID: int64Ptr(99)}},
		TenantID:  int64Ptr(1),
	}); err == nil {
		t.Fatal("non-existent target user accepted")
	}

	if _, _, err := service.Create(context.Background(), internal, 0, CreateInput{
		EventType: "unknown.event",
		Priority:  PriorityInfo,
		Title:     "x",
		Targets:   []TargetInput{{Type: TargetGlobal}},
	}); err == nil {
		t.Fatal("unknown event type accepted")
	}

	// GLOBAL target with no tenant is valid; targets normalize for empty lists.
	var captured CreateInput
	repo.createWithTargets = func(_ context.Context, input CreateInput) (*Notification, []Target, error) {
		captured = input
		return &Notification{ID: 1}, []Target{}, nil
	}
	if _, _, err := service.Create(context.Background(), internal, 0, CreateInput{
		EventType: EventRenewalReminder,
		Priority:  PriorityCritical,
		Title:     "hello",
		Targets:   []TargetInput{{Type: TargetGlobal}},
	}); err != nil {
		t.Fatalf("GLOBAL create error = %v", err)
	}
	if captured.TenantID != nil {
		t.Fatalf("global notification tenant = %v, want nil", captured.TenantID)
	}

	// Tenant-scoped empty targets normalize to TENANT.
	if _, _, err := service.Create(context.Background(), tenancy.Scope{TenantID: 1}, 0, CreateInput{
		EventType: EventRenewalReminder,
		Priority:  PriorityInfo,
		Title:     "hello",
	}); err != nil {
		t.Fatalf("tenant create error = %v", err)
	}
	if len(captured.Targets) != 1 || captured.Targets[0].Type != TargetTenant {
		t.Fatalf("normalized targets = %+v", captured.Targets)
	}
}

func TestMarkReadIsIdempotentForCounters(t *testing.T) {
	store := newMemoryStore(t)
	repo := &fakeRepo{}
	repo.Default()
	repo.visibleToUser = func(context.Context, int64, Visibility) (bool, error) { return true, nil }
	inserted := int64(1)
	repo.insertRead = func(context.Context, *int64, *int64, int64, int64, time.Time) (int64, error) {
		result := inserted
		inserted = 0 // the second call hits the unique key
		return result, nil
	}

	service := NewService(repo, store, nil, nil, nil)
	service.incrementUnread(context.Background(), 1, 9)
	service.incrementUnread(context.Background(), 1, 9)

	scope := tenancy.Scope{TenantID: 1}
	if err := service.MarkRead(context.Background(), scope, 9, 100); err != nil {
		t.Fatalf("MarkRead() error = %v", err)
	}
	if got := service.UnreadCount(context.Background(), 1, 9); got != 1 {
		t.Fatalf("unread after first read = %d, want 1", got)
	}
	if err := service.MarkRead(context.Background(), scope, 9, 100); err != nil {
		t.Fatalf("MarkRead() repeat error = %v", err)
	}
	if got := service.UnreadCount(context.Background(), 1, 9); got != 1 {
		t.Fatalf("unread after duplicate read = %d, want 1", got)
	}
}

func int64Ptr(value int64) *int64 { return &value }
