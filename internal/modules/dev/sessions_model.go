package dev

import "time"

// SessionStats is the aggregate shown on the Sessions screen.
type SessionStats struct {
	Users        int64 `json:"users"`
	ActiveTokens int64 `json:"active_tokens"`
	Used24h      int64 `json:"used_24h"`
	IdleTokens   int64 `json:"idle_tokens"`
	Inactive30d  int64 `json:"inactive_30d"`
}

// SessionUser is one user owning at least one non-expired token.
type SessionUser struct {
	UserID       int64      `json:"user_id"`
	Name         string     `json:"name"`
	Email        string     `json:"email"`
	TenantID     *int64     `json:"tenant_id"`
	ActiveTokens int64      `json:"active_tokens"`
	TotalTokens  int64      `json:"total_tokens"`
	LastUsedAt   *time.Time `json:"last_used_at"`
}

// SessionToken is one device/session row.
type SessionToken struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Device     string     `json:"device"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	Expired    bool       `json:"expired"`
	Current    bool       `json:"current"`
}

// SessionUserFilter filters the session user directory.
type SessionUserFilter struct {
	Search   string
	BeforeID int64
	Limit    int32
}

// SessionPruneResult is the preview/execute result of a session prune.
type SessionPruneResult struct {
	IdleDays int32 `json:"idle_days"`
	DryRun   bool  `json:"dry_run"`
	Affected int64 `json:"affected"`
}
