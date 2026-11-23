package config

import (
	"errors"
	"strings"
	"time"
)

// MCP server and its OAuth authorization server (MCP_*). See
// docs/guides/mcp.md.

// MCP holds the MCP server settings.
type MCP struct {
	// Enabled mounts /mcp, /oauth/* and the OAuth well-known documents
	// (MCP_ENABLED, default false). Off, they answer 404.
	Enabled bool
	// AccessTokenTTL is how long an MCP access token lives (default 1h).
	AccessTokenTTL time.Duration
	// RefreshTokenTTL is how long an unused refresh token lives; every use
	// issues a new one, so an active connection never expires (default 720h).
	RefreshTokenTTL time.Duration
	// CIMDEnabled lets any client connect with a Client ID Metadata Document
	// (an https client_id), with no registration (default true). Off, only
	// clients registered by an admin can connect.
	CIMDEnabled bool
	// AllowedClientHosts limits CIMD clients to these hosts (empty = any).
	AllowedClientHosts []string
	// SecretGrace keeps a registered client's previous secret valid this long
	// after a rotation (default 24h).
	SecretGrace time.Duration
	// MaxRequestBytes caps one MCP request body (default 1 MiB).
	MaxRequestBytes int64
	// MaxResultBytes caps one tool result (default 256 KiB); a bigger result
	// is refused with a tool error that asks for a smaller page.
	MaxResultBytes int
	// RateLimit is the tool calls per minute per token (default 120).
	RateLimit int
}

func (c *Config) loadMCP() error {
	cfg := MCP{}
	var err error
	if cfg.Enabled, err = getBool("MCP_ENABLED", false); err != nil {
		return err
	}
	if cfg.AccessTokenTTL, err = getDuration("MCP_ACCESS_TOKEN_TTL", time.Hour); err != nil {
		return err
	}
	if cfg.RefreshTokenTTL, err = getDuration("MCP_REFRESH_TOKEN_TTL", 720*time.Hour); err != nil {
		return err
	}
	if cfg.CIMDEnabled, err = getBool("MCP_OAUTH_CIMD_ENABLED", true); err != nil {
		return err
	}
	if cfg.SecretGrace, err = getDuration("MCP_OAUTH_SECRET_GRACE", 24*time.Hour); err != nil {
		return err
	}
	if cfg.MaxRequestBytes, err = getInt64("MCP_MAX_REQUEST_BYTES", 1<<20); err != nil {
		return err
	}
	if cfg.MaxResultBytes, err = getInt("MCP_MAX_RESULT_BYTES", 256<<10); err != nil {
		return err
	}
	if cfg.RateLimit, err = getInt("MCP_RATE_LIMIT", 120); err != nil {
		return err
	}
	for _, host := range strings.Split(getString("MCP_OAUTH_ALLOWED_CLIENT_HOSTS", ""), ",") {
		if host = strings.ToLower(strings.TrimSpace(host)); host != "" {
			cfg.AllowedClientHosts = append(cfg.AllowedClientHosts, host)
		}
	}

	switch {
	case cfg.AccessTokenTTL < time.Minute:
		return errors.New("MCP_ACCESS_TOKEN_TTL must be at least 1m")
	case cfg.RefreshTokenTTL < cfg.AccessTokenTTL:
		return errors.New("MCP_REFRESH_TOKEN_TTL must not be shorter than MCP_ACCESS_TOKEN_TTL")
	case cfg.SecretGrace < 0:
		return errors.New("MCP_OAUTH_SECRET_GRACE must not be negative")
	case cfg.MaxRequestBytes <= 0, cfg.MaxResultBytes <= 0:
		return errors.New("MCP_MAX_REQUEST_BYTES and MCP_MAX_RESULT_BYTES must be greater than zero")
	case cfg.RateLimit <= 0:
		return errors.New("MCP_RATE_LIMIT must be greater than zero")
	}
	c.MCP = cfg
	return nil
}
