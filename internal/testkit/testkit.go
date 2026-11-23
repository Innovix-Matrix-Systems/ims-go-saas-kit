// Package testkit makes an integration test a few lines, like Laravel's
// feature tests:
//
//	func TestBranchCreate(t *testing.T) {
//		app := testkit.New(t)                       // migrated, seeded, routed
//		admin := app.User(t, testkit.Role("Admin")) // signed-in tenant admin
//		app.As(admin).Post("/api/v1/branch", `{"name":"Dhaka"}`).
//			Status(t, http.StatusCreated)
//	}
//
// Tests are skipped unless DATABASE_TEST_DSN points at a Postgres database;
// every test package gets its own schema (see dbtest). New truncates the
// application tables, so tests in one package must not run in parallel.
package testkit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/app"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	billingdomain "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database/dbtest"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/seeder"
)

// Password is the password of every account testkit creates (and of the
// seeded super admin and developer).
const Password = "secret123"

// DefaultTenant is the tenant db:seed creates.
const DefaultTenant int64 = 1

// App is the fully wired application on a fresh, seeded schema.
type App struct {
	Deps   *app.Dependencies
	Router http.Handler
	Pool   *pgxpool.Pool
	Config *config.Config

	plan  int64 // unlimited package every testkit tenant is subscribed to
	super *User

	mcpClient string // the client App.Agent connects with (registered once)
}

// New migrates (once per package), empties the application tables, seeds
// the default tenant, roles and accounts, and builds the router. Tenants it
// creates are on an unlimited plan so plan limits never get in the way.
func New(t *testing.T, options ...Option) *App {
	t.Helper()
	settings := newSettings{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, option := range options {
		option(&settings)
	}
	pool := dbtest.Pool(t) // skips the test without DATABASE_TEST_DSN
	t.Setenv("DATABASE_DSN", dbtest.DSN(t))
	t.Setenv("APP_ENV_FILE", "")
	// Uploads go to a temp dir, never into the package under test.
	t.Setenv("APP_STORAGE_DIR", t.TempDir())
	t.Setenv("BCRYPT_ROUNDS", "4")
	t.Setenv("AUTH_DEFAULT_PASSWORD", Password)
	if settings.mcp {
		t.Setenv("MCP_ENABLED", "true")
	}

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("testkit: config.Load: %v", err)
	}
	truncateAll(t, pool)

	logger := settings.logger
	deps, cleanup, err := app.NewDependencies(cfg, logger)
	if err != nil {
		t.Fatalf("testkit: NewDependencies: %v", err)
	}
	t.Cleanup(cleanup)

	ctx := context.Background()
	if err := seeder.Run(ctx, cfg, deps.Tx.Queries(), deps.Tx, deps.Checker, logger); err != nil {
		t.Fatalf("testkit: seed: %v", err)
	}
	plan, err := deps.Platform.BillingPackages.Create(ctx, billingdomain.PackageInput{
		Name: "Testkit Unlimited", Price: 0, BillingCycle: billingdomain.CycleMonthly, TrialDays: 365, IsActive: true,
	}, []billingdomain.FeatureInput{
		{FeatureKey: string(billingdomain.FeatureUsersMax), IsEnabled: true, LimitValue: -1},
		{FeatureKey: string(billingdomain.FeatureBranchesMax), IsEnabled: true, LimitValue: -1},
	})
	if err != nil {
		t.Fatalf("testkit: create plan: %v", err)
	}
	a := &App{Deps: deps, Router: deps.Router(), Pool: pool, Config: cfg, plan: plan.ID}
	a.subscribe(t, DefaultTenant)
	return a
}

// Option customises New.
type Option func(*newSettings)

type newSettings struct {
	logger *slog.Logger
	mcp    bool
}

// WithLogger sends application logs to logger (default: discarded), e.g. to
// assert what gets logged.
func WithLogger(logger *slog.Logger) Option {
	return func(s *newSettings) { s.logger = logger }
}

// truncateAll empties every application table (migrations and the River
// queue schema are kept).
func truncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	rows, err := pool.Query(ctx, `SELECT tablename FROM pg_tables
		WHERE schemaname = current_schema()
		  AND tablename <> 'goose_db_version' AND tablename NOT LIKE 'river%'`)
	if err != nil {
		t.Fatalf("testkit: list tables: %v", err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("testkit: list tables: %v", err)
	}
	dbtest.Clean(t, pool, tables...)
}

func (a *App) subscribe(t *testing.T, tenantID int64) {
	t.Helper()
	if _, err := a.Deps.Platform.BillingSubs.StartTrial(context.Background(), tenantID, a.plan); err != nil {
		t.Fatalf("testkit: subscribe tenant %d: %v", tenantID, err)
	}
}

// ── users ────────────────────────────────────────────────────────────────

// User is a signed-in account. Token is a bearer token for it.
type User struct {
	ID       int64
	Email    string
	TenantID int64 // 0 for platform accounts (super admin)
	BranchID int64
	Token    string
}

type userSpec struct {
	tenantID int64
	role     string
	email    string
	name     string
}

// UserOption customises App.User.
type UserOption func(*userSpec)

// Role gives the user one of the tenant's roles by name (default "Admin").
func Role(name string) UserOption { return func(s *userSpec) { s.role = name } }

// InTenant creates the user in another tenant (default: the seeded one).
func InTenant(tenantID int64) UserOption { return func(s *userSpec) { s.tenantID = tenantID } }

// Email sets the e-mail address (default: a unique one).
func Email(email string) UserOption { return func(s *userSpec) { s.email = email } }

// Name sets the display name.
func Name(name string) UserOption { return func(s *userSpec) { s.name = name } }

var userSeq atomic.Int64

// User creates a tenant user (an Admin of the seeded tenant unless options
// say otherwise) and signs it in.
func (a *App) User(t *testing.T, options ...UserOption) *User {
	t.Helper()
	n := userSeq.Add(1)
	spec := userSpec{tenantID: DefaultTenant, role: "Admin", email: fmt.Sprintf("user%d@testkit.test", n), name: fmt.Sprintf("Test User %d", n)}
	for _, option := range options {
		option(&spec)
	}
	ctx := context.Background()
	role, err := a.Deps.Tx.Queries().GetRoleByName(ctx, dbgen.GetRoleByNameParams{Name: spec.role, TenantID: &spec.tenantID})
	if err != nil {
		t.Fatalf("testkit: role %q in tenant %d: %v", spec.role, spec.tenantID, err)
	}
	branchID := a.DefaultBranch(t, spec.tenantID)

	body := map[string]any{
		"name": spec.name, "email": spec.email, "password": Password,
		"branch_id": branchID, "roles": []int64{role.ID},
	}
	var created struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	a.As(a.SuperAdmin(t)).InTenant(spec.tenantID, branchID).
		Post("/api/v1/user", body).Status(t, http.StatusCreated).JSON(t, &created)

	return &User{
		ID: created.Data.ID, Email: spec.email, TenantID: spec.tenantID, BranchID: branchID,
		Token: a.Login(t, spec.email, Password),
	}
}

// SuperAdmin returns the seeded super admin, signed in.
func (a *App) SuperAdmin(t *testing.T) *User {
	t.Helper()
	if a.super == nil {
		a.super = &User{ID: 1, Email: "superadmin@ims.com", Token: a.Login(t, "superadmin@ims.com", Password)}
	}
	return a.super
}

// Login signs in and returns a bearer token.
func (a *App) Login(t *testing.T, email, password string) string {
	t.Helper()
	var body struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	a.Guest().Post("/api/v1/login", map[string]any{"email": email, "password": password, "device": "testkit"}).
		Status(t, http.StatusOK).JSON(t, &body)
	return body.Data.Token
}

// Tenant creates a tenant (with its default branch and roles) on the
// unlimited test plan and returns its id.
func (a *App) Tenant(t *testing.T, name string) int64 {
	t.Helper()
	var created struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	a.As(a.SuperAdmin(t)).InTenant(DefaultTenant, a.DefaultBranch(t, DefaultTenant)).
		Post("/api/v1/tenant", map[string]any{"name": name}).Status(t, http.StatusCreated).JSON(t, &created)
	a.subscribe(t, created.Data.ID)
	return created.Data.ID
}

// Role creates a role in the seeded tenant holding the given permissions
// and returns its name, ready for testkit.Role(name):
//
//	app.Role(t, "Viewer", "user.view", "user.view.all")
//	viewer := app.User(t, testkit.Role("Viewer"))
func (a *App) Role(t *testing.T, name string, permissions ...string) string {
	t.Helper()
	var created struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	a.As(a.SuperAdmin(t)).InTenant(DefaultTenant, a.DefaultBranch(t, DefaultTenant)).
		Post("/api/v1/role", map[string]any{"name": name}).Status(t, http.StatusCreated).JSON(t, &created)
	if len(permissions) > 0 {
		_, err := a.Pool.Exec(context.Background(),
			`INSERT INTO role_has_permissions (role_id, permission_id)
			 SELECT $1, id FROM permissions WHERE name = ANY($2)`, created.Data.ID, permissions)
		if err != nil {
			t.Fatalf("testkit: grant %v to role %q: %v", permissions, name, err)
		}
	}
	return name
}

// DefaultBranch returns the first branch of a tenant.
func (a *App) DefaultBranch(t *testing.T, tenantID int64) int64 {
	t.Helper()
	var id int64
	err := a.Pool.QueryRow(context.Background(),
		`SELECT id FROM branches WHERE tenant_id = $1 AND deleted_at IS NULL ORDER BY id LIMIT 1`, tenantID).Scan(&id)
	if err != nil {
		t.Fatalf("testkit: default branch of tenant %d: %v", tenantID, err)
	}
	return id
}

// ── requests ─────────────────────────────────────────────────────────────

// Client sends requests to the router as one user.
type Client struct {
	app     *App
	token   string
	headers map[string]string
}

// As sends requests as the given user.
func (a *App) As(user *User) *Client {
	return &Client{app: a, token: user.Token, headers: map[string]string{}}
}

// Guest sends requests without a token.
func (a *App) Guest() *Client {
	return &Client{app: a, headers: map[string]string{}}
}

// InTenant sets the acting tenant and branch headers (platform accounts).
func (c *Client) InTenant(tenantID, branchID int64) *Client {
	c.headers["X-Acting-Tenant"] = strconv.FormatInt(tenantID, 10)
	c.headers["X-Acting-Branch"] = strconv.FormatInt(branchID, 10)
	return c
}

// Header sets a request header.
func (c *Client) Header(key, value string) *Client {
	c.headers[key] = value
	return c
}

// Get sends a GET request.
func (c *Client) Get(path string) *Response { return c.Do(http.MethodGet, path, nil) }

// Post sends a POST request; body is a JSON string or any value to encode.
func (c *Client) Post(path string, body any) *Response { return c.Do(http.MethodPost, path, body) }

// Patch sends a PATCH request.
func (c *Client) Patch(path string, body any) *Response { return c.Do(http.MethodPatch, path, body) }

// Put sends a PUT request.
func (c *Client) Put(path string, body any) *Response { return c.Do(http.MethodPut, path, body) }

// Delete sends a DELETE request.
func (c *Client) Delete(path string) *Response { return c.Do(http.MethodDelete, path, nil) }

// Do sends a request through the router (no network involved).
func (c *Client) Do(method, path string, body any) *Response {
	var reader io.Reader
	switch value := body.(type) {
	case nil:
	case string:
		reader = strings.NewReader(value)
	case []byte:
		reader = strings.NewReader(string(value))
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			panic(fmt.Sprintf("testkit: encode request body: %v", err))
		}
		reader = strings.NewReader(string(encoded))
	}
	req := httptest.NewRequest(method, path, reader)
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	c.app.Router.ServeHTTP(rec, req)
	return &Response{ResponseRecorder: rec}
}

// Response is a recorded response with assertion helpers.
type Response struct {
	*httptest.ResponseRecorder
}

// Status fails the test unless the response has the given status code.
func (r *Response) Status(t *testing.T, want int) *Response {
	t.Helper()
	if r.Code != want {
		t.Fatalf("status = %d, want %d; body: %s", r.Code, want, r.Body.String())
	}
	return r
}

// JSON decodes the response body into dst.
func (r *Response) JSON(t *testing.T, dst any) *Response {
	t.Helper()
	if err := json.Unmarshal(r.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response (%d): %v; body: %s", r.Code, err, r.Body.String())
	}
	return r
}

// Data decodes the "data" member of a {data: ...} envelope into dst.
func (r *Response) Data(t *testing.T, dst any) *Response {
	t.Helper()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	r.JSON(t, &envelope)
	if err := json.Unmarshal(envelope.Data, dst); err != nil {
		t.Fatalf("decode data: %v; body: %s", err, r.Body.String())
	}
	return r
}

// TOTP is the current code an authenticator app would show for a two-factor
// secret (from POST /api/v1/two-factor), offset by steps of 30 seconds.
func TOTP(t *testing.T, secret string, steps int) string {
	t.Helper()
	code, err := authn.TOTPCodeAt(secret, time.Now().Add(time.Duration(steps)*30*time.Second))
	if err != nil {
		t.Fatalf("testkit: TOTP: %v", err)
	}
	return code
}

// Queued returns the data of the latest queued delivery of a template (a
// link or a code a test needs to follow), unsealing it when the message was
// sensitive. It fails the test when there is none.
//
//	link := app.Queued(t, "invitation")["link"].(string)
func (a *App) Queued(t *testing.T, template string) map[string]any {
	t.Helper()
	var raw []byte
	err := a.Pool.QueryRow(context.Background(), `SELECT args->'payload' FROM river_job
		WHERE args->>'kind' = 'delivery.send' AND args->'payload'->'message'->>'template' = $1
		ORDER BY id DESC LIMIT 1`, template).Scan(&raw)
	if err != nil {
		t.Fatalf("testkit: no queued %q delivery: %v", template, err)
	}
	var payload struct {
		Message struct {
			Data map[string]any `json:"data"`
		} `json:"message"`
		SealedData string `json:"sealed_data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("testkit: decode %q delivery: %v", template, err)
	}
	if payload.SealedData == "" {
		return payload.Message.Data
	}
	plain, err := a.Deps.Platform.Crypto.Decrypt(payload.SealedData)
	if err != nil {
		t.Fatalf("testkit: unseal %q delivery: %v", template, err)
	}
	var data map[string]any
	if err := json.Unmarshal(plain, &data); err != nil {
		t.Fatalf("testkit: decode %q delivery data: %v", template, err)
	}
	return data
}
