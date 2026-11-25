package social

import (
	"context"
	"encoding/json"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
)

// githubProvider signs in with a GitHub OAuth app. GitHub has no id_token:
// the client sends the code from GitHub's redirect (requested with the
// user:email scope), we exchange it with our client secret and read the
// account's primary e-mail address.
type githubProvider struct {
	oauth  *oauth2.Config
	apiURL string
}

func newGitHub(cfg config.GitHubLogin) Provider {
	return &githubProvider{
		oauth:  &oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, Endpoint: endpoints.GitHub},
		apiURL: "https://api.github.com",
	}
}

func (p *githubProvider) Name() string        { return "github" }
func (p *githubProvider) DisplayName() string { return "GitHub" }

func (p *githubProvider) Verify(ctx context.Context, proof Proof) (*Identity, error) {
	token, err := exchangeCode(ctx, p.oauth, p.DisplayName(), proof)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.apiURL+"/user/emails", nil)
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "build GitHub request", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+token.AccessToken)
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, apperr.Wrap(apperr.ServiceUnavailable, "GitHub could not be reached. Please try again.", err)
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return nil, apperr.New(apperr.InvalidCredentials, "GitHub did not share the e-mail address (the user:email scope is required).")
	case response.StatusCode != http.StatusOK:
		return nil, apperr.Newf(apperr.ServiceUnavailable, "GitHub answered %d. Please try again.", response.StatusCode)
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.NewDecoder(response.Body).Decode(&emails); err != nil {
		return nil, apperr.Wrap(apperr.ServiceUnavailable, "GitHub sent an unreadable answer. Please try again.", err)
	}
	for _, email := range emails {
		if email.Primary {
			return &Identity{Email: email.Email, EmailVerified: email.Verified}, nil
		}
	}
	return &Identity{}, nil
}
