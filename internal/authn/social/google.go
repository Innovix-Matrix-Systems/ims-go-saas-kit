package social

import "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"

// Google: the client signs in with Google Identity Services (web) or Google
// Sign-In (iOS, Android) and sends the id_token it gets.
const (
	googleIssuer  = "https://accounts.google.com"
	googleKeysURL = "https://www.googleapis.com/oauth2/v3/certs"
)

func newGoogle(cfg config.GoogleLogin) Provider {
	return newOIDCProvider("google", "Google", googleIssuer, googleKeysURL, cfg.ClientIDs)
}
