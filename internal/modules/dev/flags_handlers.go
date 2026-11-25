package dev

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/featureflags"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/httpx"
)

// Feature flags panel: thin delegation to featureflags.Service with target
// search, audited mutations and the effective/source view the UI renders.

type devFlag struct {
	Key         string `json:"key"`
	Value       bool   `json:"value"`
	Rollout     *int   `json:"rollout"`
	Default     bool   `json:"default"`
	Source      string `json:"source"`
	Overridden  bool   `json:"overridden"`
	Description string `json:"description"`
	Group       string `json:"group"`
}

type updateFlagsRequest struct {
	TargetType string           `json:"target_type" validate:"omitempty,oneof=global tenant user"`
	TargetID   *int64           `json:"target_id" validate:"omitempty,gt=0"`
	Flags      map[string]*bool `json:"flags" validate:"required_without=Rollout"`
	Rollout    map[string]int   `json:"rollout" validate:"omitempty,dive,min=0,max=100"`
}

func (h *Handler) listFlags(w http.ResponseWriter, r *http.Request) {
	target, err := flagTargetFromQuery(r)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	items, err := h.flags.EffectiveForTarget(r.Context(), target)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, toDevFlags(items, target))
}

func (h *Handler) updateFlags(w http.ResponseWriter, r *http.Request) {
	actorID := authn.UserID(r)
	var request updateFlagsRequest
	if !h.Bind(w, r, &request) {
		return
	}
	target, err := flagTargetFromBody(request)
	if err != nil {
		h.Error(w, r, err)
		return
	}

	setKeys := make([]string, 0, len(request.Flags))
	clearKeys := make([]string, 0, len(request.Flags))
	for key, value := range request.Flags {
		if value == nil {
			clearKeys = append(clearKeys, key)
			continue
		}
		setKeys = append(setKeys, key)
	}
	for key := range request.Rollout {
		if _, listed := request.Flags[key]; !listed {
			setKeys = append(setKeys, key)
		}
	}
	sort.Strings(setKeys)
	sort.Strings(clearKeys)

	started := time.Now()
	items, opErr := h.flags.SetBatch(r.Context(), actorID, target, request.Flags, request.Rollout)
	h.audit.Record(r.Context(), actorID, "flags.set", flagTargetLabel(target), map[string]any{
		"target_type": target.Type,
		"target_id":   target.ID,
		"set":         setKeys,
		"cleared":     clearKeys,
		"rollout":     request.Rollout,
	}, started, opErr)
	if opErr != nil {
		h.Error(w, r, opErr)
		return
	}
	h.Data(w, r, http.StatusOK, toDevFlags(items, target))
}

func (h *Handler) resetFlag(w http.ResponseWriter, r *http.Request) {
	actorID := authn.UserID(r)
	target, err := flagTargetFromQuery(r)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	key := chi.URLParam(r, "key")
	started := time.Now()
	opErr := h.flags.Delete(r.Context(), target, key)
	h.audit.Record(r.Context(), actorID, "flags.reset", key, map[string]any{
		"target_type": target.Type,
		"target_id":   target.ID,
		"key":         key,
	}, started, opErr)
	if opErr != nil {
		h.Error(w, r, opErr)
		return
	}
	h.Data(w, r, http.StatusOK, map[string]any{
		"key":    key,
		"target": flagTargetLabel(target),
		"reset":  true,
	})
}

func (h *Handler) syncFlags(w http.ResponseWriter, r *http.Request) {
	actorID := authn.UserID(r)
	started := time.Now()
	created, opErr := h.flags.Sync(r.Context())
	h.audit.Record(r.Context(), actorID, "flags.sync", "flags", map[string]any{
		"created": created,
	}, started, opErr)
	if opErr != nil {
		h.Error(w, r, opErr)
		return
	}
	h.Data(w, r, http.StatusOK, map[string]any{"created": created})
}

func (h *Handler) searchTargets(w http.ResponseWriter, r *http.Request) {
	kind, err := targetKind(r.URL.Query().Get("type"))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	page, perPage, offset := httpx.PageParams(r)
	items, total, err := h.targets.List(r.Context(), kind,
		strings.TrimSpace(r.URL.Query().Get("search")), int32(perPage), int32(offset))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.Paginated(w, r, http.StatusOK, items, meta, links)
}

func (h *Handler) targetStats(w http.ResponseWriter, r *http.Request) {
	kind, err := targetKind(r.URL.Query().Get("type"))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	stats, err := h.targets.Stats(r.Context(), kind)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, stats)
}

func targetKind(raw string) (string, error) {
	kind := strings.ToLower(strings.TrimSpace(raw))
	if kind != "tenant" && kind != "user" {
		return "", apperr.Validation(map[string][]string{
			"type": {"The type must be tenant or user."},
		})
	}
	return kind, nil
}

func flagTargetFromQuery(r *http.Request) (featureflags.Target, error) {
	query := r.URL.Query()
	return parseFlagTarget(query.Get("target_type"), query.Get("target_id"))
}

func flagTargetFromBody(request updateFlagsRequest) (featureflags.Target, error) {
	rawID := ""
	if request.TargetID != nil {
		rawID = strconv.FormatInt(*request.TargetID, 10)
	}
	return parseFlagTarget(request.TargetType, rawID)
}

func parseFlagTarget(rawType, rawID string) (featureflags.Target, error) {
	rawType = strings.ToLower(strings.TrimSpace(rawType))
	targetType := featureflags.TargetGlobal
	switch rawType {
	case "", "global":
	case featureflags.TargetTenant, featureflags.TargetUser:
		targetType = rawType
	default:
		return featureflags.Target{}, apperr.Validation(map[string][]string{
			"target_type": {"The selected target_type is not supported."},
		})
	}

	var id int64
	if rawID = strings.TrimSpace(rawID); rawID != "" {
		parsed, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil || parsed < 0 {
			return featureflags.Target{}, apperr.Validation(map[string][]string{
				"target_id": {"The target_id must be a positive integer."},
			})
		}
		id = parsed
	}
	if targetType == featureflags.TargetGlobal {
		if id != 0 {
			return featureflags.Target{}, apperr.Validation(map[string][]string{
				"target_id": {"The target_id must be omitted for the global target."},
			})
		}
		return featureflags.Target{Type: featureflags.TargetGlobal}, nil
	}
	if id <= 0 {
		return featureflags.Target{}, apperr.Validation(map[string][]string{
			"target_id": {"The target_id is required for this target."},
		})
	}
	return featureflags.Target{Type: targetType, ID: id}, nil
}

func toDevFlags(items []featureflags.EffectiveFlag, target featureflags.Target) []devFlag {
	flags := make([]devFlag, 0, len(items))
	for _, item := range items {
		flags = append(flags, devFlag{
			Key:         item.Flag.Key,
			Value:       item.Value,
			Rollout:     item.Rollout,
			Default:     item.Flag.Default,
			Source:      item.Source,
			Overridden:  item.Source == flagTargetSource(target),
			Description: item.Flag.Description,
			Group:       item.Flag.Group,
		})
	}
	return flags
}

func flagTargetSource(target featureflags.Target) string {
	if target.IsGlobal() {
		return "global"
	}
	return target.Type
}

func flagTargetLabel(target featureflags.Target) string {
	switch target.Type {
	case featureflags.TargetTenant:
		return formatTarget("tenant", target.ID)
	case featureflags.TargetUser:
		return formatTarget("user", target.ID)
	default:
		return "global"
	}
}
