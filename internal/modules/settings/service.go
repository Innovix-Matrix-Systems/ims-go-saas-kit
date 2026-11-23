package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

const (
	settingsTTL   = time.Hour
	globalBlobKey = "settings:blob:g"
	// groupsCacheKey also carries per-key global row ids; v2 avoids decoding
	// pre-id snapshots from a shared cache.
	groupsCacheKey = "settings:groups:v2"
)

func tenantBlobKey(tenantID int64) string {
	return fmt.Sprintf("settings:blob:t:%d", tenantID)
}

// blob is one target's key -> decoded value cache entry.
type blob map[string]any

// GroupInfo is the group metadata attached to an effective setting. ID is 0 for
// catalog groups that have not been materialized by `settings:sync` yet.
type GroupInfo struct {
	ID          int64  `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SortOrder   int    `json:"sort_order"`
	IsSystem    bool   `json:"is_system"`
}

// EffectiveSetting is one catalog key resolved for a target. ID is the global
// setting row id (0 when the key has not been materialized by `settings:sync`).
type EffectiveSetting struct {
	ID     int64
	Key    Key
	Value  any
	Source string // tenant | global | default
	Group  GroupInfo
	// SortOrder is the position inside the group: the hand-picked order when
	// set (dev dashboard), otherwise the catalog order.
	SortOrder int
}

type groupsSnapshot struct {
	Groups   []GroupInfo       `json:"groups"`
	KeyGroup map[string]string `json:"key_group"`
	KeyID    map[string]int64  `json:"key_id"`
	KeyOrder map[string]int    `json:"key_order"`
}

func (s *groupsSnapshot) orderFor(key Key) int {
	if order, ok := s.KeyOrder[key.Name]; ok {
		return order
	}
	return key.SortOrder
}

func (s *groupsSnapshot) groupFor(key Key) GroupInfo {
	slug := s.KeyGroup[key.Name]
	if slug == "" {
		slug = key.Group
	}
	for _, group := range s.Groups {
		if group.Slug == slug {
			return group
		}
	}
	return GroupInfo{Slug: slug, Name: slug, SortOrder: 0}
}

func (s *groupsSnapshot) idFor(key Key) int64 {
	return s.KeyID[key.Name]
}

// Service resolves and persists typed settings.
type Service struct {
	repo     Repository
	cache    cache.Store
	activity *activity.Recorder
}

// WithActivity records tenant setting changes in the activity log.
func (s *Service) WithActivity(recorder *activity.Recorder) *Service {
	s.activity = recorder
	return s
}

// NewService creates the settings service.
func NewService(repo Repository, store cache.Store) *Service {
	return &Service{repo: repo, cache: store}
}

// Get resolves a setting: tenant override -> global row -> code default.
func (s *Service) Get(ctx context.Context, scope tenancy.Scope, name string) (any, error) {
	key, ok := Lookup(name)
	if !ok {
		return nil, apperr.Newf(apperr.ResourceNotFound, "Setting %q is not registered.", name)
	}
	value, _, err := s.resolve(ctx, s.targetForScope(scope), key)
	if err != nil {
		return nil, err
	}
	coerced, err := key.Coerce(value)
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "coerce setting value", err)
	}
	return coerced, nil
}

// GetInt resolves an integer setting.
func (s *Service) GetInt(ctx context.Context, scope tenancy.Scope, name string) (int, error) {
	value, err := s.Get(ctx, scope, name)
	if err != nil {
		return 0, err
	}
	if number, ok := value.(int); ok {
		return number, nil
	}
	return 0, apperr.Newf(apperr.InternalServerError, "Setting %q is not an integer.", name)
}

// GetBool resolves a boolean setting.
func (s *Service) GetBool(ctx context.Context, scope tenancy.Scope, name string) (bool, error) {
	value, err := s.Get(ctx, scope, name)
	if err != nil {
		return false, err
	}
	if flag, ok := value.(bool); ok {
		return flag, nil
	}
	return false, apperr.Newf(apperr.InternalServerError, "Setting %q is not a boolean.", name)
}

// Set persists a setting at the caller's scope (tenant row when scoped, global
// otherwise).
func (s *Service) Set(ctx context.Context, scope tenancy.Scope, name string, value any) error {
	return s.writeTarget(ctx, name, s.targetForScope(scope), value)
}

// SetTenant upserts the caller's tenant override and returns the effective
// setting.
func (s *Service) SetTenant(ctx context.Context, scope tenancy.Scope, name string, value any) (*EffectiveSetting, error) {
	tenantID := scope.EffectiveTenantID()
	if tenantID == 0 {
		return nil, apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	target := Target{Type: TargetTenant, ID: tenantID}
	if err := s.writeTarget(ctx, name, target, value); err != nil {
		return nil, err
	}
	s.activity.Record(ctx, activity.Entry{
		TenantID: tenantID, Action: "setting.updated", Subject: "setting",
		Changes: map[string]any{name: value},
	})
	return s.EffectiveForTargetKey(ctx, target, name)
}

// SetTarget upserts a setting for an explicit target and returns the effective
// setting.
func (s *Service) SetTarget(ctx context.Context, name string, target Target, value any) (*EffectiveSetting, error) {
	if err := s.writeTarget(ctx, name, target, value); err != nil {
		return nil, err
	}
	return s.EffectiveForTargetKey(ctx, target, name)
}

// DeleteTarget removes an override so the value falls back (tenant -> global ->
// default); omitting a target deletes the global row.
func (s *Service) DeleteTarget(ctx context.Context, name string, target Target) error {
	key, ok := Lookup(name)
	if !ok {
		return apperr.Newf(apperr.ResourceNotFound, "Setting %q is not registered.", name)
	}
	if err := s.validateTarget(ctx, key, target); err != nil {
		return err
	}
	deleted, err := s.repo.Delete(ctx, name, target)
	if err != nil {
		return err
	}
	if !deleted {
		return apperr.Newf(apperr.ResourceNotFound, "No stored value exists for setting %q at the requested target.", name)
	}
	s.invalidate(ctx, target)
	return nil
}

// EffectiveSettings resolves every catalog key for the caller's scope.
func (s *Service) EffectiveSettings(ctx context.Context, scope tenancy.Scope) ([]EffectiveSetting, error) {
	return s.EffectiveForTarget(ctx, s.targetForScope(scope))
}

// EffectiveForTarget resolves every catalog key for an explicit target.
func (s *Service) EffectiveForTarget(ctx context.Context, target Target) ([]EffectiveSetting, error) {
	snapshot, err := s.groups(ctx)
	if err != nil {
		return nil, err
	}
	globals, err := s.globalBlob(ctx)
	if err != nil {
		return nil, err
	}
	override, err := s.overrideBlob(ctx, target)
	if err != nil {
		return nil, err
	}
	out := make([]EffectiveSetting, 0, len(Catalog))
	for _, key := range Catalog {
		value, source := resolveValue(key, override, globals)
		out = append(out, EffectiveSetting{
			ID:     snapshot.idFor(key),
			Key:    key,
			Value:  value,
			Source: source,
			Group:  snapshot.groupFor(key),

			SortOrder: snapshot.orderFor(key),
		})
	}
	sortEffective(out)
	return out, nil
}

// EffectiveForTargetKey resolves a single catalog key for an explicit target.
func (s *Service) EffectiveForTargetKey(ctx context.Context, target Target, name string) (*EffectiveSetting, error) {
	key, ok := Lookup(name)
	if !ok {
		return nil, apperr.Newf(apperr.ResourceNotFound, "Setting %q is not registered.", name)
	}
	globals, err := s.globalBlob(ctx)
	if err != nil {
		return nil, err
	}
	override, err := s.overrideBlob(ctx, target)
	if err != nil {
		return nil, err
	}
	value, source := resolveValue(key, override, globals)
	snapshot, err := s.groups(ctx)
	if err != nil {
		return nil, err
	}
	return &EffectiveSetting{
		ID:     snapshot.idFor(key),
		Key:    key,
		Value:  value,
		Source: source,
		Group:  snapshot.groupFor(key),

		SortOrder: snapshot.orderFor(key),
	}, nil
}

// Sync upserts catalog groups and inserts missing global defaults, returning
// how many global settings were created.
func (s *Service) Sync(ctx context.Context) (int, error) {
	for _, group := range Groups {
		if _, err := s.repo.UpsertSystemGroup(ctx, group); err != nil {
			return 0, err
		}
	}
	groupIDs, err := s.groupIDsBySlug(ctx)
	if err != nil {
		return 0, err
	}

	created := 0
	for _, key := range Catalog {
		encoded, err := json.Marshal(key.Default)
		if err != nil {
			return created, apperr.Wrap(apperr.InternalServerError, "encode default setting", err)
		}
		var groupID *int64
		if id, ok := groupIDs[key.Group]; ok {
			groupID = &id
		}
		_, inserted, err := s.repo.InsertGlobalIfAbsent(ctx, key.Name, encoded, groupID)
		if err != nil {
			return created, err
		}
		if inserted {
			created++
		}
		if groupID != nil {
			if err := s.repo.SetGroupIfNull(ctx, key.Name, *groupID); err != nil {
				return created, err
			}
		}
	}
	_ = s.cache.Delete(ctx, globalBlobKey)
	_ = s.cache.Delete(ctx, groupsCacheKey)
	return created, nil
}

// ListGroups returns a page of settings groups plus the total count.
func (s *Service) ListGroups(ctx context.Context, filter GroupListFilter) ([]StoredGroup, int64, error) {
	items, err := s.repo.ListGroupsPaginated(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.CountGroups(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// ListGroupsLight returns every settings group without pagination.
func (s *Service) ListGroupsLight(ctx context.Context, orderBy, orderDir string) ([]StoredGroup, error) {
	return s.repo.ListGroupsLight(ctx, orderBy, orderDir)
}

// GetGroup returns one settings group.
func (s *Service) GetGroup(ctx context.Context, id int64) (*StoredGroup, error) {
	return s.repo.GetGroup(ctx, id)
}

// CreateGroup creates a settings group, deriving its slug from the name.
func (s *Service) CreateGroup(ctx context.Context, actorID int64, input GroupInput) (*StoredGroup, error) {
	slug := slugify(input.Name)
	if slug == "" {
		return nil, apperr.Validation(map[string][]string{"name": {"The name must contain at least one alphanumeric character."}})
	}
	if _, err := s.repo.GetGroupBySlug(ctx, slug); err == nil {
		return nil, apperr.Validation(map[string][]string{"name": {"A settings group with this name already exists."}})
	} else if !apperr.Is(err, apperr.ResourceNotFound) {
		return nil, err
	}

	group, err := s.repo.CreateGroup(ctx, GroupInput{
		Slug:        slug,
		Name:        input.Name,
		Description: input.Description,
		SortOrder:   input.SortOrder,
		ActorID:     actorPtr(actorID),
	})
	if err != nil {
		return nil, err
	}
	s.invalidateGroups(ctx)
	return group, nil
}

// UpdateGroup updates a settings group, re-deriving its slug from the name.
func (s *Service) UpdateGroup(ctx context.Context, actorID, id int64, input GroupInput) (*StoredGroup, error) {
	existing, err := s.repo.GetGroup(ctx, id)
	if err != nil {
		return nil, err
	}
	slug := slugify(input.Name)
	if slug == "" {
		return nil, apperr.Validation(map[string][]string{"name": {"The name must contain at least one alphanumeric character."}})
	}
	// System groups keep their catalog slug even when renamed in the panel;
	// `settings:sync` matches them by slug and restores catalog metadata.
	if existing.IsSystem {
		slug = existing.Slug
	}
	other, err := s.repo.GetGroupBySlug(ctx, slug)
	if err == nil && other.ID != id {
		return nil, apperr.Validation(map[string][]string{"name": {"A settings group with this name already exists."}})
	}
	if err != nil && !apperr.Is(err, apperr.ResourceNotFound) {
		return nil, err
	}

	sortOrder := existing.SortOrder
	if input.SortOrder != nil {
		sortOrder = *input.SortOrder
	}
	group, err := s.repo.UpdateGroup(ctx, GroupInput{
		ID:          id,
		Slug:        slug,
		Name:        input.Name,
		Description: input.Description,
		SortOrder:   &sortOrder,
		ActorID:     actorPtr(actorID),
	})
	if err != nil {
		return nil, err
	}
	s.invalidateGroups(ctx)
	return group, nil
}

// DeleteGroup removes a settings group; assigned settings fall back to their
// catalog group (the FK sets settings.group_id to NULL).
func (s *Service) DeleteGroup(ctx context.Context, id int64) error {
	if _, err := s.repo.GetGroup(ctx, id); err != nil {
		return err
	}
	deleted, err := s.repo.DeleteGroup(ctx, id)
	if err != nil {
		return err
	}
	if !deleted {
		return apperr.Newf(apperr.ResourceNotFound, "Settings group with ID '%d' was not found.", id)
	}
	s.invalidateGroups(ctx)
	return nil
}

// AssignKeyGroup assigns a catalog key's global row to a group (nil clears the
// assignment, falling back to the catalog group).
func (s *Service) AssignKeyGroup(ctx context.Context, name string, groupID *int64) (*EffectiveSetting, error) {
	if _, ok := Lookup(name); !ok {
		return nil, apperr.Newf(apperr.ResourceNotFound, "Setting %q is not registered.", name)
	}
	if groupID != nil {
		if _, err := s.repo.GetGroup(ctx, *groupID); err != nil {
			return nil, err
		}
	}
	if _, err := s.repo.SetSettingGroup(ctx, name, groupID); err != nil {
		return nil, err
	}
	s.invalidateGroups(ctx)
	return s.EffectiveForTargetKey(ctx, Target{Type: TargetGlobal}, name)
}

// PlaceKeys puts keys into a group in the given order (dev dashboard drag
// and drop). Every key needs its global row (run settings:sync first).
func (s *Service) PlaceKeys(ctx context.Context, groupID int64, keys []string) error {
	if _, err := s.repo.GetGroup(ctx, groupID); err != nil {
		return err
	}
	for _, name := range keys {
		if _, ok := Lookup(name); !ok {
			return apperr.Newf(apperr.ResourceNotFound, "Setting %q is not registered.", name)
		}
	}
	for index, name := range keys {
		placed, err := s.repo.PlaceSetting(ctx, name, &groupID, int32(index+1))
		if err != nil {
			return err
		}
		if !placed {
			return apperr.Newf(apperr.ResourceNotFound, "Setting %q has no global row yet; run settings:sync first.", name)
		}
	}
	s.invalidateGroups(ctx)
	return nil
}

// ReorderGroups sets the group order to the given ids.
func (s *Service) ReorderGroups(ctx context.Context, ids []int64) error {
	for index, id := range ids {
		updated, err := s.repo.SetGroupSortOrder(ctx, id, int32(index+1))
		if err != nil {
			return err
		}
		if !updated {
			return apperr.Newf(apperr.ResourceNotFound, "Settings group %d was not found.", id)
		}
	}
	s.invalidateGroups(ctx)
	return nil
}

func (s *Service) invalidateGroups(ctx context.Context) {
	_ = s.cache.Delete(ctx, groupsCacheKey)
}

func actorPtr(actorID int64) *int64 {
	if actorID <= 0 {
		return nil
	}
	return &actorID
}

func (s *Service) writeTarget(ctx context.Context, name string, target Target, value any) error {
	key, ok := Lookup(name)
	if !ok {
		return apperr.Newf(apperr.ResourceNotFound, "Setting %q is not registered.", name)
	}
	if err := s.validateTarget(ctx, key, target); err != nil {
		return err
	}
	coerced, err := key.Coerce(value)
	if err != nil {
		return apperr.Validation(map[string][]string{"value": {err.Error()}})
	}
	encoded, err := json.Marshal(coerced)
	if err != nil {
		return apperr.Wrap(apperr.InternalServerError, "encode setting value", err)
	}
	if target.IsGlobal() {
		if _, err := s.repo.UpsertGlobal(ctx, name, encoded); err != nil {
			return err
		}
	} else {
		if _, err := s.repo.UpsertTarget(ctx, name, target, encoded); err != nil {
			return err
		}
	}
	s.invalidate(ctx, target)
	return nil
}

// validateTarget enforces the settings target rules: global or tenant only, and
// tenant overrides are legal only for keys declaring ScopeTenant.
func (s *Service) validateTarget(ctx context.Context, key Key, target Target) error {
	switch target.Type {
	case TargetGlobal:
		if target.ID != 0 {
			return apperr.Validation(map[string][]string{"target_id": {"The target_id must be omitted for the global target."}})
		}
		return nil
	case TargetTenant:
		if target.ID <= 0 {
			return apperr.Validation(map[string][]string{"target_id": {"The target_id is required for the tenant target."}})
		}
		if !key.AllowsTenant() {
			return apperr.Validation(map[string][]string{"target_type": {"This setting cannot be overridden per tenant."}})
		}
		exists, err := s.repo.TenantExists(ctx, target.ID)
		if err != nil {
			return err
		}
		if !exists {
			return apperr.Validation(map[string][]string{"target_id": {"The selected tenant does not exist."}})
		}
		return nil
	default:
		return apperr.Validation(map[string][]string{"target_type": {"The selected target_type is not supported for settings."}})
	}
}

func (s *Service) invalidate(ctx context.Context, target Target) {
	if target.IsGlobal() {
		_ = s.cache.Delete(ctx, globalBlobKey)
		return
	}
	_ = s.cache.Delete(ctx, tenantBlobKey(target.ID))
}

func (s *Service) resolve(ctx context.Context, target Target, key Key) (any, string, error) {
	globals, err := s.globalBlob(ctx)
	if err != nil {
		return nil, "", err
	}
	override, err := s.overrideBlob(ctx, target)
	if err != nil {
		return nil, "", err
	}
	value, source := resolveValue(key, override, globals)
	return value, source, nil
}

func resolveValue(key Key, override, global blob) (any, string) {
	if override != nil {
		if raw, ok := override[key.Name]; ok {
			if value, err := key.Coerce(raw); err == nil {
				return value, "tenant"
			}
		}
	}
	if global != nil {
		if raw, ok := global[key.Name]; ok {
			if value, err := key.Coerce(raw); err == nil {
				return value, "global"
			}
		}
	}
	return key.Default, "default"
}

func (s *Service) targetForScope(scope tenancy.Scope) Target {
	if tenantID := scope.EffectiveTenantID(); tenantID > 0 {
		return Target{Type: TargetTenant, ID: tenantID}
	}
	return Target{Type: TargetGlobal}
}

func (s *Service) overrideBlob(ctx context.Context, target Target) (blob, error) {
	if target.Type == TargetTenant && target.ID > 0 {
		return s.tenantBlob(ctx, target.ID)
	}
	return nil, nil
}

func (s *Service) globalBlob(ctx context.Context) (blob, error) {
	var cached blob
	// A cache outage degrades to the database instead of failing the request.
	if found, err := s.cache.Get(ctx, globalBlobKey, &cached); err == nil && found {
		return cached, nil
	}
	rows, err := s.repo.ListGlobal(ctx)
	if err != nil {
		return nil, err
	}
	values := make(blob, len(rows))
	for _, row := range rows {
		values[row.Key] = decode(row.Value)
	}
	_ = s.cache.Set(ctx, globalBlobKey, values, settingsTTL)
	return values, nil
}

func (s *Service) tenantBlob(ctx context.Context, tenantID int64) (blob, error) {
	key := tenantBlobKey(tenantID)
	var cached blob
	// A cache outage degrades to the database instead of failing the request.
	if found, err := s.cache.Get(ctx, key, &cached); err == nil && found {
		return cached, nil
	}
	rows, err := s.repo.ListByTarget(ctx, Target{Type: TargetTenant, ID: tenantID})
	if err != nil {
		return nil, err
	}
	values := make(blob, len(rows))
	for _, row := range rows {
		values[row.Key] = decode(row.Value)
	}
	_ = s.cache.Set(ctx, key, values, settingsTTL)
	return values, nil
}

func (s *Service) groups(ctx context.Context) (*groupsSnapshot, error) {
	var cached groupsSnapshot
	// A cache outage degrades to the database instead of failing the request.
	if found, err := s.cache.Get(ctx, groupsCacheKey, &cached); err == nil && found {
		return &cached, nil
	}
	snapshot, err := s.buildGroups(ctx)
	if err != nil {
		return nil, err
	}
	_ = s.cache.Set(ctx, groupsCacheKey, snapshot, settingsTTL)
	return snapshot, nil
}

func (s *Service) buildGroups(ctx context.Context) (*groupsSnapshot, error) {
	rows, err := s.repo.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	bySlug := make(map[string]GroupInfo, len(rows))
	byID := make(map[int64]string, len(rows))
	for _, row := range rows {
		info := GroupInfo{ID: row.ID, Slug: row.Slug, Name: row.Name, SortOrder: row.SortOrder, IsSystem: row.IsSystem}
		if row.Description != nil {
			info.Description = *row.Description
		}
		bySlug[row.Slug] = info
		byID[row.ID] = row.Slug
	}
	for _, group := range Groups {
		if _, ok := bySlug[group.Slug]; !ok {
			bySlug[group.Slug] = GroupInfo{
				Slug:        group.Slug,
				Name:        group.Name,
				Description: group.Description,
				SortOrder:   group.SortOrder,
			}
		}
	}

	keyGroup := map[string]string{}
	keyID := map[string]int64{}
	keyOrder := map[string]int{}
	globals, err := s.repo.ListGlobal(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range globals {
		keyID[row.Key] = row.ID
		if row.SortOrder != nil {
			keyOrder[row.Key] = int(*row.SortOrder)
		}
		if row.GroupID == nil {
			continue
		}
		if slug, ok := byID[*row.GroupID]; ok {
			keyGroup[row.Key] = slug
		}
	}

	groups := make([]GroupInfo, 0, len(bySlug))
	for _, info := range bySlug {
		groups = append(groups, info)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].SortOrder != groups[j].SortOrder {
			return groups[i].SortOrder < groups[j].SortOrder
		}
		return groups[i].Slug < groups[j].Slug
	})
	return &groupsSnapshot{Groups: groups, KeyGroup: keyGroup, KeyID: keyID, KeyOrder: keyOrder}, nil
}

func (s *Service) groupIDsBySlug(ctx context.Context) (map[string]int64, error) {
	rows, err := s.repo.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]int64, len(rows))
	for _, row := range rows {
		ids[row.Slug] = row.ID
	}
	return ids, nil
}

func sortEffective(items []EffectiveSetting) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Group.SortOrder != items[j].Group.SortOrder {
			return items[i].Group.SortOrder < items[j].Group.SortOrder
		}
		if items[i].Group.Slug != items[j].Group.Slug {
			return items[i].Group.Slug < items[j].Group.Slug
		}
		if items[i].SortOrder != items[j].SortOrder {
			return items[i].SortOrder < items[j].SortOrder
		}
		return items[i].Key.Name < items[j].Key.Name
	})
}

func decode(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}
