package dev

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/settings"
)

// Settings panel: thin delegation to settings.Service. Groups are editable and
// catalog keys can be dragged between them; the settings themselves remain
// registry-owned (no key creation/deletion).

type devSettingGroup struct {
	ID          int64  `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SortOrder   int    `json:"sort_order"`
	IsSystem    bool   `json:"is_system"`
}

type devSetting struct {
	ID          int64           `json:"id"`
	Key         string          `json:"key"`
	Type        string          `json:"type"`
	Value       any             `json:"value"`
	Default     any             `json:"default"`
	Description string          `json:"description"`
	Source      string          `json:"source"`
	Scope       string          `json:"scope"`
	Editable    bool            `json:"editable"`
	Overridden  bool            `json:"overridden"`
	SortOrder   int             `json:"sort_order"`
	Group       devSettingGroup `json:"group"`
}

type settingsGroupRequest struct {
	Name        string  `json:"name" validate:"required,min=2,max=255"`
	Description *string `json:"description" validate:"omitempty,max=255"`
	SortOrder   *int    `json:"sort_order" validate:"omitempty,gte=0"`
}

type setSettingRequest struct {
	TargetType string `json:"target_type" validate:"omitempty,oneof=global tenant"`
	TargetID   *int64 `json:"target_id" validate:"omitempty,gt=0"`
	Value      any    `json:"value"`
}

type assignSettingGroupRequest struct {
	GroupID *int64 `json:"group_id"`
}

func (h *Handler) listDevSettingGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.settings.ListGroupsLight(r.Context(), "sort_order", "asc")
	if err != nil {
		h.Error(w, r, err)
		return
	}
	items := make([]devSettingGroup, 0, len(groups))
	for _, group := range groups {
		items = append(items, devGroupFromStored(group))
	}
	h.Data(w, r, http.StatusOK, items)
}

func (h *Handler) createDevSettingGroup(w http.ResponseWriter, r *http.Request) {
	actorID := authn.UserID(r)
	var request settingsGroupRequest
	if !h.Bind(w, r, &request) {
		return
	}
	started := time.Now()
	group, opErr := h.settings.CreateGroup(r.Context(), actorID, settings.GroupInput{
		Name:        request.Name,
		Description: request.Description,
		SortOrder:   request.SortOrder,
		ActorID:     &actorID,
	})
	target := "settings_group:new"
	if group != nil {
		target = formatTarget("settings_group", group.ID)
	}
	h.audit.Record(r.Context(), actorID, "settings.group.create", target, map[string]any{
		"name":       request.Name,
		"sort_order": request.SortOrder,
	}, started, opErr)
	if opErr != nil {
		h.Error(w, r, opErr)
		return
	}
	h.Data(w, r, http.StatusCreated, devGroupFromStored(*group))
}

func (h *Handler) updateDevSettingGroup(w http.ResponseWriter, r *http.Request) {
	actorID := authn.UserID(r)
	groupID, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	var request settingsGroupRequest
	if !h.Bind(w, r, &request) {
		return
	}
	started := time.Now()
	group, opErr := h.settings.UpdateGroup(r.Context(), actorID, groupID, settings.GroupInput{
		Name:        request.Name,
		Description: request.Description,
		SortOrder:   request.SortOrder,
		ActorID:     &actorID,
	})
	h.audit.Record(r.Context(), actorID, "settings.group.update", formatTarget("settings_group", groupID), map[string]any{
		"name":       request.Name,
		"sort_order": request.SortOrder,
	}, started, opErr)
	if opErr != nil {
		h.Error(w, r, opErr)
		return
	}
	h.Data(w, r, http.StatusOK, devGroupFromStored(*group))
}

func (h *Handler) deleteDevSettingGroup(w http.ResponseWriter, r *http.Request) {
	actorID := authn.UserID(r)
	groupID, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	started := time.Now()
	opErr := h.settings.DeleteGroup(r.Context(), groupID)
	h.audit.Record(r.Context(), actorID, "settings.group.delete", formatTarget("settings_group", groupID),
		map[string]any{"group_id": groupID}, started, opErr)
	if opErr != nil {
		h.Error(w, r, opErr)
		return
	}
	h.Data(w, r, http.StatusOK, map[string]any{"deleted": true})
}

func (h *Handler) listDevSettings(w http.ResponseWriter, r *http.Request) {
	target, err := settingsTargetFromQuery(r)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	items, err := h.settings.EffectiveForTarget(r.Context(), target)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, toDevSettings(items, target))
}

func (h *Handler) setDevSetting(w http.ResponseWriter, r *http.Request) {
	actorID := authn.UserID(r)
	var request setSettingRequest
	if !h.Bind(w, r, &request) {
		return
	}
	if request.Value == nil {
		h.Error(w, r, apperr.Validation(map[string][]string{"value": {"The value field is required."}}))
		return
	}
	target, err := settingsTargetFromBody(request.TargetType, request.TargetID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	key := chi.URLParam(r, "key")
	started := time.Now()
	effective, opErr := h.settings.SetTarget(r.Context(), key, target, request.Value)
	h.audit.Record(r.Context(), actorID, "settings.set", "setting:"+key, map[string]any{
		"key":         key,
		"target_type": target.Type,
		"target_id":   target.ID,
		"value":       request.Value,
	}, started, opErr)
	if opErr != nil {
		h.Error(w, r, opErr)
		return
	}
	h.Data(w, r, http.StatusOK, toDevSettings([]settings.EffectiveSetting{*effective}, target)[0])
}

func (h *Handler) resetDevSetting(w http.ResponseWriter, r *http.Request) {
	actorID := authn.UserID(r)
	target, err := settingsTargetFromQuery(r)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	key := chi.URLParam(r, "key")
	started := time.Now()
	opErr := h.settings.DeleteTarget(r.Context(), key, target)
	h.audit.Record(r.Context(), actorID, "settings.reset", key, map[string]any{
		"key":         key,
		"target_type": target.Type,
		"target_id":   target.ID,
	}, started, opErr)
	if opErr != nil {
		h.Error(w, r, opErr)
		return
	}
	h.Data(w, r, http.StatusOK, map[string]any{
		"key":    key,
		"target": settingsTargetLabel(target),
		"reset":  true,
	})
}

func (h *Handler) assignDevSettingGroup(w http.ResponseWriter, r *http.Request) {
	actorID := authn.UserID(r)
	key := chi.URLParam(r, "key")
	var request assignSettingGroupRequest
	if !h.Bind(w, r, &request) {
		return
	}
	started := time.Now()
	_, opErr := h.settings.AssignKeyGroup(r.Context(), key, request.GroupID)
	h.audit.Record(r.Context(), actorID, "settings.group.assign", key, map[string]any{
		"key":      key,
		"group_id": request.GroupID,
	}, started, opErr)
	if opErr != nil {
		h.Error(w, r, opErr)
		return
	}
	h.Data(w, r, http.StatusOK, map[string]any{
		"key":      key,
		"group_id": request.GroupID,
	})
}

func (h *Handler) syncDevSettings(w http.ResponseWriter, r *http.Request) {
	actorID := authn.UserID(r)
	started := time.Now()
	created, opErr := h.settings.Sync(r.Context())
	h.audit.Record(r.Context(), actorID, "settings.sync", "settings", map[string]any{
		"created": created,
	}, started, opErr)
	if opErr != nil {
		h.Error(w, r, opErr)
		return
	}
	h.Data(w, r, http.StatusOK, map[string]any{"created": created})
}

func toDevSettings(items []settings.EffectiveSetting, target settings.Target) []devSetting {
	out := make([]devSetting, 0, len(items))
	for _, item := range items {
		scope := string(item.Key.Scope)
		if scope == "" {
			scope = "global"
		}
		out = append(out, devSetting{
			ID:          item.ID,
			Key:         item.Key.Name,
			Type:        string(item.Key.Type),
			Value:       item.Value,
			Default:     item.Key.Default,
			Description: item.Key.Description,
			Source:      item.Source,
			Scope:       scope,
			Editable:    target.IsGlobal() || (target.Type == settings.TargetTenant && item.Key.AllowsTenant()),
			Overridden:  settingsSourceMatchesTarget(item.Source, target),
			SortOrder:   item.SortOrder,
			Group:       devGroupFromInfo(item.Group),
		})
	}
	return out
}

func settingsSourceMatchesTarget(source string, target settings.Target) bool {
	if target.IsGlobal() {
		return source == "global"
	}
	return source == target.Type
}

func devGroupFromInfo(info settings.GroupInfo) devSettingGroup {
	return devSettingGroup{
		ID:          info.ID,
		Slug:        info.Slug,
		Name:        info.Name,
		Description: info.Description,
		SortOrder:   info.SortOrder,
		IsSystem:    info.IsSystem,
	}
}

func devGroupFromStored(group settings.StoredGroup) devSettingGroup {
	item := devSettingGroup{
		ID:        group.ID,
		Slug:      group.Slug,
		Name:      group.Name,
		SortOrder: group.SortOrder,
		IsSystem:  group.IsSystem,
	}
	if group.Description != nil {
		item.Description = *group.Description
	}
	return item
}

func settingsTargetFromQuery(r *http.Request) (settings.Target, error) {
	query := r.URL.Query()
	return parseSettingsTarget(query.Get("target_type"), query.Get("target_id"))
}

func settingsTargetFromBody(targetType string, targetID *int64) (settings.Target, error) {
	rawID := ""
	if targetID != nil {
		rawID = strconv.FormatInt(*targetID, 10)
	}
	return parseSettingsTarget(targetType, rawID)
}

func parseSettingsTarget(rawType, rawID string) (settings.Target, error) {
	rawType = strings.ToLower(strings.TrimSpace(rawType))
	targetType := settings.TargetGlobal
	switch rawType {
	case "", "global":
	case settings.TargetTenant:
		targetType = rawType
	default:
		return settings.Target{}, apperr.Validation(map[string][]string{
			"target_type": {"The selected target_type is not supported for settings."},
		})
	}

	var id int64
	if rawID = strings.TrimSpace(rawID); rawID != "" {
		parsed, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil || parsed < 0 {
			return settings.Target{}, apperr.Validation(map[string][]string{
				"target_id": {"The target_id must be a positive integer."},
			})
		}
		id = parsed
	}
	if targetType == settings.TargetGlobal {
		if id != 0 {
			return settings.Target{}, apperr.Validation(map[string][]string{
				"target_id": {"The target_id must be omitted for the global target."},
			})
		}
		return settings.Target{Type: settings.TargetGlobal}, nil
	}
	if id <= 0 {
		return settings.Target{}, apperr.Validation(map[string][]string{
			"target_id": {"The target_id is required for the tenant target."},
		})
	}
	return settings.Target{Type: targetType, ID: id}, nil
}

func settingsTargetLabel(target settings.Target) string {
	if target.IsGlobal() {
		return "global"
	}
	return formatTarget("tenant", target.ID)
}

type placeSettingsRequest struct {
	Keys []string `json:"keys" validate:"required,min=1,max=500,dive,required"`
}

// placeSettings puts keys into a group in the given order (drag and drop).
func (h *Handler) placeSettings(w http.ResponseWriter, r *http.Request) {
	groupID, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	var request placeSettingsRequest
	if !h.Bind(w, r, &request) {
		return
	}
	started := time.Now()
	opErr := h.settings.PlaceKeys(r.Context(), groupID, request.Keys)
	h.audit.Record(r.Context(), authn.UserID(r), "settings.place", formatTarget("settings_group", groupID),
		map[string]any{"group_id": groupID, "keys": request.Keys}, started, opErr)
	if opErr != nil {
		h.Error(w, r, opErr)
		return
	}
	h.Data(w, r, http.StatusOK, map[string]any{"group_id": groupID, "keys": request.Keys})
}

type reorderGroupsRequest struct {
	IDs []int64 `json:"ids" validate:"required,min=1,max=200,dive,gt=0"`
}

// reorderSettingGroups sets the order of settings groups.
func (h *Handler) reorderSettingGroups(w http.ResponseWriter, r *http.Request) {
	var request reorderGroupsRequest
	if !h.Bind(w, r, &request) {
		return
	}
	started := time.Now()
	opErr := h.settings.ReorderGroups(r.Context(), request.IDs)
	h.audit.Record(r.Context(), authn.UserID(r), "settings.groups.reorder", "settings_groups",
		map[string]any{"ids": request.IDs}, started, opErr)
	if opErr != nil {
		h.Error(w, r, opErr)
		return
	}
	h.Data(w, r, http.StatusOK, map[string]any{"ids": request.IDs})
}
