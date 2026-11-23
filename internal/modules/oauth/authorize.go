package oauth

import (
	"context"
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/mcp"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/i18n"
)

//go:embed templates/pages.html
var pagesFS embed.FS

const (
	flowCookie = "ims_oauth_flow"
	flowTTL    = 10 * time.Minute
)

// flow is one authorization in progress, kept in the cache under a random
// id the browser holds in a cookie.
type flow struct {
	ClientID     string `json:"client_id"`
	RedirectURI  string `json:"redirect_uri"`
	State        string `json:"state"`
	Challenge    string `json:"challenge"`
	Resource     string `json:"resource"`
	WantWrite    bool   `json:"want_write"`
	CSRF         string `json:"csrf"`
	PendingEmail string `json:"pending_email,omitempty"` // waiting for an OTP
	UserID       int64  `json:"user_id,omitempty"`       // signed in
}

// Pages serves the browser side of the authorization endpoint: sign-in, OTP
// and consent. They are plain server-rendered forms with no JavaScript.
type Pages struct {
	service  *Service
	signIn   *authn.Service
	users    authn.UserLookup
	cache    cache.Store
	product  string
	resetURL string
	secure   bool
	logger   *slog.Logger
	pages    map[string]*template.Template
}

// NewPages creates the pages. product is the name shown on them.
func NewPages(service *Service, signIn *authn.Service, users authn.UserLookup, store cache.Store,
	product, resetURL string, logger *slog.Logger) (*Pages, error) {
	base, err := template.ParseFS(pagesFS, "templates/pages.html")
	if err != nil {
		return nil, err
	}
	pages := map[string]*template.Template{}
	for _, name := range []string{"error", "signin", "otp", "consent"} {
		page, err := base.Clone()
		if err != nil {
			return nil, err
		}
		if _, err := page.Parse(`{{define "content"}}{{template "` + name + `_content" .}}{{end}}`); err != nil {
			return nil, err
		}
		pages[name] = page
	}
	return &Pages{
		service: service, signIn: signIn, users: users, cache: store, product: product,
		resetURL: resetURL, secure: strings.HasPrefix(service.Settings().Issuer, "https://"),
		logger: logger, pages: pages,
	}, nil
}

// view is what every page template sees.
type view struct {
	ctx           context.Context
	Lang          string
	Product       string
	Heading       string
	Error         string
	Message       string
	CSRF          string
	Email         string
	ResetURL      string
	ClientParams  map[string]any
	ConsentParams map[string]any
	ClientID      string
	RedirectHost  string
	LoopbackOnly  bool
	OfferWrite    bool
	PickTenant    bool
	Tenants       []ConsentTenant
}

// T translates for the page's locale.
func (v view) T(key, fallback string, params ...map[string]any) string {
	var data map[string]any
	if len(params) > 0 {
		data = params[0]
	}
	return i18n.TOr(v.ctx, key, fallback, data)
}

func (p *Pages) newView(r *http.Request, heading string) view {
	return view{ctx: r.Context(), Lang: string(i18n.FromContext(r.Context())), Product: p.product, Heading: heading}
}

func (p *Pages) render(w http.ResponseWriter, status int, page string, v view) {
	pageHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := p.pages[page].ExecuteTemplate(w, "layout", v); err != nil && p.logger != nil {
		p.logger.Error("oauth page render failed", "page", page, "error", err)
	}
}

// fail shows an error page. It never redirects: used when the client or its
// redirect URI cannot be trusted.
func (p *Pages) fail(w http.ResponseWriter, r *http.Request, status int, message string) {
	v := p.newView(r, i18n.TOr(r.Context(), "oauth.error_title", "This connection can't continue", nil))
	v.Message = message
	p.render(w, status, "error", v)
}

func pageHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Pragma", "no-cache")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
	h.Set("Referrer-Policy", "no-referrer")
}

// Authorize is GET /oauth/authorize: check the request, then show sign-in.
func (p *Pages) Authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	client, err := p.service.ResolveClient(r.Context(), q.Get("client_id"))
	if err != nil {
		if clientError(err) {
			p.fail(w, r, http.StatusBadRequest, err.Error())
			return
		}
		p.fail(w, r, http.StatusServiceUnavailable, "The server could not check this app. Try again in a moment.")
		return
	}
	redirectURI := q.Get("redirect_uri")
	if redirectURI == "" && len(client.RedirectURIs) == 1 {
		redirectURI = client.RedirectURIs[0]
	}
	if redirectURI == "" || !client.RedirectAllowed(redirectURI) {
		p.fail(w, r, http.StatusBadRequest, "The app asked to return to an address it has not declared ("+redirectURI+").")
		return
	}

	// From here on, problems go back to the client.
	state := q.Get("state")
	back := func(code, description string) {
		p.redirect(w, r, redirectURI, url.Values{"error": {code}, "error_description": {description}, "state": {state}})
	}
	if q.Get("response_type") != "code" {
		back("unsupported_response_type", "Only response_type=code is supported.")
		return
	}
	challenge := q.Get("code_challenge")
	if challenge == "" || q.Get("code_challenge_method") != "S256" {
		back("invalid_request", "PKCE with code_challenge_method=S256 is required.")
		return
	}
	resource := p.service.Settings().Resource
	if raw := q.Get("resource"); raw != "" && canonical(raw) != resource {
		back("invalid_target", "This server issues tokens only for "+resource+".")
		return
	}
	wantWrite := false
	for _, scope := range strings.Fields(q.Get("scope")) {
		switch scope {
		case mcp.ScopeRead, "offline_access":
		case mcp.ScopeWrite:
			wantWrite = true
		default:
			back("invalid_scope", "Unknown scope "+scope+".")
			return
		}
	}
	if client.Registered && !contains(client.MaxScopes, mcp.ScopeWrite) {
		wantWrite = false
	}

	csrf, err := randomToken()
	id, err2 := randomToken()
	if err != nil || err2 != nil {
		p.fail(w, r, http.StatusInternalServerError, "Something went wrong. Try again.")
		return
	}
	f := flow{ClientID: client.ID, RedirectURI: redirectURI, State: state, Challenge: challenge,
		Resource: resource, WantWrite: wantWrite, CSRF: csrf}
	if err := p.cache.Set(r.Context(), "oauth_flow:"+id, f, flowTTL); err != nil {
		p.fail(w, r, http.StatusServiceUnavailable, "The server could not start the sign-in. Try again in a moment.")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: flowCookie, Value: id, Path: "/oauth", MaxAge: int(flowTTL.Seconds()),
		HttpOnly: true, Secure: p.secure, SameSite: http.SameSiteLaxMode,
	})
	p.showSignIn(w, r, f, client, "", "")
}

func (p *Pages) showSignIn(w http.ResponseWriter, r *http.Request, f flow, client *Client, email, problem string) {
	v := p.newView(r, i18n.TOr(r.Context(), "oauth.signin_title", "Sign in", nil))
	v.CSRF, v.Email, v.Error, v.ResetURL = f.CSRF, email, problem, p.resetURL
	v.ClientParams = map[string]any{"client": client.Name}
	status := http.StatusOK
	if problem != "" {
		status = http.StatusUnauthorized
	}
	p.render(w, status, "signin", v)
}

// Login is POST /oauth/authorize/login: the password, then the OTP when the
// account needs one. The same throttles and account checks as the API login.
func (p *Pages) Login(w http.ResponseWriter, r *http.Request) {
	id, f, client, ok := p.loadFlow(w, r)
	if !ok {
		return
	}
	ip := httpx.ClientIP(r.Context())

	var user *authn.UserRecord
	if f.PendingEmail != "" {
		record, err := p.signIn.SignInOTP(r.Context(), f.PendingEmail, strings.TrimSpace(r.PostFormValue("code")), ip)
		if err != nil {
			v := p.newView(r, i18n.TOr(r.Context(), "oauth.otp_title", "Enter your code", nil))
			v.CSRF, v.Error = f.CSRF, signInProblem(r.Context(), err)
			p.render(w, http.StatusUnauthorized, "otp", v)
			return
		}
		user = record
	} else {
		email := strings.TrimSpace(r.PostFormValue("email"))
		record, otp, err := p.signIn.SignIn(r.Context(), email, r.PostFormValue("password"), ip)
		if err != nil {
			p.showSignIn(w, r, f, client, email, signInProblem(r.Context(), err))
			return
		}
		if otp {
			f.PendingEmail = email
			p.saveFlow(r.Context(), id, f)
			v := p.newView(r, i18n.TOr(r.Context(), "oauth.otp_title", "Enter your code", nil))
			v.CSRF = f.CSRF
			p.render(w, http.StatusOK, "otp", v)
			return
		}
		user = record
	}

	if client.TenantID != 0 && user.TenantID != client.TenantID {
		p.dropFlow(w, r, id)
		p.fail(w, r, http.StatusForbidden, "This app is available only to one organisation's users, and your account is not one of them.")
		return
	}
	f.PendingEmail = ""
	f.UserID = user.ID
	p.saveFlow(r.Context(), id, f)
	p.showConsent(w, r, f, client, user)
}

func (p *Pages) showConsent(w http.ResponseWriter, r *http.Request, f flow, client *Client, user *authn.UserRecord) {
	v := p.newView(r, i18n.TOr(r.Context(), "oauth.consent_title", "Allow access?", nil))
	v.CSRF = f.CSRF
	v.ConsentParams = map[string]any{"client": client.Name, "user": user.Name}
	v.ClientID = client.ID
	v.LoopbackOnly = client.LoopbackOnly()
	v.OfferWrite = f.WantWrite
	if u, err := url.Parse(f.RedirectURI); err == nil {
		v.RedirectHost = u.Hostname()
		if isLoopback(u.Hostname()) {
			v.RedirectHost = i18n.TOr(r.Context(), "oauth.this_computer", "an app on this computer", nil)
		}
	}
	if user.TenantID == 0 {
		v.PickTenant = true
		tenants, err := p.service.ConsentTenants(r.Context())
		if err == nil {
			v.Tenants = tenants
		}
	}
	p.render(w, http.StatusOK, "consent", v)
}

// Consent is POST /oauth/authorize/consent: Allow issues a code and sends the
// browser back to the client; Deny sends it back with access_denied.
func (p *Pages) Consent(w http.ResponseWriter, r *http.Request) {
	id, f, client, ok := p.loadFlow(w, r)
	if !ok {
		return
	}
	if f.UserID == 0 {
		p.showSignIn(w, r, f, client, "", "")
		return
	}
	p.dropFlow(w, r, id)

	if r.PostFormValue("decision") != "allow" {
		p.redirect(w, r, f.RedirectURI, url.Values{"error": {"access_denied"},
			"error_description": {"The user denied access."}, "state": {f.State}})
		return
	}
	user, err := p.users.FindByID(r.Context(), f.UserID)
	if err != nil || !user.IsActive {
		p.fail(w, r, http.StatusForbidden, "Your account is not active.")
		return
	}

	approval := Approval{
		UserID: user.ID, UserTenantID: user.TenantID, Client: client, RedirectURI: f.RedirectURI,
		Scopes:        GrantScopes(client, f.WantWrite && r.PostFormValue("allow_write") == "1"),
		CodeChallenge: f.Challenge, Resource: f.Resource,
	}
	if user.TenantID == 0 {
		if tenantID, _ := strconv.ParseInt(r.PostFormValue("tenant_id"), 10, 64); tenantID > 0 {
			if !p.service.TenantPickable(r.Context(), tenantID) {
				p.fail(w, r, http.StatusBadRequest, "That tenant can't be picked.")
				return
			}
			branchID, err := p.service.FirstBranch(r.Context(), tenantID)
			if err != nil {
				p.fail(w, r, http.StatusBadRequest, "That tenant has no branch to work in.")
				return
			}
			approval.TenantID, approval.BranchID = tenantID, branchID
		}
	}

	code, err := p.service.Approve(r.Context(), approval)
	if err != nil {
		p.redirect(w, r, f.RedirectURI, url.Values{"error": {"server_error"}, "state": {f.State}})
		return
	}
	p.redirect(w, r, f.RedirectURI, url.Values{"code": {code}, "state": {f.State}})
}

// loadFlow finds the browser's authorization in progress and checks the
// form's CSRF token against it.
func (p *Pages) loadFlow(w http.ResponseWriter, r *http.Request) (string, flow, *Client, bool) {
	expired := func() {
		p.fail(w, r, http.StatusBadRequest, i18n.TOr(r.Context(), "oauth.expired",
			"This sign-in has expired. Go back to the app and connect again.", nil))
	}
	cookie, err := r.Cookie(flowCookie)
	if err != nil || cookie.Value == "" {
		expired()
		return "", flow{}, nil, false
	}
	var f flow
	if found, err := p.cache.Get(r.Context(), "oauth_flow:"+cookie.Value, &f); err != nil || !found {
		expired()
		return "", flow{}, nil, false
	}
	if r.PostFormValue("csrf") == "" || r.PostFormValue("csrf") != f.CSRF {
		p.fail(w, r, http.StatusForbidden, "The form could not be verified. Go back to the app and connect again.")
		return "", flow{}, nil, false
	}
	client, err := p.service.ResolveClient(r.Context(), f.ClientID)
	if err != nil || !client.RedirectAllowed(f.RedirectURI) {
		p.fail(w, r, http.StatusBadRequest, "This app can no longer connect.")
		return "", flow{}, nil, false
	}
	return cookie.Value, f, client, true
}

func (p *Pages) saveFlow(ctx context.Context, id string, f flow) {
	_ = p.cache.Set(ctx, "oauth_flow:"+id, f, flowTTL)
}

func (p *Pages) dropFlow(w http.ResponseWriter, r *http.Request, id string) {
	_ = p.cache.Delete(r.Context(), "oauth_flow:"+id)
	http.SetCookie(w, &http.Cookie{Name: flowCookie, Value: "", Path: "/oauth", MaxAge: -1,
		HttpOnly: true, Secure: p.secure, SameSite: http.SameSiteLaxMode})
}

// redirect sends the browser back to the client with params and the issuer
// (RFC 9207), keeping the redirect URI's own query.
func (p *Pages) redirect(w http.ResponseWriter, r *http.Request, redirectURI string, params url.Values) {
	target, err := url.Parse(redirectURI)
	if err != nil {
		p.fail(w, r, http.StatusBadRequest, "The app's return address is not valid.")
		return
	}
	query := target.Query()
	for key, values := range params {
		if len(values) > 0 && values[0] != "" {
			query.Set(key, values[0])
		}
	}
	query.Set("iss", p.service.Settings().Issuer)
	target.RawQuery = query.Encode()
	pageHeaders(w)
	http.Redirect(w, r, target.String(), http.StatusFound)
}

// signInProblem is the message the sign-in form shows for err.
func signInProblem(ctx context.Context, err error) string {
	var appErr *apperr.Error
	if e, ok := err.(*apperr.Error); ok {
		appErr = e
	}
	if appErr == nil {
		return i18n.TOr(ctx, "oauth.signin_failed", "Sign-in failed. Try again.", nil)
	}
	switch appErr.Code {
	case apperr.InvalidCredentials:
		return i18n.TOr(ctx, "oauth.invalid_credentials", "The e-mail or password is not right.", nil)
	case apperr.AccountLocked, apperr.RateLimitExceeded:
		return i18n.TOr(ctx, "oauth.throttled", "Too many attempts. Wait a few minutes and try again.", nil)
	default:
		return appErr.Message
	}
}
