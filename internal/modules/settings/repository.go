package settings

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
)

// Setting is one stored row.
type Setting struct {
	ID         int64
	Key        string
	TargetType *string
	TargetID   *int64
	GroupID    *int64
	SortOrder  *int32 // hand-picked position in the group; nil = catalog order
	Value      []byte
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// StoredGroup is one settings_groups row.
type StoredGroup struct {
	ID          int64
	Slug        string
	Name        string
	Description *string
	SortOrder   int
	IsSystem    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// GroupInput carries settings-group create/update input. SortOrder is nil when
// the caller omitted it (create → 0, update → keep the existing value).
type GroupInput struct {
	ID          int64
	Slug        string
	Name        string
	Description *string
	SortOrder   *int
	ActorID     *int64
}

// GroupListFilter carries settings-group list filters.
type GroupListFilter struct {
	Search   string
	OrderBy  string
	OrderDir string
	Limit    int32
	Offset   int32
}

// Repository is the persistence contract for settings and groups.
type Repository interface {
	Get(ctx context.Context, key string, target Target) (*Setting, error)
	ListGlobal(ctx context.Context) ([]Setting, error)
	ListByTarget(ctx context.Context, target Target) ([]Setting, error)
	UpsertGlobal(ctx context.Context, key string, value []byte) (*Setting, error)
	UpsertTarget(ctx context.Context, key string, target Target, value []byte) (*Setting, error)
	InsertGlobalIfAbsent(ctx context.Context, key string, value []byte, groupID *int64) (*Setting, bool, error)
	SetGroupIfNull(ctx context.Context, key string, groupID int64) error
	Delete(ctx context.Context, key string, target Target) (bool, error)
	UpsertSystemGroup(ctx context.Context, group Group) (*StoredGroup, error)
	ListGroups(ctx context.Context) ([]StoredGroup, error)
	GetGroup(ctx context.Context, id int64) (*StoredGroup, error)
	GetGroupBySlug(ctx context.Context, slug string) (*StoredGroup, error)
	CreateGroup(ctx context.Context, input GroupInput) (*StoredGroup, error)
	UpdateGroup(ctx context.Context, input GroupInput) (*StoredGroup, error)
	DeleteGroup(ctx context.Context, id int64) (bool, error)
	ListGroupsPaginated(ctx context.Context, filter GroupListFilter) ([]StoredGroup, error)
	CountGroups(ctx context.Context, filter GroupListFilter) (int64, error)
	ListGroupsLight(ctx context.Context, orderBy, orderDir string) ([]StoredGroup, error)
	SetSettingGroup(ctx context.Context, key string, groupID *int64) (*Setting, error)
	PlaceSetting(ctx context.Context, key string, groupID *int64, sortOrder int32) (bool, error)
	SetGroupSortOrder(ctx context.Context, id int64, sortOrder int32) (bool, error)
	TenantExists(ctx context.Context, tenantID int64) (bool, error)
}

type repository struct {
	queries *dbgen.Queries
}

// NewRepository creates the sqlc-backed settings repository.
func NewRepository(queries *dbgen.Queries) Repository {
	return &repository{queries: queries}
}

func (r *repository) Get(ctx context.Context, key string, target Target) (*Setting, error) {
	row, err := r.queries.GetSetting(ctx, dbgen.GetSettingParams{
		Key:        key,
		TargetType: target.TypePtr(),
		TargetID:   target.IDPtr(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Setting %q was not found.", key)
		}
		return nil, database.MapError("settings.get", err)
	}
	setting := mapSetting(row)
	return &setting, nil
}

func (r *repository) ListGlobal(ctx context.Context) ([]Setting, error) {
	rows, err := r.queries.ListGlobalSettings(ctx)
	if err != nil {
		return nil, database.MapError("settings.list_global", err)
	}
	return mapSettings(rows), nil
}

func (r *repository) ListByTarget(ctx context.Context, target Target) ([]Setting, error) {
	rows, err := r.queries.ListSettingsByTarget(ctx, dbgen.ListSettingsByTargetParams{
		TargetType: target.TypePtr(),
		TargetID:   target.IDPtr(),
	})
	if err != nil {
		return nil, database.MapError("settings.list_target", err)
	}
	return mapSettings(rows), nil
}

func (r *repository) UpsertGlobal(ctx context.Context, key string, value []byte) (*Setting, error) {
	row, err := r.queries.UpsertGlobalSetting(ctx, dbgen.UpsertGlobalSettingParams{Key: key, Value: value})
	if err != nil {
		return nil, database.MapError("settings.upsert_global", err)
	}
	setting := mapSetting(row)
	return &setting, nil
}

func (r *repository) UpsertTarget(ctx context.Context, key string, target Target, value []byte) (*Setting, error) {
	row, err := r.queries.UpsertTargetSetting(ctx, dbgen.UpsertTargetSettingParams{
		Key:        key,
		TargetType: target.TypePtr(),
		TargetID:   target.IDPtr(),
		Value:      value,
	})
	if err != nil {
		return nil, database.MapError("settings.upsert_target", err)
	}
	setting := mapSetting(row)
	return &setting, nil
}

func (r *repository) InsertGlobalIfAbsent(ctx context.Context, key string, value []byte, groupID *int64) (*Setting, bool, error) {
	row, err := r.queries.InsertGlobalSettingIfAbsent(ctx, dbgen.InsertGlobalSettingIfAbsentParams{
		Key:     key,
		GroupID: groupID,
		Value:   value,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, database.MapError("settings.insert_global", err)
	}
	setting := mapSetting(row)
	return &setting, true, nil
}

func (r *repository) SetGroupIfNull(ctx context.Context, key string, groupID int64) error {
	_, err := r.queries.SetSettingGroupIfNull(ctx, dbgen.SetSettingGroupIfNullParams{
		Key:     key,
		GroupID: &groupID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return database.MapError("settings.set_group", err)
	}
	return nil
}

func (r *repository) Delete(ctx context.Context, key string, target Target) (bool, error) {
	affected, err := r.queries.DeleteSetting(ctx, dbgen.DeleteSettingParams{
		Key:        key,
		TargetType: target.TypePtr(),
		TargetID:   target.IDPtr(),
	})
	if err != nil {
		return false, database.MapError("settings.delete", err)
	}
	return affected > 0, nil
}

func (r *repository) UpsertSystemGroup(ctx context.Context, group Group) (*StoredGroup, error) {
	description := group.Description
	row, err := r.queries.UpsertSystemGroup(ctx, dbgen.UpsertSystemGroupParams{
		Slug:        group.Slug,
		Name:        group.Name,
		Description: &description,
		SortOrder:   int32(group.SortOrder),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, database.MapError("settings.upsert_group", err)
	}
	stored := mapGroup(row)
	return &stored, nil
}

func (r *repository) ListGroups(ctx context.Context) ([]StoredGroup, error) {
	rows, err := r.queries.ListSettingsGroups(ctx)
	if err != nil {
		return nil, database.MapError("settings.list_groups", err)
	}
	return mapGroups(rows), nil
}

func (r *repository) GetGroup(ctx context.Context, id int64) (*StoredGroup, error) {
	row, err := r.queries.GetSettingsGroup(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Settings group with ID '%d' was not found.", id)
		}
		return nil, database.MapError("settings.get_group", err)
	}
	group := mapGroup(row)
	return &group, nil
}

func (r *repository) GetGroupBySlug(ctx context.Context, slug string) (*StoredGroup, error) {
	row, err := r.queries.GetSettingsGroupBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Settings group %q was not found.", slug)
		}
		return nil, database.MapError("settings.get_group_by_slug", err)
	}
	group := mapGroup(row)
	return &group, nil
}

func (r *repository) CreateGroup(ctx context.Context, input GroupInput) (*StoredGroup, error) {
	row, err := r.queries.CreateSettingsGroup(ctx, dbgen.CreateSettingsGroupParams{
		Slug:        input.Slug,
		Name:        input.Name,
		Description: input.Description,
		SortOrder:   int32(intValue(input.SortOrder)),
		ActorID:     input.ActorID,
	})
	if err != nil {
		return nil, database.MapError("settings.create_group", err)
	}
	group := mapGroup(row)
	return &group, nil
}

func (r *repository) UpdateGroup(ctx context.Context, input GroupInput) (*StoredGroup, error) {
	row, err := r.queries.UpdateSettingsGroup(ctx, dbgen.UpdateSettingsGroupParams{
		Slug:        input.Slug,
		Name:        input.Name,
		Description: input.Description,
		SortOrder:   int32(intValue(input.SortOrder)),
		ActorID:     input.ActorID,
		ID:          input.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Settings group with ID '%d' was not found.", input.ID)
		}
		return nil, database.MapError("settings.update_group", err)
	}
	group := mapGroup(row)
	return &group, nil
}

func (r *repository) DeleteGroup(ctx context.Context, id int64) (bool, error) {
	affected, err := r.queries.DeleteSettingsGroup(ctx, id)
	if err != nil {
		return false, database.MapError("settings.delete_group", err)
	}
	return affected > 0, nil
}

func (r *repository) ListGroupsPaginated(ctx context.Context, filter GroupListFilter) ([]StoredGroup, error) {
	rows, err := r.queries.ListSettingsGroupsPaginated(ctx, dbgen.ListSettingsGroupsPaginatedParams{
		Search:     optionalString(filter.Search),
		OrderBy:    filter.OrderBy,
		OrderDir:   filter.OrderDir,
		PageLimit:  filter.Limit,
		PageOffset: filter.Offset,
	})
	if err != nil {
		return nil, database.MapError("settings.list_groups", err)
	}
	return mapGroups(rows), nil
}

func (r *repository) CountGroups(ctx context.Context, filter GroupListFilter) (int64, error) {
	total, err := r.queries.CountSettingsGroups(ctx, optionalString(filter.Search))
	if err != nil {
		return 0, database.MapError("settings.count_groups", err)
	}
	return total, nil
}

func (r *repository) ListGroupsLight(ctx context.Context, orderBy, orderDir string) ([]StoredGroup, error) {
	rows, err := r.queries.ListSettingsGroupsLight(ctx, dbgen.ListSettingsGroupsLightParams{
		OrderBy:  orderBy,
		OrderDir: orderDir,
	})
	if err != nil {
		return nil, database.MapError("settings.list_groups_light", err)
	}
	return mapGroups(rows), nil
}

func (r *repository) SetSettingGroup(ctx context.Context, key string, groupID *int64) (*Setting, error) {
	row, err := r.queries.SetSettingGroup(ctx, dbgen.SetSettingGroupParams{Key: key, GroupID: groupID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Setting %q has no global row to assign.", key)
		}
		return nil, database.MapError("settings.set_group", err)
	}
	setting := mapSetting(row)
	return &setting, nil
}

func (r *repository) PlaceSetting(ctx context.Context, key string, groupID *int64, sortOrder int32) (bool, error) {
	affected, err := r.queries.PlaceSetting(ctx, dbgen.PlaceSettingParams{Key: key, GroupID: groupID, SortOrder: &sortOrder})
	if err != nil {
		return false, database.MapError("settings.place", err)
	}
	return affected > 0, nil
}

func (r *repository) SetGroupSortOrder(ctx context.Context, id int64, sortOrder int32) (bool, error) {
	affected, err := r.queries.SetSettingsGroupSortOrder(ctx, dbgen.SetSettingsGroupSortOrderParams{ID: id, SortOrder: sortOrder})
	if err != nil {
		return false, database.MapError("settings.group_order", err)
	}
	return affected > 0, nil
}

func (r *repository) TenantExists(ctx context.Context, tenantID int64) (bool, error) {
	_, err := r.queries.GetTenant(ctx, tenantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, database.MapError("settings.tenant_exists", err)
	}
	return true, nil
}

func mapSetting(row dbgen.Setting) Setting {
	return Setting{
		ID:         row.ID,
		Key:        row.Key,
		TargetType: row.TargetType,
		TargetID:   row.TargetID,
		GroupID:    row.GroupID,
		SortOrder:  row.SortOrder,
		Value:      row.Value,
		CreatedAt:  row.CreatedAt.Time,
		UpdatedAt:  row.UpdatedAt.Time,
	}
}

func mapSettings(rows []dbgen.Setting) []Setting {
	settings := make([]Setting, 0, len(rows))
	for _, row := range rows {
		settings = append(settings, mapSetting(row))
	}
	return settings
}

func mapGroup(row dbgen.SettingsGroup) StoredGroup {
	return StoredGroup{
		ID:          row.ID,
		Slug:        row.Slug,
		Name:        row.Name,
		Description: row.Description,
		SortOrder:   int(row.SortOrder),
		IsSystem:    row.IsSystem,
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}
}

func mapGroups(rows []dbgen.SettingsGroup) []StoredGroup {
	groups := make([]StoredGroup, 0, len(rows))
	for _, row := range rows {
		groups = append(groups, mapGroup(row))
	}
	return groups
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
