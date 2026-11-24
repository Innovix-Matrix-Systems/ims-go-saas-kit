package featureflags

import (
	"hash/fnv"
	"strconv"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
)

// Who a percentage rollout counts (Flag.RolloutBy).
const (
	RolloutByUser   = "user"   // default: each user is in or out
	RolloutByTenant = "tenant" // a whole tenant is in or out together
)

// state is one stored flag value: on/off plus an optional rollout percentage.
type state struct {
	Value   bool   `json:"v"`
	Rollout *int16 `json:"r,omitempty"`
}

// enabledFor decides a stored state for one subject. A rollout puts each
// subject in a fixed bucket (0-99) per flag, so raising the percentage only
// adds subjects and never flips existing ones off.
func (st state) enabledFor(flag Flag, tenantID, userID int64) bool {
	if !st.Value || st.Rollout == nil {
		return st.Value
	}
	unit, ok := rolloutUnit(flag, tenantID, userID)
	if !ok {
		return *st.Rollout >= 100
	}
	return bucket(flag.Key, unit) < int(*st.Rollout)
}

// decidable reports whether a subject is known well enough to resolve a
// rollout (a global or tenant view of a per-user rollout is not).
func decidable(flag Flag, tenantID, userID int64) bool {
	_, ok := rolloutUnit(flag, tenantID, userID)
	return ok
}

func rolloutUnit(flag Flag, tenantID, userID int64) (string, bool) {
	if flag.RolloutBy == RolloutByTenant {
		if tenantID > 0 {
			return "t" + strconv.FormatInt(tenantID, 10), true
		}
		return "", false
	}
	if userID > 0 {
		return "u" + strconv.FormatInt(userID, 10), true
	}
	return "", false
}

// bucket maps (flag, subject) to 0-99. The flag key is part of the hash so a
// user in the first 10% of one flag is not in the first 10% of every flag.
func bucket(key, unit string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	_, _ = h.Write([]byte{':'})
	_, _ = h.Write([]byte(unit))
	return int(h.Sum32() % 100)
}

// newState builds the stored state for an enable/disable with an optional
// percentage. 100% is stored as "everyone" (nil).
func newState(value bool, rollout *int) (state, error) {
	if rollout == nil {
		return state{Value: value}, nil
	}
	if *rollout < 0 || *rollout > 100 {
		return state{}, apperr.Validation(map[string][]string{"rollout": {"The rollout must be between 0 and 100."}})
	}
	if !value {
		return state{}, apperr.Validation(map[string][]string{"rollout": {"A rollout can only be set on an enabled flag."}})
	}
	if *rollout == 100 {
		return state{Value: true}, nil
	}
	percent := int16(*rollout)
	return state{Value: true, Rollout: &percent}, nil
}
