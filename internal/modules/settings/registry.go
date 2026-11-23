// Package settings implements the typed settings catalog, grouped REST surface
// and service. Values resolve tenant override -> global row -> code default.
package settings

import (
	"fmt"
	"math"
)

// ValueType describes the storage/validation type of a setting.
type ValueType string

const (
	TypeString ValueType = "string"
	TypeInt    ValueType = "int"
	TypeBool   ValueType = "bool"
	TypeJSON   ValueType = "json"
)

// Scope declares which targets may override a setting. The zero value
// (ScopeGlobalOnly) means no tenant override is legal.
type Scope string

const (
	// ScopeGlobalOnly is the zero value: only the global row (or default) applies.
	ScopeGlobalOnly Scope = ""
	// ScopeTenant allows a per-tenant override.
	ScopeTenant Scope = "tenant"
)

// Key is one typed setting definition.
type Key struct {
	Name        string
	Type        ValueType
	Default     any
	Description string
	Group       string // settings_groups.slug
	SortOrder   int
	Scope       Scope
}

// AllowsTenant reports whether a tenant override is legal for the key.
func (k Key) AllowsTenant() bool {
	return k.Scope == ScopeTenant
}

// Group is one settings group definition (settings_groups row seed).
type Group struct {
	Slug        string
	Name        string
	Description string
	SortOrder   int
}

// Groups is the catalog-owned group set; `settings:sync` upserts them.
var Groups = []Group{
	{Slug: "ungrouped", Name: "Ungrouped", Description: "Settings parked outside a functional group.", SortOrder: 0},
	{Slug: "general", Name: "General", Description: "Core application settings.", SortOrder: 1},
	{Slug: "notifications", Name: "Notifications", Description: "Outbound notification behavior.", SortOrder: 2},
	{Slug: "data-processing", Name: "Data Processing", Description: "Import/export retention and processing.", SortOrder: 3},
	{Slug: "branding", Name: "Branding", Description: "Name, logo, colour and footer shown in e-mails.", SortOrder: 4},
	{Slug: "security", Name: "Security", Description: "Account security: sign-in alerts and two-factor policy.", SortOrder: 5},
	{Slug: "privacy", Name: "Privacy", Description: "Account deletion and personal-data export.", SortOrder: 6},
}

// Catalog is the application setting catalog; `settings:sync` upserts globals.
var Catalog = []Key{
	{
		Name:        "system.name",
		Type:        TypeString,
		Default:     "IMS Go Admin Starter",
		Description: "Display name used in notifications and the dashboard.",
		Group:       "general",
		SortOrder:   10,
		Scope:       ScopeGlobalOnly,
	},
	{
		Name:        "notification.enabled",
		Type:        TypeBool,
		Default:     true,
		Description: "Toggles outbound notification fan-out.",
		Group:       "notifications",
		SortOrder:   10,
		Scope:       ScopeTenant,
	},
	{
		Name:        "data_processing_job.cleanup_days",
		Type:        TypeInt,
		Default:     30,
		Description: "Retention in days for completed import/export job records.",
		Group:       "data-processing",
		SortOrder:   10,
		Scope:       ScopeTenant,
	},
	// Privacy: account deletion and personal-data export
	// (docs/design/account-deletion-and-export.md).
	{
		Name:        "privacy.deletion_grace_days",
		Type:        TypeInt,
		Default:     14,
		Description: "Days between a user asking to delete their account and the erasure; they can cancel until then (0 erases at the next run).",
		Group:       "privacy",
		SortOrder:   10,
		Scope:       ScopeTenant,
	},
	{
		Name:        "privacy.export_ttl_hours",
		Type:        TypeInt,
		Default:     72,
		Description: "Hours a personal-data export stays downloadable before it is deleted.",
		Group:       "privacy",
		SortOrder:   20,
	},
	{
		Name:        "privacy.erase_soft_deleted_after_days",
		Type:        TypeInt,
		Default:     0,
		Description: "Erase users who have been deleted for this many days (0 keeps deleted users until someone erases them).",
		Group:       "privacy",
		SortOrder:   30,
	},
	// Security (phase 26.3).
	{
		Name:        "security.new_sign_in_alert",
		Type:        TypeBool,
		Default:     false,
		Description: "Mail users when they sign in from an IP address not used in the last 90 days.",
		Group:       "security",
		SortOrder:   10,
		Scope:       ScopeTenant,
	},
	{
		Name:        "security.require_two_factor",
		Type:        TypeBool,
		Default:     false,
		Description: "Users must turn on two-factor authentication; until they do, signing in gives a token that can only set it up.",
		Group:       "security",
		SortOrder:   20,
		Scope:       ScopeTenant,
	},
	// Branding (phase 26.2): the e-mail layout. A tenant override applies only
	// when the tenant's plan has custom.branding.
	{
		Name:        "branding.name",
		Type:        TypeString,
		Default:     "",
		Description: "Name shown in e-mails (empty: system.name).",
		Group:       "branding",
		SortOrder:   10,
		Scope:       ScopeTenant,
	},
	{
		Name:        "branding.logo_url",
		Type:        TypeString,
		Default:     "",
		Description: "Logo shown at the top of e-mails (an absolute https URL; empty: the name).",
		Group:       "branding",
		SortOrder:   20,
		Scope:       ScopeTenant,
	},
	{
		Name:        "branding.color",
		Type:        TypeString,
		Default:     "#2563eb",
		Description: "Accent colour of e-mails (buttons, header line), as #rrggbb.",
		Group:       "branding",
		SortOrder:   30,
		Scope:       ScopeTenant,
	},
	{
		Name:        "branding.footer",
		Type:        TypeString,
		Default:     "",
		Description: "Footer line of e-mails (empty: © and the name).",
		Group:       "branding",
		SortOrder:   40,
		Scope:       ScopeTenant,
	},
}

// Lookup finds a catalog entry by name.
func Lookup(name string) (Key, bool) {
	for _, key := range Catalog {
		if key.Name == name {
			return key, true
		}
	}
	return Key{}, false
}

// LookupGroup finds a catalog group by slug.
func LookupGroup(slug string) (Group, bool) {
	for _, group := range Groups {
		if group.Slug == slug {
			return group, true
		}
	}
	return Group{}, false
}

// Coerce validates and converts a value to the key's type. JSON-decoded
// numbers arrive as float64; direct Go calls may pass int/int64.
func (k Key) Coerce(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	switch k.Type {
	case TypeString:
		if _, ok := value.(string); !ok {
			return nil, fmt.Errorf("setting %q expects a string", k.Name)
		}
		return value, nil
	case TypeBool:
		if _, ok := value.(bool); !ok {
			return nil, fmt.Errorf("setting %q expects a boolean", k.Name)
		}
		return value, nil
	case TypeInt:
		switch typed := value.(type) {
		case float64:
			// JSON numbers are floats: accept only whole numbers that fit
			// an int, never silently truncate 1.9 to 1 (M29).
			if typed != math.Trunc(typed) || math.Abs(typed) > math.MaxInt32 {
				return nil, fmt.Errorf("setting %q expects an integer", k.Name)
			}
			return int(typed), nil
		case int:
			return typed, nil
		case int64:
			return int(typed), nil
		default:
			return nil, fmt.Errorf("setting %q expects an integer", k.Name)
		}
	case TypeJSON:
		switch value.(type) {
		case map[string]any, []any, string, float64, bool, int, int64:
			return value, nil
		default:
			return nil, fmt.Errorf("setting %q expects a JSON value", k.Name)
		}
	default:
		return nil, fmt.Errorf("setting %q has unsupported type %q", k.Name, k.Type)
	}
}
