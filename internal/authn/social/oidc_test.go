package social

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
)

// fakeIssuer is an OpenID Connect provider in a test: it publishes one
// signing key and signs the id_tokens the test asks for.
type fakeIssuer struct {
	url string
	key *rsa.PrivateKey
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	keys := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(keys)
	}))
	t.Cleanup(server.Close)
	return &fakeIssuer{url: server.URL, key: key}
}

func (f *fakeIssuer) sign(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "test"}}, nil)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	payload, _ := json.Marshal(claims)
	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	raw, err := signed.CompactSerialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return raw
}

func (f *fakeIssuer) claims(overrides map[string]any) map[string]any {
	claims := map[string]any{
		"iss":            f.url,
		"aud":            "ios-client",
		"sub":            "1234",
		"email":          "ada@example.com",
		"email_verified": true,
		"iat":            time.Now().Unix(),
		"exp":            time.Now().Add(time.Hour).Unix(),
	}
	for k, v := range overrides {
		claims[k] = v
	}
	return claims
}

func TestOIDCProviderVerify(t *testing.T) {
	issuer := newFakeIssuer(t)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := newOIDCProvider("google", "Google", issuer.url, issuer.url, []string{"web-client", "ios-client"})

	tests := []struct {
		name     string
		token    string
		want     *Identity
		wantCode apperr.Code
	}{
		{name: "valid, issued to a mobile client id", token: issuer.sign(t, issuer.key, issuer.claims(nil)),
			want: &Identity{Email: "ada@example.com", EmailVerified: true}},
		{name: "unverified e-mail is reported, not refused", token: issuer.sign(t, issuer.key, issuer.claims(map[string]any{"email_verified": false})),
			want: &Identity{Email: "ada@example.com"}},
		{name: "issued to another app", token: issuer.sign(t, issuer.key, issuer.claims(map[string]any{"aud": "someone-else"})),
			wantCode: apperr.InvalidCredentials},
		{name: "expired", token: issuer.sign(t, issuer.key, issuer.claims(map[string]any{"exp": time.Now().Add(-time.Minute).Unix()})),
			wantCode: apperr.InvalidCredentials},
		{name: "another issuer", token: issuer.sign(t, issuer.key, issuer.claims(map[string]any{"iss": "https://evil.example"})),
			wantCode: apperr.InvalidCredentials},
		{name: "signed with a key the provider never published", token: issuer.sign(t, otherKey, issuer.claims(nil)),
			wantCode: apperr.InvalidCredentials},
		{name: "not a token", token: "garbage", wantCode: apperr.InvalidCredentials},
		{name: "missing", token: "", wantCode: apperr.ValidationFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := provider.Verify(context.Background(), Proof{IDToken: tt.token})
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
		})
	}
}
