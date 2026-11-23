package notifications

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/delivery"
)

// CreateInput carries notification creation input (targets included so the
// row and its targets commit together).
type CreateInput struct {
	TenantID  *int64
	EventType string
	Priority  string
	Title     string
	Body      *string
	Data      []byte
	GroupKey  *string
	CreatedBy *int64
	ExpiresAt *time.Time
	Targets   []TargetInput
	// Channels: the feed ("database") plus mail, sms, push (normalized by
	// the service).
	Channels []string
}

// UpdateInput carries notification update input (targets replace the old set).
type UpdateInput struct {
	EventType string
	Priority  string
	Title     string
	Body      *string
	Data      []byte
	GroupKey  *string
	UpdatedBy *int64
	ExpiresAt *time.Time
	Targets   []TargetInput
}

// TargetInput is one addressability rule.
type TargetInput struct {
	Type string
	ID   *int64
}

// ListFilter carries admin list filters. AllTenants is true only for
// internal scopes; tenant-bound callers are scoped to TenantID.
type ListFilter struct {
	AllTenants bool
	TenantID   int64
	Search     string
	Priority   string
	OrderBy    string
	OrderDir   string
	Limit      int32
	Offset     int32
}

// Visibility carries the caller's addressability inputs.
type Visibility struct {
	UserID      int64
	TenantID    int64
	AllTenants  bool
	IsNonTenant bool
	BranchID    int64
	RoleIDs     []int64
}

// FeedParams carries one feed page request.
type FeedParams struct {
	Visibility
	UnreadOnly      bool
	Priority        string
	ReadAllBefore   int64
	CursorCreatedAt *time.Time
	CursorID        *int64
	Limit           int32
}

// Repository is the persistence contract for notifications.
type Repository interface {
	CreateWithTargets(ctx context.Context, input CreateInput) (*Notification, []Target, error)
	Get(ctx context.Context, id int64) (*Notification, error)
	GetInTenant(ctx context.Context, id int64, allTenants bool, tenantID int64) (*Notification, error)
	UpdateWithTargets(ctx context.Context, id int64, allTenants bool, tenantID int64, input UpdateInput) (*Notification, []Target, error)
	Delete(ctx context.Context, id int64, allTenants bool, tenantID int64) error
	List(ctx context.Context, filter ListFilter) ([]Notification, error)
	Count(ctx context.Context, filter ListFilter) (int64, error)
	ListTargets(ctx context.Context, notificationID int64) ([]Target, error)
	ResolveUserIDs(ctx context.Context, tenantID *int64, targets []Target) ([]int64, error)
	InsertDeliveries(ctx context.Context, tenantID *int64, notificationID int64, userIDs []int64) ([]int64, error)
	ListUnreadDeliveryUserIDs(ctx context.Context, notificationID int64) ([]int64, error)
	InsertRead(ctx context.Context, tenantID, branchID *int64, notificationID, userID int64, readAt time.Time) (int64, error)
	VisibleToUser(ctx context.Context, notificationID int64, v Visibility) (bool, error)
	MarkAllRead(ctx context.Context, v Visibility, readAt time.Time) (int64, error)
	Feed(ctx context.Context, params FeedParams, backward bool) ([]FeedItem, error)
	CountUnread(ctx context.Context, userID int64, tenantID int64, allTenants bool) (int64, error)
	UserScope(ctx context.Context, userID int64) (branchID int64, tenantID int64, err error)
	UserRoleIDs(ctx context.Context, userID int64) ([]int64, error)
	UserExistsInTenant(ctx context.Context, id, tenantID int64) (bool, error)
	BranchExistsInTenant(ctx context.Context, id, tenantID int64) (bool, error)
	RoleExistsForTenant(ctx context.Context, id int64, tenantID *int64) (bool, error)
	// Channels and preferences (phase 26.6).
	Recipients(ctx context.Context, userIDs []int64) ([]delivery.Recipient, error)
	DisabledChannels(ctx context.Context, eventType string, userIDs []int64) (map[int64]map[string]bool, error)
	Preferences(ctx context.Context, userID int64) (map[string]bool, error)
	SetPreference(ctx context.Context, userID int64, eventType, channel string, enabled bool) error
}

type repository struct {
	queries *dbgen.Queries
	tx      *database.TxManager
}

// NewRepository creates the sqlc-backed notification repository.
func NewRepository(queries *dbgen.Queries, tx *database.TxManager) Repository {
	return &repository{queries: queries, tx: tx}
}

func (r *repository) CreateWithTargets(ctx context.Context, input CreateInput) (*Notification, []Target, error) {
	var created dbgen.Notification
	var targets []Target
	err := r.tx.WithTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.CreateNotification(ctx, dbgen.CreateNotificationParams{
			TenantID:  input.TenantID,
			EventType: input.EventType,
			Priority:  input.Priority,
			Title:     input.Title,
			Body:      input.Body,
			Data:      input.Data,
			GroupKey:  input.GroupKey,
			CreatedBy: input.CreatedBy,
			ExpiresAt: timestamptz(input.ExpiresAt),
			Channels:  input.Channels,
		})
		if err != nil {
			return err
		}
		created = row
		targets, err = createTargets(ctx, q, row.ID, input.TenantID, input.Targets)
		return err
	})
	if err != nil {
		return nil, nil, database.MapError("notifications.create", err)
	}
	notification := mapNotification(created)
	return &notification, targets, nil
}

func (r *repository) Get(ctx context.Context, id int64) (*Notification, error) {
	row, err := r.queries.GetNotification(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Notification with ID '%d' was not found.", id)
		}
		return nil, database.MapError("notifications.get", err)
	}
	notification := mapNotification(row)
	return &notification, nil
}

func (r *repository) GetInTenant(ctx context.Context, id int64, allTenants bool, tenantID int64) (*Notification, error) {
	row, err := r.queries.GetNotificationInTenant(ctx, dbgen.GetNotificationInTenantParams{
		ID:         id,
		AllTenants: allTenants,
		TenantID:   &tenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Notification with ID '%d' was not found.", id)
		}
		return nil, database.MapError("notifications.get_in_tenant", err)
	}
	notification := mapNotification(row)
	return &notification, nil
}

func (r *repository) UpdateWithTargets(ctx context.Context, id int64, allTenants bool, tenantID int64, input UpdateInput) (*Notification, []Target, error) {
	var updated dbgen.Notification
	var targets []Target
	err := r.tx.WithTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.UpdateNotificationInTenant(ctx, dbgen.UpdateNotificationInTenantParams{
			ID:         id,
			EventType:  input.EventType,
			Priority:   input.Priority,
			Title:      input.Title,
			Body:       input.Body,
			Data:       input.Data,
			GroupKey:   input.GroupKey,
			ExpiresAt:  timestamptz(input.ExpiresAt),
			UpdatedBy:  input.UpdatedBy,
			AllTenants: allTenants,
			TenantID:   &tenantID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperr.Newf(apperr.ResourceNotFound, "Notification with ID '%d' was not found.", id)
			}
			return err
		}
		updated = row
		if _, err := q.DeleteNotificationTargets(ctx, id); err != nil {
			return err
		}
		targets, err = createTargets(ctx, q, id, row.TenantID, input.Targets)
		return err
	})
	if err != nil {
		var appErr *apperr.Error
		if errors.As(err, &appErr) {
			return nil, nil, appErr
		}
		return nil, nil, database.MapError("notifications.update", err)
	}
	notification := mapNotification(updated)
	return &notification, targets, nil
}

func (r *repository) Delete(ctx context.Context, id int64, allTenants bool, tenantID int64) error {
	affected, err := r.queries.DeleteNotificationInTenant(ctx, dbgen.DeleteNotificationInTenantParams{
		ID:         id,
		AllTenants: allTenants,
		TenantID:   &tenantID,
	})
	if err != nil {
		return database.MapError("notifications.delete", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.ResourceNotFound, "Notification with ID '%d' was not found.", id)
	}
	return nil
}

func (r *repository) List(ctx context.Context, filter ListFilter) ([]Notification, error) {
	rows, err := r.queries.ListNotifications(ctx, dbgen.ListNotificationsParams{
		AllTenants: filter.AllTenants,
		TenantID:   &filter.TenantID,
		Search:     optionalString(filter.Search),
		Priority:   optionalPriority(filter.Priority),
		OrderBy:    filter.OrderBy,
		OrderDir:   filter.OrderDir,
		PageLimit:  filter.Limit,
		PageOffset: filter.Offset,
	})
	if err != nil {
		return nil, database.MapError("notifications.list", err)
	}
	items := make([]Notification, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapNotification(row))
	}
	return items, nil
}

func (r *repository) Count(ctx context.Context, filter ListFilter) (int64, error) {
	total, err := r.queries.CountNotifications(ctx, dbgen.CountNotificationsParams{
		AllTenants: filter.AllTenants,
		TenantID:   &filter.TenantID,
		Search:     optionalString(filter.Search),
		Priority:   optionalPriority(filter.Priority),
	})
	if err != nil {
		return 0, database.MapError("notifications.count", err)
	}
	return total, nil
}

func (r *repository) ListTargets(ctx context.Context, notificationID int64) ([]Target, error) {
	rows, err := r.queries.ListNotificationTargets(ctx, notificationID)
	if err != nil {
		return nil, database.MapError("notifications.list_targets", err)
	}
	targets := make([]Target, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, mapTarget(row))
	}
	return targets, nil
}

// ResolveUserIDs expands target rules to the distinct user IDs they address.
// Role- and branch-targeted expansion is scoped to the notification's tenant:
// a tenant-bound notification can never fan out beyond its own tenant, even
// when a role id collides across tenants (all_tenants is only true for
// global notifications, whose targets carry no USER/ROLE/BRANCH rules).
func (r *repository) ResolveUserIDs(ctx context.Context, tenantID *int64, targets []Target) ([]int64, error) {
	allTenants := tenantID == nil
	scopeTenant := int64(0)
	if tenantID != nil {
		scopeTenant = *tenantID
	}
	seen := map[int64]struct{}{}
	for _, target := range targets {
		var ids []int64
		var err error
		switch target.TargetType {
		case TargetUser:
			if target.TargetID == nil {
				continue
			}
			ids, err = r.queries.ListUserIDsByIDs(ctx, []int64{*target.TargetID})
		case TargetRole:
			if target.TargetID == nil {
				continue
			}
			ids, err = r.queries.ListUserIDsByRoleIDs(ctx, dbgen.ListUserIDsByRoleIDsParams{
				RoleIds:    []int64{*target.TargetID},
				AllTenants: allTenants,
				TenantID:   &scopeTenant,
			})
		case TargetBranch:
			if target.TargetID == nil {
				continue
			}
			ids, err = r.queries.ListUserIDsByBranch(ctx, dbgen.ListUserIDsByBranchParams{
				BranchID:   target.TargetID,
				AllTenants: allTenants,
				TenantID:   &scopeTenant,
			})
		case TargetTenant:
			if tenantID == nil {
				ids, err = r.queries.ListGlobalUserIDs(ctx)
			} else {
				ids, err = r.queries.ListUserIDsByTenant(ctx, tenantID)
			}
		case TargetGlobal:
			ids, err = r.queries.ListGlobalUserIDs(ctx)
		}
		if err != nil {
			return nil, database.MapError("notifications.resolve_target_users", err)
		}
		for _, id := range ids {
			seen[id] = struct{}{}
		}
	}
	userIDs := make([]int64, 0, len(seen))
	for id := range seen {
		userIDs = append(userIDs, id)
	}
	sort.Slice(userIDs, func(i, j int) bool { return userIDs[i] < userIDs[j] })
	return userIDs, nil
}

func (r *repository) InsertDeliveries(ctx context.Context, tenantID *int64, notificationID int64, userIDs []int64) ([]int64, error) {
	inserted, err := r.queries.InsertNotificationDeliveries(ctx, dbgen.InsertNotificationDeliveriesParams{
		TenantID:       tenantID,
		NotificationID: notificationID,
		UserIds:        userIDs,
	})
	if err != nil {
		return nil, database.MapError("notifications.insert_deliveries", err)
	}
	return inserted, nil
}

func (r *repository) ListUnreadDeliveryUserIDs(ctx context.Context, notificationID int64) ([]int64, error) {
	ids, err := r.queries.ListUnreadDeliveryUserIDs(ctx, notificationID)
	if err != nil {
		return nil, database.MapError("notifications.unread_delivery_users", err)
	}
	return ids, nil
}

func (r *repository) InsertRead(ctx context.Context, tenantID, branchID *int64, notificationID, userID int64, readAt time.Time) (int64, error) {
	inserted, err := r.queries.InsertNotificationRead(ctx, dbgen.InsertNotificationReadParams{
		TenantID:       tenantID,
		BranchID:       branchID,
		NotificationID: notificationID,
		UserID:         userID,
		ReadAt:         pgtype.Timestamptz{Time: readAt, Valid: true},
	})
	if err != nil {
		return 0, database.MapError("notifications.insert_read", err)
	}
	return inserted, nil
}

func (r *repository) VisibleToUser(ctx context.Context, notificationID int64, v Visibility) (bool, error) {
	visible, err := r.queries.NotificationVisibleToUser(ctx, dbgen.NotificationVisibleToUserParams{
		NotificationID: notificationID,
		AllTenants:     v.AllTenants,
		TenantID:       optionalInt64(v.TenantID),
		UserID:         v.UserID,
		RoleIds:        emptyInt64Slice(v.RoleIDs),
		BranchID:       v.BranchID,
		IsNonTenant:    v.IsNonTenant,
	})
	if err != nil {
		return false, database.MapError("notifications.visible", err)
	}
	return visible, nil
}

func (r *repository) MarkAllRead(ctx context.Context, v Visibility, readAt time.Time) (int64, error) {
	inserted, err := r.queries.MarkAllNotificationsRead(ctx, dbgen.MarkAllNotificationsReadParams{
		TenantID:    optionalInt64(v.TenantID),
		BranchID:    optionalInt64(v.BranchID),
		UserID:      v.UserID,
		ReadAt:      pgtype.Timestamptz{Time: readAt, Valid: true},
		AllTenants:  v.AllTenants,
		RoleIds:     emptyInt64Slice(v.RoleIDs),
		IsNonTenant: v.IsNonTenant,
	})
	if err != nil {
		return 0, database.MapError("notifications.mark_all_read", err)
	}
	return inserted, nil
}

func (r *repository) Feed(ctx context.Context, params FeedParams, backward bool) ([]FeedItem, error) {
	cursor := pgtype.Timestamptz{}
	var cursorID *int64
	if params.CursorCreatedAt != nil {
		cursor = pgtype.Timestamptz{Time: *params.CursorCreatedAt, Valid: true}
		cursorID = params.CursorID
	}

	if backward {
		backwardCursorID := int64(0)
		if cursorID != nil {
			backwardCursorID = *cursorID
		}
		rows, err := r.queries.ListNotificationFeedBackward(ctx, dbgen.ListNotificationFeedBackwardParams{
			UserID:          params.UserID,
			AllTenants:      params.AllTenants,
			TenantID:        optionalInt64(params.TenantID),
			UnreadOnly:      params.UnreadOnly,
			Priority:        optionalPriority(params.Priority),
			ReadAllBefore:   params.ReadAllBefore,
			RoleIds:         emptyInt64Slice(params.RoleIDs),
			BranchID:        params.BranchID,
			IsNonTenant:     params.IsNonTenant,
			CursorCreatedAt: cursor,
			CursorID:        backwardCursorID,
			PageLimit:       params.Limit,
		})
		if err != nil {
			return nil, database.MapError("notifications.feed_backward", err)
		}
		items := make([]FeedItem, 0, len(rows))
		for _, row := range rows {
			items = append(items, mapFeedBackwardRow(row))
		}
		return items, nil
	}

	rows, err := r.queries.ListNotificationFeed(ctx, dbgen.ListNotificationFeedParams{
		UserID:          params.UserID,
		AllTenants:      params.AllTenants,
		TenantID:        optionalInt64(params.TenantID),
		UnreadOnly:      params.UnreadOnly,
		Priority:        optionalPriority(params.Priority),
		ReadAllBefore:   params.ReadAllBefore,
		RoleIds:         emptyInt64Slice(params.RoleIDs),
		BranchID:        params.BranchID,
		IsNonTenant:     params.IsNonTenant,
		CursorCreatedAt: cursor,
		CursorID:        cursorID,
		PageLimit:       params.Limit,
	})
	if err != nil {
		return nil, database.MapError("notifications.feed", err)
	}
	items := make([]FeedItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, FeedItem{
			ID:        row.ID,
			EventType: row.EventType,
			Priority:  row.Priority,
			Title:     row.Title,
			Body:      row.Body,
			Data:      row.Data,
			GroupKey:  row.GroupKey,
			CreatedAt: row.CreatedAt.Time,
			IsRead:    row.IsRead,
		})
	}
	return items, nil
}

func (r *repository) CountUnread(ctx context.Context, userID int64, tenantID int64, allTenants bool) (int64, error) {
	total, err := r.queries.CountUnreadNotificationsForUser(ctx, dbgen.CountUnreadNotificationsForUserParams{
		UserID:     userID,
		AllTenants: allTenants,
		TenantID:   optionalInt64(tenantID),
	})
	if err != nil {
		return 0, database.MapError("notifications.count_unread", err)
	}
	return total, nil
}

func (r *repository) UserScope(ctx context.Context, userID int64) (int64, int64, error) {
	row, err := r.queries.GetUserNotificationScope(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, 0, apperr.Newf(apperr.ResourceNotFound, "User with ID '%d' was not found.", userID)
		}
		return 0, 0, database.MapError("notifications.user_scope", err)
	}
	return row.BranchID, row.TenantID, nil
}

func (r *repository) UserRoleIDs(ctx context.Context, userID int64) ([]int64, error) {
	ids, err := r.queries.ListUserRoleIDs(ctx, userID)
	if err != nil {
		return nil, database.MapError("notifications.user_roles", err)
	}
	return ids, nil
}

func (r *repository) UserExistsInTenant(ctx context.Context, id, tenantID int64) (bool, error) {
	exists, err := r.queries.UserExistsInTenant(ctx, dbgen.UserExistsInTenantParams{ID: id, TenantID: tenantID})
	if err != nil {
		return false, database.MapError("notifications.user_exists", err)
	}
	return exists, nil
}

func (r *repository) BranchExistsInTenant(ctx context.Context, id, tenantID int64) (bool, error) {
	exists, err := r.queries.BranchExistsInTenant(ctx, dbgen.BranchExistsInTenantParams{ID: id, TenantID: tenantID})
	if err != nil {
		return false, database.MapError("notifications.branch_exists", err)
	}
	return exists, nil
}

func (r *repository) RoleExistsForTenant(ctx context.Context, id int64, tenantID *int64) (bool, error) {
	exists, err := r.queries.RoleExistsForTenant(ctx, dbgen.RoleExistsForTenantParams{ID: id, TenantID: tenantID})
	if err != nil {
		return false, database.MapError("notifications.role_exists", err)
	}
	return exists, nil
}

func createTargets(ctx context.Context, q *dbgen.Queries, notificationID int64, tenantID *int64, inputs []TargetInput) ([]Target, error) {
	targets := make([]Target, 0, len(inputs))
	for _, input := range inputs {
		row, err := q.CreateNotificationTarget(ctx, dbgen.CreateNotificationTargetParams{
			TenantID:       tenantID,
			NotificationID: notificationID,
			TargetType:     input.Type,
			TargetID:       input.ID,
		})
		if err != nil {
			return nil, err
		}
		targets = append(targets, mapTarget(row))
	}
	return targets, nil
}

func mapFeedBackwardRow(row dbgen.ListNotificationFeedBackwardRow) FeedItem {
	return FeedItem{
		ID:        row.ID,
		EventType: row.EventType,
		Priority:  row.Priority,
		Title:     row.Title,
		Body:      row.Body,
		Data:      row.Data,
		GroupKey:  row.GroupKey,
		CreatedAt: row.CreatedAt.Time,
		IsRead:    row.IsRead,
	}
}

func timestamptz(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *value, Valid: true}
}

func optionalString(value string) string {
	return value
}

func optionalPriority(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func optionalInt64(value int64) *int64 {
	if value == 0 {
		return nil
	}
	return &value
}

func emptyInt64Slice(values []int64) []int64 {
	if values == nil {
		return []int64{}
	}
	return values
}

// Recipients returns delivery recipients for active users, with their
// verified addresses only.
func (r *repository) Recipients(ctx context.Context, userIDs []int64) ([]delivery.Recipient, error) {
	rows, err := r.queries.ListNotificationRecipients(ctx, userIDs)
	if err != nil {
		return nil, database.MapError("notifications.recipients", err)
	}
	recipients := make([]delivery.Recipient, 0, len(rows))
	for _, row := range rows {
		recipient := delivery.Recipient{UserID: row.ID, Name: row.Name, Email: row.Email, Phone: row.Phone}
		if row.TenantID != nil {
			recipient.TenantID = *row.TenantID
		}
		recipients = append(recipients, recipient)
	}
	return recipients, nil
}

// DisabledChannels maps user → channel for the users who turned a channel
// off for an event type.
func (r *repository) DisabledChannels(ctx context.Context, eventType string, userIDs []int64) (map[int64]map[string]bool, error) {
	rows, err := r.queries.ListDisabledNotificationChannels(ctx, dbgen.ListDisabledNotificationChannelsParams{EventType: eventType, Ids: userIDs})
	if err != nil {
		return nil, database.MapError("notifications.disabled_channels", err)
	}
	disabled := map[int64]map[string]bool{}
	for _, row := range rows {
		if disabled[row.UserID] == nil {
			disabled[row.UserID] = map[string]bool{}
		}
		disabled[row.UserID][row.Channel] = true
	}
	return disabled, nil
}

// Preferences maps "event|channel" to the user's stored choice.
func (r *repository) Preferences(ctx context.Context, userID int64) (map[string]bool, error) {
	rows, err := r.queries.ListNotificationPreferences(ctx, userID)
	if err != nil {
		return nil, database.MapError("notifications.preferences", err)
	}
	stored := make(map[string]bool, len(rows))
	for _, row := range rows {
		stored[row.EventType+"|"+row.Channel] = row.Enabled
	}
	return stored, nil
}

// SetPreference stores one choice.
func (r *repository) SetPreference(ctx context.Context, userID int64, eventType, channel string, enabled bool) error {
	err := r.queries.UpsertNotificationPreference(ctx, dbgen.UpsertNotificationPreferenceParams{
		UserID: userID, EventType: eventType, Channel: channel, Enabled: enabled,
	})
	if err != nil {
		return database.MapError("notifications.set_preference", err)
	}
	return nil
}
