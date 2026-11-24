package social

import (
	"context"
	"errors"

	"golang.org/x/oauth2"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
)

// exchangeCode trades the authorization code the client got from the
// provider's redirect for the provider's access token: the first step of
// every code provider (GitHub; Facebook or GitLab when added). The client
// ran the redirect, so it sends the PKCE verifier and the redirect_uri it
// used; the provider checks both.
func exchangeCode(ctx context.Context, cfg *oauth2.Config, displayName string, proof Proof) (*oauth2.Token, error) {
	if proof.Code == "" {
		return nil, apperr.Validation(map[string][]string{"code": {"The code field is required."}})
	}
	var options []oauth2.AuthCodeOption
	if proof.CodeVerifier != "" {
		options = append(options, oauth2.VerifierOption(proof.CodeVerifier))
	}
	if proof.RedirectURI != "" {
		options = append(options, oauth2.SetAuthURLParam("redirect_uri", proof.RedirectURI))
	}

	token, err := cfg.Exchange(context.WithValue(ctx, oauth2.HTTPClient, httpClient), proof.Code, options...)
	if err != nil {
		var rejected *oauth2.RetrieveError
		if errors.As(err, &rejected) {
			return nil, apperr.Wrap(apperr.InvalidCredentials, "The "+displayName+" sign-in could not be verified.", err)
		}
		return nil, apperr.Wrap(apperr.ServiceUnavailable, displayName+" could not be reached. Please try again.", err)
	}
	return token, nil
}
