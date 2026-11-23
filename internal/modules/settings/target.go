package settings

// Target identifies a polymorphic settings/feature-flag target. The zero value
// is the global target (no target_type/target_id).
type Target struct {
	Type string // "" = global, "tenant", "user"
	ID   int64
}

// Target type values.
const (
	TargetGlobal = ""
	TargetTenant = "tenant"
	TargetUser   = "user"
)

// TypePtr returns the nullable target_type column value.
func (t Target) TypePtr() *string {
	if t.Type == TargetGlobal {
		return nil
	}
	value := t.Type
	return &value
}

// IDPtr returns the nullable target_id column value.
func (t Target) IDPtr() *int64 {
	if t.Type == TargetGlobal {
		return nil
	}
	value := t.ID
	return &value
}

// IsGlobal reports whether the target is the global target.
func (t Target) IsGlobal() bool {
	return t.Type == TargetGlobal
}
