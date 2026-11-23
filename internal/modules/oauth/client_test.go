package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRedirectMatching(t *testing.T) {
	c := &Client{RedirectURIs: []string{"http://localhost/callback", "https://claude.ai/api/mcp/auth_callback"}}
	cases := map[string]bool{
		"http://localhost/callback":                    true,
		"http://localhost:53682/callback":              true, // loopback: any port
		"http://localhost:53682/other":                 false,
		"http://127.0.0.1:53682/callback":              true, // any loopback host: the same computer
		"http://[::1]:53682/callback":                  true,
		"https://claude.ai/api/mcp/auth_callback":      true,
		"https://claude.ai:8443/api/mcp/auth_callback": false, // non-loopback: exact only
		"https://claude.ai/api/mcp/auth_callback/x":    false,
	}
	for uri, want := range cases {
		if got := c.RedirectAllowed(uri); got != want {
			t.Errorf("%s = %v, want %v", uri, got, want)
		}
	}
	if c.LoopbackOnly() {
		t.Error("a client with an https redirect is not loopback-only")
	}
	if !(&Client{RedirectURIs: []string{"http://127.0.0.1/cb"}}).LoopbackOnly() {
		t.Error("loopback-only client not detected")
	}
}

func TestValidRedirect(t *testing.T) {
	for uri, ok := range map[string]bool{
		"https://example.com/cb": true, "http://localhost:1/cb": true, "http://example.com/cb": false,
		"https://example.com/cb#x": false, "/relative": false, "javascript:alert(1)": false,
	} {
		if err := validRedirect(uri); (err == nil) != ok {
			t.Errorf("%s: err = %v", uri, err)
		}
	}
}

func TestPublicIP(t *testing.T) {
	for addr, public := range map[string]bool{
		"8.8.8.8": true, "127.0.0.1": false, "10.1.2.3": false, "192.168.1.1": false, "169.254.169.254": false,
		"100.64.0.1": false, "0.0.0.0": false, "::1": false, "fd00::1": false, "2606:4700::1111": true,
	} {
		if got := publicIP(net.ParseIP(addr)); got != public {
			t.Errorf("%s = %v, want %v", addr, got, public)
		}
	}
}

func TestCacheTTLIsClamped(t *testing.T) {
	for header, want := range map[string]time.Duration{
		"": cimdMinCache, "max-age=60": cimdMinCache, "public, max-age=3600": time.Hour, "max-age=999999": cimdMaxCache,
	} {
		if got := cacheTTL(header); got != want {
			t.Errorf("%q = %v, want %v", header, got, want)
		}
	}
}

func TestPKCE(t *testing.T) {
	verifier := strings.Repeat("a", 43)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if !pkceMatches(verifier, challenge) {
		t.Error("matching verifier refused")
	}
	if pkceMatches(strings.Repeat("b", 43), challenge) || pkceMatches("short", challenge) {
		t.Error("wrong or short verifier accepted")
	}
}

func TestClientSecretWithGrace(t *testing.T) {
	now := time.Now()
	c := &Client{Confidential: true, secretHash: hashSecret("new"), previousSecretHash: hashSecret("old"), previousExpires: now.Add(time.Hour)}
	if !c.CheckSecret("new", now) || !c.CheckSecret("old", now) {
		t.Error("current or in-grace secret refused")
	}
	if c.CheckSecret("old", now.Add(2*time.Hour)) || c.CheckSecret("", now) || c.CheckSecret("other", now) {
		t.Error("expired, empty or wrong secret accepted")
	}
}

func TestMetadataDocumentFetch(t *testing.T) {
	var body, contentType string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	clients := NewClients(nil, nil, true, nil)
	clients.allowPrivate = true
	clients.http = srv.Client()
	id := srv.URL + "/client.json"

	contentType = "application/json"
	body = `{"client_id":"` + id + `","client_name":"Agent","redirect_uris":["http://localhost/callback"],"token_endpoint_auth_method":"none"}`
	client, err := clients.Resolve(context.Background(), id)
	if err != nil || client.Name != "Agent" || client.Registered {
		t.Fatalf("valid document: %+v, %v", client, err)
	}

	bad := map[string]string{
		"other client_id": `{"client_id":"https://elsewhere/x","client_name":"A","redirect_uris":["http://localhost/cb"]}`,
		"no name":         `{"client_id":"` + id + `","redirect_uris":["http://localhost/cb"]}`,
		"no redirects":    `{"client_id":"` + id + `","client_name":"A"}`,
		"http redirect":   `{"client_id":"` + id + `","client_name":"A","redirect_uris":["http://evil.example/cb"]}`,
		"confidential":    `{"client_id":"` + id + `","client_name":"A","redirect_uris":["http://localhost/cb"],"token_endpoint_auth_method":"client_secret_basic"}`,
		"too large":       `{"client_id":"` + id + `","client_name":"` + strings.Repeat("x", cimdMaxBytes) + `","redirect_uris":["http://localhost/cb"]}`,
		"not json":        `<html>`,
	}
	for name, doc := range bad {
		body = doc
		if _, err := clients.Resolve(context.Background(), id); err == nil || !clientError(err) {
			t.Errorf("%s: want a client error, got %v", name, err)
		}
	}
	contentType, body = "text/html", `{"client_id":"`+id+`","client_name":"A","redirect_uris":["http://localhost/cb"]}`
	if _, err := clients.Resolve(context.Background(), id); err == nil {
		t.Error("a non-JSON content type must be refused")
	}

	for _, id := range []string{"http://example.com/c.json", "https://example.com", "https://example.com/", "https://u:p@example.com/c"} {
		if _, err := clients.Resolve(context.Background(), id); err == nil {
			t.Errorf("%s must be refused", id)
		}
	}
	off := NewClients(nil, nil, false, nil)
	if _, err := off.Resolve(context.Background(), id); err == nil {
		t.Error("CIMD off must refuse URL client ids")
	}
	limited := NewClients(nil, nil, true, []string{"claude.ai"})
	if _, err := limited.Resolve(context.Background(), "https://other.example/c.json"); err == nil {
		t.Error("a host outside MCP_OAUTH_ALLOWED_CLIENT_HOSTS must be refused")
	}
}
