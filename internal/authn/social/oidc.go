package social

import (
	"context"
	"errors"
	"slices"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
)

// oidcProvider verifies an OpenID Connect id_token, the proof Google (and
// Microsoft and Apple, when added) hands a client: the signature against the
// provider's published keys, the issuer, the expiry, and that the token was
// issued to one of our client ids.
type oidcProvider struct {
	name        string
	displayName string
	clientIDs   []string
	verifier    *oidc.IDTokenVerifier
}

// newOIDCProvider needs no network at startup: the keys are fetched on the
// first sign-in and cached, and refetched when the provider rotates them.
func newOIDCProvider(name, displayName, issuer, keysURL string, clientIDs []string) *oidcProvider {
	keys := oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), httpClient), keysURL)
	// The audience is checked below against every client id (web, iOS,
	// Android); go-oidc checks only one.
	verifier := oidc.NewVerifier(issuer, keys, &oidc.Config{SkipClientIDCheck: true})
	return &oidcProvider{name: name, displayName: displayName, clientIDs: clientIDs, verifier: verifier}
}

func (p *oidcProvider) Name() string        { return p.name }
func (p *oidcProvider) DisplayName() string { return p.displayName }

func (p *oidcProvider) Verify(ctx context.Context, proof Proof) (*Identity, error) {
	if proof.IDToken == "" {
		return nil, apperr.Validation(map[string][]string{"id_token": {"The id_token field is required."}})
	}
	token, err := p.verifier.Verify(ctx, proof.IDToken)
	if err != nil {
		var expired *oidc.TokenExpiredError
		if errors.As(err, &expired) {
			return nil, apperr.Wrap(apperr.InvalidCredentials, "The "+p.displayName+" sign-in has expired. Please sign in again.", err)
		}
		return nil, apperr.Wrap(apperr.InvalidCredentials, "The "+p.displayName+" sign-in could not be verified.", err)
	}
	if !slices.ContainsFunc(token.Audience, func(aud string) bool { return slices.Contains(p.clientIDs, aud) }) {
		return nil, apperr.New(apperr.InvalidCredentials, "The "+p.displayName+" sign-in was made for another app.")
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := token.Claims(&claims); err != nil {
		return nil, apperr.Wrap(apperr.InvalidCredentials, "The "+p.displayName+" sign-in could not be read.", err)
	}
	return &Identity{Email: claims.Email, EmailVerified: claims.EmailVerified}, nil
}
