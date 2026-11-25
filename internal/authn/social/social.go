// Package social verifies a sign-in made at an outside provider (Google,
// GitHub, …) and reports the e-mail address it proved. It knows nothing about
// our users or tokens: authn.Service looks the address up and signs the user
// in exactly as after a password check.
//
// Providers are drivers, like the cache and storage drivers: one file each,
// behind Provider, enabled by SOCIAL_PROVIDERS. docs/guides/social-login.md
// has the recipe for adding one.
package social

import (
	"context"
	"net/http"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
)

// Proof is what the client brings back from the provider. Each provider
// reads the fields it needs: an OpenID Connect provider the id_token, a
// code provider the authorization code (and its PKCE verifier).
type Proof struct {
	IDToken      string
	Code         string
	CodeVerifier string
	RedirectURI  string
}

// Identity is who the provider says the user is.
type Identity struct {
	Email string
	// EmailVerified is the provider's word that the address belongs to the
	// person who signed in. An unverified address is never used to sign in.
	EmailVerified bool
}

// Provider is one outside sign-in provider.
type Provider interface {
	Name() string        // "google": the {provider} in the URL
	DisplayName() string // "Google": for the login screen
	// Verify checks the proof with the provider. It returns an *apperr.Error:
	// INVALID_CREDENTIALS for a proof the provider rejects, VALIDATION_FAILED
	// for a missing field, SERVICE_UNAVAILABLE when the provider cannot be
	// reached.
	Verify(ctx context.Context, proof Proof) (*Identity, error)
}

// Registry holds the enabled providers in display order. A nil *Registry
// has none.
type Registry struct {
	providers []Provider
}

// New builds the providers SOCIAL_PROVIDERS enables (config.loadSocial has
// already checked their names and credentials).
func New(cfg config.Social) *Registry {
	registry := &Registry{}
	for _, name := range cfg.Providers {
		switch name {
		case "google":
			registry.providers = append(registry.providers, newGoogle(cfg.Google))
		case "github":
			registry.providers = append(registry.providers, newGitHub(cfg.GitHub))
		}
	}
	return registry
}

// NewRegistry holds the given providers (tests use it with fakes).
func NewRegistry(providers ...Provider) *Registry {
	return &Registry{providers: providers}
}

// Get returns the enabled provider with this name.
func (r *Registry) Get(name string) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	for _, provider := range r.providers {
		if provider.Name() == name {
			return provider, true
		}
	}
	return nil, false
}

// List returns the enabled providers in display order.
func (r *Registry) List() []Provider {
	if r == nil {
		return nil
	}
	return r.providers
}

// httpClient calls the providers. A sign-in waits on it, so it gives up
// quickly.
var httpClient = &http.Client{Timeout: 10 * time.Second, Transport: tracing.Transport(nil)}
