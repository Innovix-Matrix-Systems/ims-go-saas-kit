package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Social login (SOCIAL_*): sign in with an outside provider such as Google
// or GitHub. See docs/guides/social-login.md.

// Social holds the social login settings.
type Social struct {
	// Providers are the enabled providers, in the order the login screen
	// shows them (SOCIAL_PROVIDERS, default empty = social login off).
	Providers []string
	Google    GoogleLogin
	GitHub    GitHubLogin
}

// GoogleLogin holds the Google sign-in settings.
type GoogleLogin struct {
	// ClientIDs are the OAuth client ids an id_token may be issued to: the
	// web client and any iOS or Android client (SOCIAL_GOOGLE_CLIENT_IDS).
	ClientIDs []string
}

// GitHubLogin holds the GitHub OAuth app settings.
type GitHubLogin struct {
	ClientID     string // SOCIAL_GITHUB_CLIENT_ID
	ClientSecret string // SOCIAL_GITHUB_CLIENT_SECRET
}

func (c *Config) loadSocial() error {
	cfg := Social{
		Providers: splitCSV(strings.ToLower(getString("SOCIAL_PROVIDERS", ""))),
		Google:    GoogleLogin{ClientIDs: splitCSV(getString("SOCIAL_GOOGLE_CLIENT_IDS", ""))},
		GitHub: GitHubLogin{
			ClientID:     strings.TrimSpace(getString("SOCIAL_GITHUB_CLIENT_ID", "")),
			ClientSecret: strings.TrimSpace(getString("SOCIAL_GITHUB_CLIENT_SECRET", "")),
		},
	}

	// A listed provider without its credentials would only fail at the first
	// sign-in; refuse to start instead.
	for i, provider := range cfg.Providers {
		if slices.Contains(cfg.Providers[:i], provider) {
			return fmt.Errorf("SOCIAL_PROVIDERS lists %q twice", provider)
		}
		switch provider {
		case "google":
			if len(cfg.Google.ClientIDs) == 0 {
				return errors.New("SOCIAL_PROVIDERS includes google: SOCIAL_GOOGLE_CLIENT_IDS must not be empty")
			}
		case "github":
			if cfg.GitHub.ClientID == "" || cfg.GitHub.ClientSecret == "" {
				return errors.New("SOCIAL_PROVIDERS includes github: SOCIAL_GITHUB_CLIENT_ID and SOCIAL_GITHUB_CLIENT_SECRET must be set")
			}
		default:
			return fmt.Errorf("SOCIAL_PROVIDERS %q is not supported (google|github)", provider)
		}
	}
	c.Social = cfg
	return nil
}
