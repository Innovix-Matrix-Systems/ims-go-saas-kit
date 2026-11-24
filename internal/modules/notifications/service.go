package notifications

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

const (
	unreadTTL         = 7 * 24 * time.Hour
	readAllBeforeTTL  = 90 * 24 * time.Hour
	fanOutChunkSize   = 500
	decrementChunk    = 500
	enabledSettingKey = "notification.enabled"
)

// Service implements the notification rules: creation and target validation,
// fan-out, reads, cached unread counters with a database fallback, and the
// cursor feed.
type Service struct {
	repo     Repository
	cache    cache.Store
	queue    queue.Engine
	settings *settings.Service
	logger   *slog.Logger
	channels channelOptions
}

// NewService creates the notification service.
func NewService(repo Repository, store cache.Store, engine queue.Engine, settingsService *settings.Service, logger *slog.Logger) *Service {
	return &Service{repo: repo, cache: store, queue: engine, settings: settingsService, logger: logger}
}

// Create validates and persists a notification plus its targets, then
// dispatches the fan-out job.
func (s *Service) Create(ctx context.Context, scope tenancy.Scope, actorID int64, input CreateInput) (*Notification, []Target, error) {
	if _, ok := Events[input.EventType]; !ok {
		return nil, nil, apperr.Validation(map[string][]string{
			"event_type": {"The selected event type is invalid."},
		})
	}
	if _, ok := Priorities[input.Priority]; !ok {
		return nil, nil, apperr.Validation(map[string][]string{
			"priority": {"The selected priority is invalid."},
		})
	}
	channels, err := normalizeChannels(input.EventType, input.Channels)
	if err != nil {
		return nil, nil, err
	}
	input.Channels = channels
	tenantID := resolvedTenant(scope, input.TenantID)
	if err := s.validateTargets(ctx, scope, tenantID, input.Targets); err != nil {
		return nil, nil, err
	}
	input.TenantID = tenantID
	input.Targets = normalizeTargets(input.Targets, tenantID)
	if input.CreatedBy == nil && actorID > 0 {
		input.CreatedBy = &actorID
	}

	notification, targets, err := s.repo.CreateWithTargets(ctx, input)
	if err != nil {
		return nil, nil, err
	}
	s.dispatchFanOut(ctx, scope, *notification)
	return notification, targets, nil
}

// Update replaces a notification's content and targets (tenant immutable) and
// delta-fans-out to newly addressed users.
func (s *Service) Update(ctx context.Context, scope tenancy.Scope, actorID, id int64, input UpdateInput) (*Notification, []Target, error) {
	existing, err := s.scopedGet(ctx, scope, id)
	if err != nil {
		return nil, nil, err
	}
	if _, ok := Events[input.EventType]; !ok {
		return nil, nil, apperr.Validation(map[string][]string{
			"event_type": {"The selected event type is invalid."},
		})
	}
	if _, ok := Priorities[input.Priority]; !ok {
		return nil, nil, apperr.Validation(map[string][]string{
			"priority": {"The selected priority is invalid."},
		})
	}
	if err := s.validateTargets(ctx, scope, existing.TenantID, input.Targets); err != nil {
		return nil, nil, err
	}
	if actorID > 0 {
		input.UpdatedBy = &actorID
	}

	notification, targets, err := s.repo.UpdateWithTargets(ctx, id, scope.AllTenants(), scope.EffectiveTenantID(), input)
	if err != nil {
		return nil, nil, err
	}
	s.dispatchFanOut(ctx, scope, *notification)
	return notification, targets, nil
}

// Delete removes a notification and enqueues unread-counter repairs for its
// audience.
func (s *Service) Delete(ctx context.Context, scope tenancy.Scope, id int64) error {
	existing, err := s.scopedGet(ctx, scope, id)
	if err != nil {
		return err
	}
	userIDs, err := s.repo.ListUnreadDeliveryUserIDs(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, id, scope.AllTenants(), scope.EffectiveTenantID()); err != nil {
		return err
	}
	s.dispatchDecrement(ctx, tenantValue(existing.TenantID), userIDs)
	return nil
}

// Get loads a notification within the caller's scope.
func (s *Service) Get(ctx context.Context, scope tenancy.Scope, id int64) (*Notification, error) {
	return s.scopedGet(ctx, scope, id)
}

func (s *Service) scopedGet(ctx context.Context, scope tenancy.Scope, id int64) (*Notification, error) {
	return s.repo.GetInTenant(ctx, id, scope.AllTenants(), scope.EffectiveTenantID())
}

// List returns notifications with pagination and filters (admin view). The
// filter is tenant-scoped: all_tenants is only true for internal scopes.
func (s *Service) List(ctx context.Context, scope tenancy.Scope, filter ListFilter) ([]Notification, int64, error) {
	filter.AllTenants = scope.AllTenants()
	filter.TenantID = scope.EffectiveTenantID()
	items, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.Count(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Targets loads a notification's addressability rules.
func (s *Service) Targets(ctx context.Context, notificationID int64) ([]Target, error) {
	return s.repo.ListTargets(ctx, notificationID)
}

// MarkRead records a read for the user (404 when the notification does not
// address them) and repairs the cached unread counter.
func (s *Service) MarkRead(ctx context.Context, scope tenancy.Scope, userID, notificationID int64) error {
	visibility, err := s.visibility(ctx, scope, userID)
	if err != nil {
		return err
	}
	visible, err := s.repo.VisibleToUser(ctx, notificationID, visibility)
	if err != nil {
		return err
	}
	if !visible {
		return apperr.Newf(apperr.ResourceNotFound, "Notification with ID '%d' was not found.", notificationID)
	}

	inserted, err := s.repo.InsertRead(ctx, optionalInt64(visibility.TenantID), optionalInt64(visibility.BranchID), notificationID, userID, time.Now().UTC())
	if err != nil {
		return err
	}
	if inserted > 0 {
		s.decrementUnread(ctx, visibility.TenantID, userID)
	}
	return nil
}

// MarkAllRead enqueues the bulk read job for the user.
func (s *Service) MarkAllRead(ctx context.Context, scope tenancy.Scope, userID int64) error {
	if _, err := s.visibility(ctx, scope, userID); err != nil {
		return err
	}
	branchID, allBranches := scope.ListBranchID(), scope.EffectiveTenantID() > 0 && scope.ListBranchID() == 0
	if s.queue == nil {
		return s.ProcessMarkAllRead(ctx, userID, branchID, allBranches)
	}
	enqueueCtx := tenancy.WithScope(ctx, tenancy.Scope{
		TenantID: scope.EffectiveTenantID(),
		BranchID: scope.EffectiveBranchID(),
	})
	payload := markAllReadPayload{UserID: userID, BranchID: branchID, AllBranches: allBranches}
	if err := s.queue.Enqueue(enqueueCtx, KindMarkAllRead, payload, queue.EnqueueOptions{}); err != nil {
		s.logWarn(ctx, "mark-all-read enqueue failed", "user_id", userID, "error", err)
		return apperr.Wrap(apperr.ServiceUnavailable, "The request could not be queued.", err)
	}
	return nil
}

// UnreadCount returns the cached unread count, recomputing from the database
// when the cache key is missing (counters are an optimization only).
func (s *Service) UnreadCount(ctx context.Context, tenantID, userID int64) int {
	key := unreadKey(tenantID, userID)
	var count int64
	if found, err := s.cache.Get(ctx, key, &count); err == nil && found {
		return int(count)
	}
	dbCount, err := s.repo.CountUnread(ctx, userID, tenantID, tenantID == 0)
	if err != nil {
		s.logWarn(ctx, "unread count recompute failed", "user_id", userID, "error", err)
		return 0
	}
	_ = s.cache.Set(ctx, key, dbCount, unreadTTL)
	return int(dbCount)
}

// FeedFilter carries feed query options.
type FeedFilter struct {
	Limit      int32
	UnreadOnly bool
	Priority   string
	Cursor     *Cursor
}

// FeedPage is one cursor page of the feed.
type FeedPage struct {
	Items       []FeedItem
	PerPage     int
	NextCursor  *string
	PrevCursor  *string
	UnreadCount int
}

// Feed returns the user's notification feed.
func (s *Service) Feed(ctx context.Context, scope tenancy.Scope, userID int64, filter FeedFilter) (*FeedPage, error) {
	visibility, err := s.visibility(ctx, scope, userID)
	if err != nil {
		return nil, err
	}

	params := FeedParams{
		Visibility:    visibility,
		UnreadOnly:    filter.UnreadOnly,
		Priority:      filter.Priority,
		ReadAllBefore: s.readAllBefore(ctx, visibility.TenantID, userID),
		Limit:         filter.Limit + 1,
	}
	backward := false
	if filter.Cursor != nil {
		createdAt := filter.Cursor.timestamp()
		cursorID := filter.Cursor.ID
		params.CursorCreatedAt = &createdAt
		params.CursorID = &cursorID
		backward = !filter.Cursor.PointsToNext
	}

	items, err := s.repo.Feed(ctx, params, backward)
	if err != nil {
		return nil, err
	}
	hasMore := len(items) > int(filter.Limit)
	if hasMore {
		items = items[:filter.Limit]
	}
	if backward {
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
	}

	page := &FeedPage{Items: items, PerPage: int(filter.Limit)}
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		next := EncodeCursor(last.CreatedAt, last.ID, true)
		page.NextCursor = &next
	}
	if filter.Cursor != nil && len(items) > 0 {
		first := items[0]
		prev := EncodeCursor(first.CreatedAt, first.ID, false)
		page.PrevCursor = &prev
	}
	page.UnreadCount = s.UnreadCount(ctx, visibility.TenantID, userID)
	return page, nil
}

// ProcessFanOut executes the fan-out job: resolve target users, insert
// idempotent deliveries in chunks and increment counters only for newly
// inserted rows (retry-safe).
func (s *Service) ProcessFanOut(ctx context.Context, notificationID int64) error {
	notification, err := s.repo.Get(ctx, notificationID)
	if err != nil {
		if apperr.Is(err, apperr.ResourceNotFound) {
			return nil
		}
		return err
	}
	targets, err := s.repo.ListTargets(ctx, notificationID)
	if err != nil {
		return err
	}
	userIDs, err := s.repo.ResolveUserIDs(ctx, notification.TenantID, targets)
	if err != nil {
		return err
	}
	tenantID := tenantValue(notification.TenantID)

	for start := 0; start < len(userIDs); start += fanOutChunkSize {
		end := start + fanOutChunkSize
		if end > len(userIDs) {
			end = len(userIDs)
		}
		inserted, err := s.repo.InsertDeliveries(ctx, notification.TenantID, notificationID, userIDs[start:end])
		if err != nil {
			return err
		}
		for _, userID := range inserted {
			s.incrementUnread(ctx, tenantID, userID)
		}
		// Mail, SMS and push go to the newly addressed users only, so a
		// retried fan-out never sends twice.
		s.deliver(ctx, *notification, inserted)
	}
	return nil
}

// ProcessMarkAllRead marks every non-expired notification addressable to the
// user as read, then sets the read-all marker and resets the counter.
// branchID and allBranches name the branch notices the feed showed (both
// empty: the user's own branch).
func (s *Service) ProcessMarkAllRead(ctx context.Context, userID, branchID int64, allBranches bool) error {
	visibility, err := s.jobVisibility(ctx, userID, branchID, allBranches)
	if err != nil {
		return err
	}
	readAt := time.Now().UTC()
	if _, err := s.repo.MarkAllRead(ctx, visibility, readAt); err != nil {
		return err
	}
	_ = s.cache.Set(ctx, readAllBeforeKey(visibility.TenantID, userID), readAt.Unix(), readAllBeforeTTL)
	_ = s.cache.Set(ctx, unreadKey(visibility.TenantID, userID), 0, unreadTTL)
	return nil
}

// ProcessCounterDecrement repairs unread counters after a notification is
// deleted (chunked payloads; missing keys are simply skipped).
func (s *Service) ProcessCounterDecrement(ctx context.Context, tenantID int64, userIDs []int64) error {
	for _, userID := range userIDs {
		s.decrementUnread(ctx, tenantID, userID)
	}
	return nil
}

func (s *Service) dispatchFanOut(ctx context.Context, scope tenancy.Scope, notification Notification) {
	if s.queue == nil || !s.fanOutEnabled(ctx) {
		return
	}
	enqueueCtx := tenancy.WithScope(ctx, tenancy.Scope{
		TenantID: tenantValue(notification.TenantID),
		BranchID: scope.EffectiveBranchID(),
	})
	queueName := queueForPriority(notification.Priority)
	if err := s.queue.Enqueue(enqueueCtx, KindFanOut, fanOutPayload{NotificationID: notification.ID}, queue.EnqueueOptions{Queue: &queueName}); err != nil {
		// The notification exists; a failed dispatch is logged and can be
		// recovered by re-saving the notification.
		s.logWarn(ctx, "notification fan-out enqueue failed", "notification_id", notification.ID, "error", err)
	}
}

func (s *Service) dispatchDecrement(ctx context.Context, tenantID int64, userIDs []int64) {
	if len(userIDs) == 0 {
		return
	}
	if s.queue == nil {
		_ = s.ProcessCounterDecrement(ctx, tenantID, userIDs)
		return
	}
	for start := 0; start < len(userIDs); start += decrementChunk {
		end := start + decrementChunk
		if end > len(userIDs) {
			end = len(userIDs)
		}
		payload := decrementUnreadPayload{TenantID: tenantID, UserIDs: userIDs[start:end]}
		if err := s.queue.Enqueue(ctx, KindDecrementUnread, payload, queue.EnqueueOptions{}); err != nil {
			s.logWarn(ctx, "decrement-counters enqueue failed", "error", err)
		}
	}
}

// visibility builds the caller's addressability inputs (roles and branch come
// from the user row).
func (s *Service) visibility(ctx context.Context, scope tenancy.Scope, userID int64) (Visibility, error) {
	homeBranch, userTenant, err := s.repo.UserScope(ctx, userID)
	if err != nil {
		return Visibility{}, err
	}
	roleIDs, err := s.repo.UserRoleIDs(ctx, userID)
	if err != nil {
		return Visibility{}, err
	}
	if scope.AllTenants() {
		return Visibility{
			UserID:      userID,
			TenantID:    0,
			AllTenants:  true,
			IsNonTenant: true,
			BranchID:    homeBranch,
			RoleIDs:     roleIDs,
		}, nil
	}
	tenantID := scope.EffectiveTenantID()
	if tenantID == 0 {
		tenantID = userTenant
	}
	branchID, allBranches := branchVisibility(scope, homeBranch)
	return Visibility{
		UserID:      userID,
		TenantID:    tenantID,
		AllTenants:  false,
		IsNonTenant: tenantID == 0,
		BranchID:    branchID,
		AllBranches: allBranches,
		RoleIDs:     roleIDs,
	}, nil
}

// branchVisibility picks the branch notices a feed shows: those of the
// branch the member switched to (or the acting branch), of every branch
// when they look at all of them, else of their own branch.
func branchVisibility(scope tenancy.Scope, homeBranch int64) (int64, bool) {
	if scope.EffectiveTenantID() == 0 {
		return homeBranch, false
	}
	if branch := scope.ListBranchID(); branch > 0 {
		return branch, false
	}
	return 0, true
}

// jobVisibility resolves visibility inside workers: the envelope scope wins
// when it carries a tenant, otherwise the user's own tenant is used.
func (s *Service) jobVisibility(ctx context.Context, userID, listBranch int64, allBranches bool) (Visibility, error) {
	branchID, userTenant, err := s.repo.UserScope(ctx, userID)
	if err != nil {
		return Visibility{}, err
	}
	roleIDs, err := s.repo.UserRoleIDs(ctx, userID)
	if err != nil {
		return Visibility{}, err
	}
	tenantID := userTenant
	if scope, ok := tenancy.FromContext(ctx); ok && scope.EffectiveTenantID() > 0 {
		tenantID = scope.EffectiveTenantID()
	}
	visibility := Visibility{
		UserID:      userID,
		TenantID:    tenantID,
		AllTenants:  tenantID == 0,
		IsNonTenant: tenantID == 0,
		BranchID:    branchID,
		RoleIDs:     roleIDs,
	}
	if tenantID > 0 {
		switch {
		case allBranches:
			visibility.BranchID, visibility.AllBranches = 0, true
		case listBranch > 0:
			visibility.BranchID = listBranch
		}
	}
	return visibility, nil
}

func (s *Service) fanOutEnabled(ctx context.Context) bool {
	if s.settings == nil {
		return true
	}
	enabled, err := s.settings.GetBool(ctx, tenancy.Scope{Internal: true}, enabledSettingKey)
	if err != nil {
		return true
	}
	return enabled
}

func (s *Service) incrementUnread(ctx context.Context, tenantID, userID int64) {
	key := unreadKey(tenantID, userID)
	created, err := s.cache.SetNX(ctx, key, int64(1), unreadTTL)
	if err != nil {
		s.logWarn(ctx, "unread increment failed", "user_id", userID, "error", err)
		return
	}
	if created {
		return
	}
	if _, err := s.cache.Increment(ctx, key, 1); err != nil {
		s.logWarn(ctx, "unread increment failed", "user_id", userID, "error", err)
	}
}

func (s *Service) decrementUnread(ctx context.Context, tenantID, userID int64) {
	key := unreadKey(tenantID, userID)
	var count int64
	found, err := s.cache.Get(ctx, key, &count)
	if err != nil || !found {
		// Nothing cached: the next read recomputes from the database.
		return
	}
	if count <= 1 {
		_ = s.cache.Set(ctx, key, int64(0), unreadTTL)
		return
	}
	if _, err := s.cache.Increment(ctx, key, -1); err != nil {
		s.logWarn(ctx, "unread decrement failed", "user_id", userID, "error", err)
	}
}

func (s *Service) readAllBefore(ctx context.Context, tenantID, userID int64) int64 {
	var marker int64
	found, err := s.cache.Get(ctx, readAllBeforeKey(tenantID, userID), &marker)
	if err != nil || !found {
		return 0
	}
	return marker
}

func (s *Service) validateTargets(ctx context.Context, scope tenancy.Scope, tenantID *int64, targets []TargetInput) error {
	fields := map[string][]string{}
	hasNonGlobal := false
	for i, target := range targets {
		key := fmt.Sprintf("targets.%d", i)
		switch target.Type {
		case TargetUser, TargetRole, TargetBranch:
			hasNonGlobal = true
			if target.ID == nil || *target.ID < 1 {
				fields[key+".id"] = append(fields[key+".id"], "The target id field is required.")
			}
		case TargetTenant:
			hasNonGlobal = true
			if target.ID != nil {
				fields[key+".id"] = append(fields[key+".id"], "The target id field must be null.")
			}
		case TargetGlobal:
			if target.ID != nil {
				fields[key+".id"] = append(fields[key+".id"], "The target id field must be null.")
			}
		default:
			fields[key+".type"] = append(fields[key+".type"], "The selected target type is invalid.")
		}
	}
	if hasNonGlobal && (tenantID == nil || *tenantID < 1) {
		fields["tenant_id"] = append(fields["tenant_id"], "The tenant id field is required.")
	}
	if len(fields) > 0 {
		return apperr.Validation(fields)
	}

	tenantValueInt := int64(0)
	if tenantID != nil {
		tenantValueInt = *tenantID
	}
	for i, target := range targets {
		if target.ID == nil {
			continue
		}
		key := fmt.Sprintf("targets.%d.id", i)
		var exists bool
		var err error
		switch target.Type {
		case TargetUser:
			exists, err = s.repo.UserExistsInTenant(ctx, *target.ID, tenantValueInt)
		case TargetBranch:
			exists, err = s.repo.BranchExistsInTenant(ctx, *target.ID, tenantValueInt)
		case TargetRole:
			var scopedTenant *int64
			if tenantID != nil {
				scopedTenant = tenantID
			}
			exists, err = s.repo.RoleExistsForTenant(ctx, *target.ID, scopedTenant)
		}
		if err != nil {
			return err
		}
		if !exists {
			fields[key] = append(fields[key], "The selected target id is invalid.")
		}
	}
	if len(fields) > 0 {
		return apperr.Validation(fields)
	}
	_ = scope
	return nil
}

func normalizeTargets(targets []TargetInput, tenantID *int64) []TargetInput {
	if len(targets) > 0 {
		return targets
	}
	if tenantID != nil {
		return []TargetInput{{Type: TargetTenant}}
	}
	return []TargetInput{{Type: TargetGlobal}}
}

func resolvedTenant(scope tenancy.Scope, explicit *int64) *int64 {
	if scope.Internal {
		return explicit
	}
	if tenant := scope.EffectiveTenantID(); tenant > 0 {
		return &tenant
	}
	return nil
}

func tenantValue(tenantID *int64) int64 {
	if tenantID == nil {
		return 0
	}
	return *tenantID
}

func unreadKey(tenantID, userID int64) string {
	return fmt.Sprintf("notif:t%d:u%d:unread", tenantID, userID)
}

func readAllBeforeKey(tenantID, userID int64) string {
	return fmt.Sprintf("notif:t%d:u%d:read_all_before", tenantID, userID)
}

func (s *Service) logWarn(ctx context.Context, message string, args ...any) {
	if s.logger == nil {
		return
	}
	s.logger.WarnContext(ctx, message, args...)
}
