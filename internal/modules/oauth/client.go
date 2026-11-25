package oauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
)

// Client is an OAuth client as the authorization server sees it, whether it
// identified itself with a Client ID Metadata Document or was registered by
// an admin.
type Client struct {
	ID           string // the client_id: an https URL (CIMD) or ims_… (registered)
	Name         string
	RedirectURIs []string
	// Registered is true for a client from oauth_clients.
	Registered   bool
	Confidential bool
	// MaxScopes caps what a registered client may be granted.
	MaxScopes []string
	// TenantID limits a registered client to one tenant's users (0 = any).
	TenantID int64

	secretHash         string
	previousSecretHash string
	previousExpires    time.Time
}

// errClient is a client_id or redirect_uri problem: the authorization
// endpoint must show it, never redirect with it.
type errClient struct{ msg string }

func (e errClient) Error() string { return e.msg }

// clientError reports whether err is about the client itself.
func clientError(err error) bool {
	var e errClient
	return errors.As(err, &e)
}

// Clients resolves client_ids.
type Clients struct {
	repo         Repository
	cache        cache.Store
	cimd         bool
	allowedHosts []string
	http         *http.Client
	// allowPrivate lets tests fetch documents from httptest servers.
	allowPrivate bool
}

// RegisteredPrefix starts every client_id an admin registers, so it can
// never be mistaken for a CIMD URL.
const RegisteredPrefix = "ims_"

const (
	cimdTimeout  = 5 * time.Second
	cimdMaxBytes = 64 << 10
	cimdMinCache = 5 * time.Minute
	cimdMaxCache = 24 * time.Hour
)

// NewClients creates the resolver.
func NewClients(repo Repository, store cache.Store, cimd bool, allowedHosts []string) *Clients {
	c := &Clients{repo: repo, cache: store, cimd: cimd, allowedHosts: allowedHosts}
	c.http = &http.Client{
		Timeout: cimdTimeout,
		// A metadata document must be served at its own URL.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: tracing.Transport(&http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: cimdTimeout, Control: c.dialControl}).DialContext,
			TLSHandshakeTimeout:   cimdTimeout,
			ResponseHeaderTimeout: cimdTimeout,
			MaxIdleConns:          4,
			IdleConnTimeout:       30 * time.Second,
		}),
	}
	return c
}

// Resolve returns the client for client_id.
func (c *Clients) Resolve(ctx context.Context, clientID string) (*Client, error) {
	clientID = strings.TrimSpace(clientID)
	switch {
	case clientID == "":
		return nil, errClient{"The request has no client_id."}
	case strings.HasPrefix(clientID, "https://"):
		if !c.cimd {
			return nil, errClient{"This server accepts only registered clients."}
		}
		return c.metadataDocument(ctx, clientID)
	case strings.HasPrefix(clientID, RegisteredPrefix):
		record, err := c.repo.FindClient(ctx, clientID)
		if apperr.Is(err, apperr.ResourceNotFound) {
			return nil, errClient{"This client is not registered."}
		}
		if err != nil {
			return nil, err
		}
		if !record.IsActive {
			return nil, errClient{"This client has been deactivated."}
		}
		return registered(*record), nil
	default:
		return nil, errClient{"The client_id is not valid."}
	}
}

func registered(record ClientRecord) *Client {
	client := &Client{
		ID:           record.ClientID,
		Name:         record.Name,
		RedirectURIs: record.RedirectURIs,
		Registered:   true,
		Confidential: record.Type == TypeConfidential,
		MaxScopes:    record.Scopes,
		TenantID:     record.TenantID,
		secretHash:   record.SecretHash,
	}
	if record.PreviousSecretHash != "" && record.PreviousSecretExpiresAt != nil {
		client.previousSecretHash = record.PreviousSecretHash
		client.previousExpires = *record.PreviousSecretExpiresAt
	}
	return client
}

// CheckSecret verifies a confidential client's secret (the current one, or
// the previous one during its grace period).
func (c *Client) CheckSecret(secret string, now time.Time) bool {
	if !c.Confidential || secret == "" {
		return false
	}
	sum := hashSecret(secret)
	if subtle.ConstantTimeCompare([]byte(sum), []byte(c.secretHash)) == 1 {
		return true
	}
	return c.previousSecretHash != "" && now.Before(c.previousExpires) &&
		subtle.ConstantTimeCompare([]byte(sum), []byte(c.previousSecretHash)) == 1
}

// RedirectAllowed reports whether uri is one of the client's redirect URIs.
// Loopback redirects (http://localhost, http://127.0.0.1, http://[::1]) match
// on any port, as native apps pick one at run time (RFC 8252 §7.3).
func (c *Client) RedirectAllowed(uri string) bool {
	for _, allowed := range c.RedirectURIs {
		if uri == allowed {
			return true
		}
		if loopbackEqual(allowed, uri) {
			return true
		}
	}
	return false
}

// LoopbackOnly reports whether every redirect URI is on this computer: the
// consent page then warns that an app here, not a website, will connect.
func (c *Client) LoopbackOnly() bool {
	if len(c.RedirectURIs) == 0 {
		return false
	}
	for _, uri := range c.RedirectURIs {
		if u, err := url.Parse(uri); err != nil || !isLoopback(u.Hostname()) {
			return false
		}
	}
	return true
}

func loopbackEqual(allowed, got string) bool {
	a, err1 := url.Parse(allowed)
	g, err2 := url.Parse(got)
	if err1 != nil || err2 != nil {
		return false
	}
	// localhost, 127.0.0.1 and ::1 all mean this computer: tools differ in
	// which one they put in their redirect (mcp-remote uses 127.0.0.1).
	return a.Scheme == "http" && g.Scheme == "http" &&
		isLoopback(a.Hostname()) && isLoopback(g.Hostname()) &&
		a.Path == g.Path && a.RawQuery == g.RawQuery && g.Fragment == ""
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// metadataDocument is a client's CIMD: fetched over https from its client_id,
// validated, and cached by its Cache-Control (clamped to 5 min – 24 h).
type metadataDocument struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

func (c *Clients) metadataDocument(ctx context.Context, clientID string) (*Client, error) {
	u, err := url.Parse(clientID)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path == "" || u.Path == "/" ||
		u.Fragment != "" || u.User != nil {
		return nil, errClient{"The client_id must be an https URL with a path."}
	}
	if u.Port() != "" && u.Port() != "443" && !c.allowPrivate {
		return nil, errClient{"The client_id must use the default https port."}
	}
	if len(c.allowedHosts) > 0 && !contains(c.allowedHosts, strings.ToLower(u.Hostname())) {
		return nil, errClient{"This server does not accept clients from " + u.Hostname() + "."}
	}

	key := "oauth_cimd:" + hashSecret(clientID)
	if c.cache != nil {
		var doc metadataDocument
		if ok, _ := c.cache.Get(ctx, key, &doc); ok && doc.ClientID == clientID {
			return &Client{ID: doc.ClientID, Name: doc.ClientName, RedirectURIs: doc.RedirectURIs}, nil
		}
	}

	doc, ttl, err := c.fetch(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if c.cache != nil {
		_ = c.cache.Set(ctx, key, doc, ttl)
	}
	return &Client{ID: doc.ClientID, Name: doc.ClientName, RedirectURIs: doc.RedirectURIs}, nil
}

func (c *Clients) fetch(ctx context.Context, clientID string) (*metadataDocument, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return nil, 0, errClient{"The client_id is not a valid URL."}
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, errClient{"The client's metadata document could not be fetched."}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, errClient{fmt.Sprintf("The client's metadata document answered %d.", resp.StatusCode)}
	}
	if mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mediaType != "application/json" {
		return nil, 0, errClient{"The client's metadata document is not JSON."}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, cimdMaxBytes+1))
	if err != nil || len(body) > cimdMaxBytes {
		return nil, 0, errClient{"The client's metadata document is too large."}
	}

	var doc metadataDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, 0, errClient{"The client's metadata document is not valid JSON."}
	}
	switch {
	case doc.ClientID != clientID:
		return nil, 0, errClient{"The client's metadata document names a different client_id."}
	case strings.TrimSpace(doc.ClientName) == "":
		return nil, 0, errClient{"The client's metadata document has no client_name."}
	case len(doc.RedirectURIs) == 0:
		return nil, 0, errClient{"The client's metadata document has no redirect_uris."}
	case doc.TokenEndpointAuthMethod != "" && doc.TokenEndpointAuthMethod != "none":
		return nil, 0, errClient{"Only public clients (token_endpoint_auth_method none) can connect without registration."}
	}
	for _, uri := range doc.RedirectURIs {
		if err := validRedirect(uri); err != nil {
			return nil, 0, errClient{"The client's metadata document has an invalid redirect URI: " + err.Error()}
		}
	}
	doc.ClientName = strings.TrimSpace(doc.ClientName)
	return &doc, cacheTTL(resp.Header.Get("Cache-Control")), nil
}

// validRedirect accepts https URLs and http loopback URLs, without fragments.
func validRedirect(uri string) error {
	u, err := url.Parse(uri)
	switch {
	case err != nil || u.Host == "":
		return errors.New("not an absolute URL")
	case u.Fragment != "":
		return errors.New("has a fragment")
	case u.Scheme == "https":
		return nil
	case u.Scheme == "http" && isLoopback(u.Hostname()):
		return nil
	default:
		return errors.New("must be https, or http on localhost")
	}
}

func cacheTTL(header string) time.Duration {
	ttl := cimdMinCache
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(strings.ToLower(part))
		if strings.HasPrefix(part, "max-age=") {
			if secs, err := strconv.Atoi(strings.TrimPrefix(part, "max-age=")); err == nil {
				ttl = time.Duration(secs) * time.Second
			}
		}
	}
	if ttl < cimdMinCache {
		return cimdMinCache
	}
	if ttl > cimdMaxCache {
		return cimdMaxCache
	}
	return ttl
}

// dialControl refuses to connect to anything that is not a public address,
// after DNS resolution: loopback, private, link-local (cloud metadata),
// multicast and unspecified addresses are all out (SSRF).
func (c *Clients) dialControl(_, address string, _ syscall.RawConn) error {
	if c.allowPrivate {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !publicIP(ip) {
		return fmt.Errorf("refusing to fetch client metadata from %s", host)
	}
	return nil
}

func publicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	// Carrier-grade NAT (100.64.0.0/10) and the IPv4-mapped forms of the
	// ranges above.
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 100 && v4[1]&0xc0 == 64 {
			return false
		}
		if v4[0] == 0 {
			return false
		}
	}
	return true
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
