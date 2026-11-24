package featureflags

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/tracing"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

const flagsTTL = 5 * time.Minute

// v2: blobs carry a rollout percentage next to the value.
func globalBlobKey() string               { return "flags:v2:g" }
func tenantBlobKey(tenantID int64) string { return fmt.Sprintf("flags:v2:t:%d", tenantID) }
func userBlobKey(userID int64) string     { return fmt.Sprintf("flags:v2:u:%d", userID) }

// blob is one target's key -> stored state cache entry.
type blob map[string]state

// EffectiveFlag is one catalog flag resolved for a target.
type EffectiveFlag struct {
	Flag    Flag
	Value   bool
	Rollout *int   // percentage when the winning value is a partial rollout
	Source  string // user | tenant | global | default
}

// Service resolves and persists feature flags.
type Service struct {
	repo  Repository
	cache cache.Store
}

// NewService creates the feature-flag service.
func NewService(repo Repository, store cache.Store) *Service {
	return &Service{repo: repo, cache: store}
}

// Effective resolves every catalog flag for the caller:
// user -> tenant -> global -> default, then applies any percentage rollout.
func (s *Service) Effective(ctx context.Context, scope tenancy.Scope, userID int64) (map[string]bool, error) {
	tenantID := scope.EffectiveTenantID()
	levels, err := s.levels(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(Catalog))
	for _, flag := range Catalog {
		st, _ := resolve(flag, levels)
		result[flag.Key] = st.enabledFor(flag, tenantID, userID)
	}
	return result, nil
}

// IsEnabled resolves a single flag for the caller (used by the gate middleware).
func (s *Service) IsEnabled(ctx context.Context, scope tenancy.Scope, userID int64, key string) (bool, error) {
	if _, ok := Lookup(key); !ok {
		return false, apperr.Newf(apperr.InternalServerError, "Feature flag %q is not registered.", key)
	}
	values, err := s.Effective(ctx, scope, userID)
	if err != nil {
		return false, err
	}
	return values[key], nil
}

// EffectiveForTarget resolves every catalog flag for an explicit target. For a
// user target a rollout is decided for that user; for a global (or tenant)
// view of a per-user rollout, Value is "on" and Rollout says for how many.
func (s *Service) EffectiveForTarget(ctx context.Context, target Target) ([]EffectiveFlag, error) {
	var tenantID, userID int64
	switch target.Type {
	case TargetTenant:
		tenantID = target.ID
	case TargetUser:
		userID = target.ID
		owner, exists, err := s.repo.UserTenant(ctx, target.ID)
		if err != nil {
			return nil, err
		}
		if exists {
			tenantID = owner
		}
	}
	levels, err := s.levels(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}

	out := make([]EffectiveFlag, 0, len(Catalog))
	for _, flag := range Catalog {
		st, source := resolve(flag, levels)
		item := EffectiveFlag{Flag: flag, Value: st.Value, Source: source}
		if st.Rollout != nil {
			percent := int(*st.Rollout)
			item.Rollout = &percent
			if decidable(flag, tenantID, userID) {
				item.Value = st.enabledFor(flag, tenantID, userID)
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// SetBatch applies a batch of flag values to a target. A nil value clears that
// override (inherit from the next level). rollout optionally limits an enabled
// flag to a percentage of users (or tenants, see Flag.RolloutBy); a key that
// only appears in rollout is enabled for that percentage.
func (s *Service) SetBatch(ctx context.Context, actorID int64, target Target, flags map[string]*bool, rollout map[string]int) (_ []EffectiveFlag, err error) {
	ctx, end := tracing.Begin(ctx, "featureflags.set", attribute.Int("flags", len(flags)))
	defer end(&err)
	if err := s.validateTarget(ctx, target); err != nil {
		return nil, err
	}

	type change struct {
		clear bool
		state state
	}
	changes := make(map[string]change, len(flags)+len(rollout))
	for key, value := range flags {
		if value == nil {
			if _, ok := rollout[key]; ok {
				return nil, apperr.Validation(map[string][]string{"rollout." + key: {"A cleared flag cannot also set a rollout."}})
			}
			changes[key] = change{clear: true}
			continue
		}
		changes[key] = change{state: state{Value: *value}}
	}
	for key, percent := range rollout {
		value := true
		if current, ok := flags[key]; ok && current != nil {
			value = *current
		}
		st, err := newState(value, &percent)
		if err != nil {
			return nil, err
		}
		changes[key] = change{state: st}
	}
	for key := range changes {
		if _, ok := Lookup(key); !ok {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Feature flag %q is not registered.", key)
		}
	}

	var actor *int64
	if actorID > 0 {
		actor = &actorID
	}
	for key, c := range changes {
		var err error
		switch {
		case c.clear:
			_, err = s.repo.Delete(ctx, key, target)
		case target.IsGlobal():
			_, err = s.repo.UpsertGlobal(ctx, key, c.state.Value, c.state.Rollout, actor)
		default:
			_, err = s.repo.UpsertTarget(ctx, key, target, c.state.Value, c.state.Rollout, actor)
		}
		if err != nil {
			return nil, err
		}
	}
	s.invalidate(ctx, target)
	return s.EffectiveForTarget(ctx, target)
}

// Delete clears a single override at the target.
func (s *Service) Delete(ctx context.Context, target Target, key string) (err error) {
	ctx, end := tracing.Begin(ctx, "featureflags.delete")
	defer end(&err)
	if _, ok := Lookup(key); !ok {
		return apperr.Newf(apperr.ResourceNotFound, "Feature flag %q is not registered.", key)
	}
	if err := s.validateTarget(ctx, target); err != nil {
		return err
	}
	deleted, err := s.repo.Delete(ctx, key, target)
	if err != nil {
		return err
	}
	if !deleted {
		return apperr.Newf(apperr.ResourceNotFound, "No stored value exists for feature flag %q at the requested target.", key)
	}
	s.invalidate(ctx, target)
	return nil
}

// Sync inserts missing global defaults and returns how many were created.
func (s *Service) Sync(ctx context.Context) (_ int, err error) {
	ctx, end := tracing.Begin(ctx, "featureflags.sync")
	defer end(&err)
	created := 0
	for _, flag := range Catalog {
		_, inserted, err := s.repo.InsertGlobalIfAbsent(ctx, flag.Key, flag.Default)
		if err != nil {
			return created, err
		}
		if inserted {
			created++
		}
	}
	_ = s.cache.Delete(ctx, globalBlobKey())
	return created, nil
}

// flagLevels holds the stored states from most to least specific.
type flagLevels struct{ user, tenant, global blob }

func (s *Service) levels(ctx context.Context, tenantID, userID int64) (flagLevels, error) {
	var levels flagLevels
	var err error
	if levels.global, err = s.load(ctx, globalBlobKey(), Target{Type: TargetGlobal}); err != nil {
		return levels, err
	}
	if tenantID > 0 {
		if levels.tenant, err = s.load(ctx, tenantBlobKey(tenantID), Target{Type: TargetTenant, ID: tenantID}); err != nil {
			return levels, err
		}
	}
	if userID > 0 {
		if levels.user, err = s.load(ctx, userBlobKey(userID), Target{Type: TargetUser, ID: userID}); err != nil {
			return levels, err
		}
	}
	return levels, nil
}

// resolve picks the most specific stored state: user -> tenant -> global ->
// code default.
func resolve(flag Flag, levels flagLevels) (state, string) {
	if st, ok := levels.user[flag.Key]; ok {
		return st, "user"
	}
	if st, ok := levels.tenant[flag.Key]; ok {
		return st, "tenant"
	}
	if st, ok := levels.global[flag.Key]; ok {
		return st, "global"
	}
	return state{Value: flag.Default}, "default"
}

func (s *Service) validateTarget(ctx context.Context, target Target) error {
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
		exists, err := s.repo.TenantExists(ctx, target.ID)
		if err != nil {
			return err
		}
		if !exists {
			return apperr.Validation(map[string][]string{"target_id": {"The selected tenant does not exist."}})
		}
		return nil
	case TargetUser:
		if target.ID <= 0 {
			return apperr.Validation(map[string][]string{"target_id": {"The target_id is required for the user target."}})
		}
		_, exists, err := s.repo.UserTenant(ctx, target.ID)
		if err != nil {
			return err
		}
		if !exists {
			return apperr.Validation(map[string][]string{"target_id": {"The selected user does not exist."}})
		}
		return nil
	default:
		return apperr.Validation(map[string][]string{"target_type": {"The selected target_type is not supported."}})
	}
}

func (s *Service) invalidate(ctx context.Context, target Target) {
	switch target.Type {
	case TargetGlobal:
		_ = s.cache.Delete(ctx, globalBlobKey())
	case TargetTenant:
		_ = s.cache.Delete(ctx, tenantBlobKey(target.ID))
	case TargetUser:
		_ = s.cache.Delete(ctx, userBlobKey(target.ID))
	}
}

// load reads one target's stored states, cached. A cache outage degrades to
// the database instead of failing the request.
func (s *Service) load(ctx context.Context, key string, target Target) (blob, error) {
	var cached blob
	if found, err := s.cache.Get(ctx, key, &cached); err == nil && found {
		return cached, nil
	}
	var rows []FeatureFlag
	var err error
	if target.IsGlobal() {
		rows, err = s.repo.ListGlobal(ctx)
	} else {
		rows, err = s.repo.ListByTarget(ctx, target)
	}
	if err != nil {
		return nil, err
	}
	values := make(blob, len(rows))
	for _, row := range rows {
		values[row.Key] = state{Value: row.Value, Rollout: row.Rollout}
	}
	_ = s.cache.Set(ctx, key, values, flagsTTL)
	return values, nil
}
