package social

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"golang.org/x/oauth2"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
)

type githubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

// fakeGitHub answers the token exchange and /user/emails like GitHub does.
func fakeGitHub(t *testing.T, emailsStatus int, emails []githubEmail) (*githubProvider, url.Values) {
	t.Helper()
	exchanged := url.Values{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			_ = r.ParseForm()
			for k, v := range r.PostForm {
				exchanged[k] = v
			}
			w.Header().Set("Content-Type", "application/json")
			if r.PostForm.Get("code") != "good-code" {
				// GitHub answers a bad code with 200 and an error body.
				_, _ = w.Write([]byte(`{"error":"bad_verification_code","error_description":"The code passed is incorrect or expired."}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"gho_test","token_type":"bearer","scope":"user:email"}`))
		case "/user/emails":
			if r.Header.Get("Authorization") != "Bearer gho_test" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(emailsStatus)
			_ = json.NewEncoder(w).Encode(emails)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	provider := &githubProvider{
		oauth: &oauth2.Config{
			ClientID: "client", ClientSecret: "secret",
			Endpoint: oauth2.Endpoint{TokenURL: server.URL + "/login/oauth/access_token"},
		},
		apiURL: server.URL,
	}
	return provider, exchanged
}

func TestGitHubProviderVerify(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	tests := []struct {
		name     string
		code     string
		status   int
		emails   []githubEmail
		want     *Identity
		wantCode apperr.Code
	}{
		{name: "primary verified address", code: "good-code", status: http.StatusOK,
			emails: []githubEmail{{Email: "old@example.com", Verified: true}, {Email: "ada@example.com", Primary: true, Verified: true}},
			want:   &Identity{Email: "ada@example.com", EmailVerified: true}},
		{name: "primary address not verified", code: "good-code", status: http.StatusOK,
			emails: []githubEmail{{Email: "ada@example.com", Primary: true}},
			want:   &Identity{Email: "ada@example.com"}},
		{name: "no primary address", code: "good-code", status: http.StatusOK, want: &Identity{}},
		{name: "code rejected", code: "stale-code", wantCode: apperr.InvalidCredentials},
		{name: "user:email scope not granted", code: "good-code", status: http.StatusForbidden, wantCode: apperr.InvalidCredentials},
		{name: "GitHub failing", code: "good-code", status: http.StatusBadGateway, wantCode: apperr.ServiceUnavailable},
		{name: "missing code", wantCode: apperr.ValidationFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, exchanged := fakeGitHub(t, tt.status, tt.emails)
			got, err := provider.Verify(context.Background(), Proof{
				Code: tt.code, CodeVerifier: verifier, RedirectURI: "https://app.example.com/auth/github",
			})
			if tt.wantCode != "" {
				if !apperr.Is(err, tt.wantCode) {
					t.Fatalf("err = %v, want %s", err, tt.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if *got != *tt.want {
				t.Fatalf("identity = %+v, want %+v", *got, *tt.want)
			}
			if exchanged.Get("code_verifier") != verifier {
				t.Errorf("code_verifier = %q, want the client's verifier", exchanged.Get("code_verifier"))
			}
			if exchanged.Get("redirect_uri") != "https://app.example.com/auth/github" {
				t.Errorf("redirect_uri = %q, want the client's", exchanged.Get("redirect_uri"))
			}
		})
	}
}
