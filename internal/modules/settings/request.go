package settings

import "time"

// GroupInsertUpdateRequest is the settings-group create/update payload.
type GroupInsertUpdateRequest struct {
	Name        string  `json:"name" validate:"required,min=2,max=255"`
	Description *string `json:"description" validate:"omitempty,max=255"`
	SortOrder   *int    `json:"sort_order" validate:"omitempty,gte=0"`
}

// AssignGroupRequest assigns a catalog key to a group (null clears it).
type AssignGroupRequest struct {
	GroupID *int64 `json:"group_id"`
}

// GroupResponse is the public settings-group representation.
type GroupResponse struct {
	ID          int64     `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	SortOrder   int       `json:"sort_order"`
	IsSystem    bool      `json:"is_system"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// GroupLightResponse is the lightweight settings-group representation.
type GroupLightResponse struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
}

func (req GroupInsertUpdateRequest) toInput() GroupInput {
	return GroupInput{
		Name:        req.Name,
		Description: req.Description,
		SortOrder:   req.SortOrder,
	}
}

func newGroupResponse(group StoredGroup) GroupResponse {
	return GroupResponse{
		ID:          group.ID,
		Slug:        group.Slug,
		Name:        group.Name,
		Description: group.Description,
		SortOrder:   group.SortOrder,
		IsSystem:    group.IsSystem,
		CreatedAt:   group.CreatedAt,
		UpdatedAt:   group.UpdatedAt,
	}
}

func newGroupLightResponse(group StoredGroup) GroupLightResponse {
	return GroupLightResponse{ID: group.ID, Name: group.Name, SortOrder: group.SortOrder}
}

// UpdateSettingRequest is the internal, target-aware setting update payload.
type UpdateSettingRequest struct {
	Value      any    `json:"value" validate:"required"`
	TargetType string `json:"target_type" validate:"omitempty,oneof=global tenant"`
	TargetID   *int64 `json:"target_id" validate:"omitempty,gt=0"`
}

// TenantUpdateRequest updates the caller's tenant override.
type TenantUpdateRequest struct {
	Value any `json:"value" validate:"required"`
}

// GroupRef is group metadata attached to a setting. ID is 0 when the group has
// not been materialized yet.
type GroupRef struct {
	ID          int64  `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SortOrder   int    `json:"sort_order"`
}

// GroupedSettings is one group with its effective settings.
type GroupedSettings struct {
	Group    GroupRef      `json:"group"`
	Settings []SettingItem `json:"settings"`
}

// SettingItem is one effective setting in a response. Group is present on the
// flat internal views and omitted from the grouped public view. ID is the
// global setting row id (0 when not yet materialized).
type SettingItem struct {
	ID          int64     `json:"id"`
	Key         string    `json:"key"`
	Type        string    `json:"type"`
	Value       any       `json:"value"`
	Default     any       `json:"default"`
	Description string    `json:"description"`
	Source      string    `json:"source"`
	Scope       string    `json:"scope"`
	Editable    bool      `json:"editable"`
	SortOrder   int       `json:"sort_order"`
	Group       *GroupRef `json:"group,omitempty"`
}

func newSettingItem(effective EffectiveSetting, editable, withGroup bool) SettingItem {
	scope := string(effective.Key.Scope)
	if scope == "" {
		scope = "global"
	}
	item := SettingItem{
		ID:          effective.ID,
		Key:         effective.Key.Name,
		Type:        string(effective.Key.Type),
		Value:       effective.Value,
		Default:     effective.Key.Default,
		Description: effective.Key.Description,
		Source:      effective.Source,
		Scope:       scope,
		Editable:    editable,
		SortOrder:   effective.Key.SortOrder,
	}
	if withGroup {
		group := newGroupRef(effective.Group)
		item.Group = &group
	}
	return item
}

func newGroupRef(info GroupInfo) GroupRef {
	return GroupRef{
		ID:          info.ID,
		Slug:        info.Slug,
		Name:        info.Name,
		Description: info.Description,
		SortOrder:   info.SortOrder,
	}
}

// groupSettings groups effective settings by their group, preserving the
// service's group/key ordering. A non-empty filter keeps only that group.
func groupSettings(items []EffectiveSetting, filter string, canWriteTenant bool) []GroupedSettings {
	order := make([]string, 0, len(items))
	bySlug := make(map[string]*GroupedSettings)
	for _, effective := range items {
		slug := effective.Group.Slug
		if filter != "" && slug != filter {
			continue
		}
		group, ok := bySlug[slug]
		if !ok {
			created := GroupedSettings{Group: newGroupRef(effective.Group), Settings: make([]SettingItem, 0)}
			bySlug[slug] = &created
			order = append(order, slug)
			group = &created
		}
		editable := canWriteTenant && effective.Key.AllowsTenant()
		group.Settings = append(group.Settings, newSettingItem(effective, editable, false))
	}
	groups := make([]GroupedSettings, 0, len(order))
	for _, slug := range order {
		groups = append(groups, *bySlug[slug])
	}
	return groups
}

func toSettingItems(items []EffectiveSetting, editable bool) []SettingItem {
	out := make([]SettingItem, 0, len(items))
	for _, effective := range items {
		out = append(out, newSettingItem(effective, editable, true))
	}
	return out
}
